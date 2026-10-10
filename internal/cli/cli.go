package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/supervisor"
	"github.com/codeasy-digix/codexm/internal/tui"
	"golang.org/x/term"
)

func Main(args []string) error {
	global := flag.NewFlagSet("codexm", flag.ContinueOnError)
	cfg := global.String("config", "", "configuration file")
	version := global.Bool("version", false, "print version")
	global.Usage = help
	if e := global.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return nil
		}
		return e
	}
	if *version {
		fmt.Println("codexm " + config.Version)
		return nil
	}
	args = global.Args()
	command := ""
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}
	if command == "__worker" {
		if len(args) != 1 {
			return errors.New("missing worker spec")
		}
		b, e := os.ReadFile(args[0])
		if e != nil {
			return e
		}
		var spec cr.WorkerSpec
		if e = json.Unmarshal(b, &spec); e != nil {
			return e
		}
		return cr.Worker(spec)
	}
	c, e := config.Load(*cfg)
	if e != nil {
		return e
	}
	self, e := os.Executable()
	if e != nil {
		return e
	}
	self, _ = filepath.EvalSymlinks(self)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if command == "__supervisor" {
		s, e := supervisor.New(ctx, c, self)
		if e != nil {
			return e
		}
		return s.Run()
	}
	if command == "doctor" {
		return doctor(ctx, c, args)
	}
	if command == "runtime" {
		if len(args) != 1 || args[0] != "refresh" {
			return errors.New("usage: codexm runtime refresh")
		}
		r, e := cr.Locate(c)
		if e != nil {
			return e
		}
		r, e = r.Refresh(ctx)
		if e != nil {
			return e
		}
		fmt.Printf("Pinned %s\n%s\nExisting jobs keep their engine. Restart the supervisor to use this pin for new jobs.\n", r.Version, r.Binary)
		return nil
	}
	if command == "setup" {
		f := flag.NewFlagSet("setup", flag.ContinueOnError)
		manager := f.String("manager", "", "manager account nickname")
		if e = f.Parse(args); e != nil {
			return e
		}
		if *manager == "" {
			return tui.Run(c, self)
		}
		if !config.Nickname.MatchString(*manager) {
			return errors.New("invalid account nickname")
		}
		r, e := cr.Resolve(ctx, c)
		if e != nil {
			return e
		}
		a := accounts.New(r).Read(ctx, *manager)
		if !a.Authenticated || a.Error != "" {
			return fmt.Errorf("manager account: %s", a.Error)
		}
		if supervisor.Ping(ctx, c) == nil {
			if e = supervisor.Request(ctx, c, "set-manager", supervisor.JobArgs{Account: *manager}, nil); e != nil {
				return e
			}
		} else {
			c.Manager.Account = *manager
			if e = c.Save(); e != nil {
				return e
			}
		}
		fmt.Printf("Manager account %s saved in %s\n", *manager, c.Path)
		return nil
	}
	if command == "" || command == "agents" && len(args) == 0 {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("interactive terminal required; use status --json, or setup --manager NAME")
		}
		return tui.Run(c, self)
	}
	if command == "help" {
		help()
		return nil
	}
	if c.Manager.Account == "" {
		return errors.New("choose the manager account first: codexm (interactive) or codexm setup --manager NAME")
	}
	if command == "shutdown" && supervisor.Ping(ctx, c) != nil {
		return errors.New("supervisor is not running")
	}
	if e = supervisor.Start(ctx, c, self); e != nil {
		return e
	}
	var result any
	switch command {
	case "status", "agents":
		jsonOutput := has(args, "--json")
		var st supervisor.Status
		if e = supervisor.Request(ctx, c, "status", nil, &st); e != nil {
			return e
		}
		if jsonOutput {
			return output(st)
		}
		fmt.Printf("codexm %s · manager %s · %s · %s\n", st.Version, st.ManagerAccount, st.Health, st.CodexVersion)
		fmt.Println(st.Attach)
		for _, j := range st.Jobs {
			fmt.Printf("%s  %-20s %-14s %-10s %s\n", j.ID, j.Title, j.State, j.Account, j.ThreadID)
			if j.Session != "" {
				fmt.Println("  tmux attach -t " + j.Session)
			}
		}
		return nil
	case "new":
		f := flag.NewFlagSet(command, flag.ContinueOnError)
		n := supervisor.NewArgs{}
		f.StringVar(&n.Model, "model", "", "model for a new conversation")
		f.StringVar(&n.Effort, "effort", "", "reasoning effort for a new conversation")
		f.StringVar(&n.Cwd, "cwd", "", "workspace directory")
		f.StringVar(&n.Title, "title", "", "title")
		f.StringVar(&n.Kind, "kind", "chat", "chat, task, goal, group")
		f.StringVar(&n.Account, "account", "", "initial worker account")
		f.StringVar(&n.ParentID, "parent", "", "parent job ID")
		f.StringVar(&n.Goal, "goal", "", "goal objective")
		f.BoolVar(&n.ReadOnly, "read-only", false, "read-only workspace")
		prompt := f.String("prompt", "", "first instruction")
		promptFile := f.String("prompt-file", "", "read instruction from a file")
		check := f.String("check", "", "completion check as JSON argv")
		_ = f.Bool("json", false, "JSON result")
		if e = f.Parse(args); e != nil {
			return e
		}
		n.Prompt = *prompt
		if n.Prompt == "" {
			n.Prompt = strings.Join(f.Args(), " ")
		}
		if *promptFile != "" {
			b, e := os.ReadFile(*promptFile)
			if e != nil {
				return e
			}
			n.Prompt = string(b)
		}
		if n.Cwd == "" {
			n.Cwd, _ = os.Getwd()
		}
		if *check != "" {
			if e = json.Unmarshal([]byte(*check), &n.CompletionCommand); e != nil {
				return e
			}
		}
		e = supervisor.Request(ctx, c, command, n, &result)
	case "resume":
		f := flag.NewFlagSet(command, flag.ContinueOnError)
		a := supervisor.JobArgs{}
		f.StringVar(&a.Account, "account", "", "initial worker account")
		f.StringVar(&a.Kind, "kind", "", "override job kind")
		f.BoolVar(&a.Manage, "manage", false, "explicitly take ownership and continue")
		_ = f.Bool("json", false, "JSON result")
		// Accept the conversational ID before flags as well as after them.
		id := ""
		if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			id = args[0]
			args = args[1:]
		}
		if e = f.Parse(args); e != nil {
			return e
		}
		if id == "" && len(f.Args()) > 0 {
			id = f.Args()[0]
		}
		a.ThreadID = id
		e = supervisor.Request(ctx, c, command, a, &result)
	case "threads":
		f := flag.NewFlagSet(command, flag.ContinueOnError)
		search := f.String("search", "", "search term")
		cursor := f.String("cursor", "", "next page cursor")
		_ = f.Bool("json", false, "JSON result")
		if e = f.Parse(args); e != nil {
			return e
		}
		e = supervisor.Request(ctx, c, "threads", supervisor.JobArgs{Search: *search, Cursor: *cursor}, &result)
	case "send", "steer", "manager", "goal":
		id := ""
		if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			id = args[0]
			args = args[1:]
		}
		f := flag.NewFlagSet(command, flag.ContinueOnError)
		text := f.String("text", "", "instruction")
		file := f.String("file", "", "instruction file")
		all := f.Bool("all", false, "manager: scope all jobs")
		_ = f.Bool("json", false, "JSON result")
		if e = f.Parse(args); e != nil {
			return e
		}
		if *all {
			id = ""
		}
		if *text == "" {
			*text = strings.Join(f.Args(), " ")
		}
		if *file != "" {
			b, e := os.ReadFile(*file)
			if e != nil {
				return e
			}
			*text = string(b)
		}
		api := command
		if command == "steer" {
			api = "send"
		}
		e = supervisor.Request(ctx, c, api, supervisor.JobArgs{ID: id, Text: *text, Steer: command == "steer"}, &result)
	case "pause", "resume-job", "stop", "complete", "events", "read":
		if len(args) < 1 {
			return fmt.Errorf("usage: codexm %s JOB_ID", command)
		}
		e = supervisor.Request(ctx, c, command, supervisor.JobArgs{ID: args[0]}, &result)
	case "attach":
		if len(args) != 1 {
			return errors.New("usage: codexm attach JOB_ID")
		}
		var a struct {
			Binary   string `json:"binary"`
			Socket   string `json:"socket"`
			ThreadID string `json:"thread_id"`
		}
		if e = supervisor.Request(ctx, c, "attach", supervisor.JobArgs{ID: args[0]}, &a); e != nil {
			return e
		}
		cmd := exec.Command(a.Binary, "-c", "check_for_update_on_startup=false", "--remote", "unix://"+a.Socket, "resume", a.ThreadID)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	case "reply":
		if len(args) != 3 {
			return errors.New("usage: codexm reply JOB_ID REQUEST_ID_JSON RESULT_JSON")
		}
		e = supervisor.Request(ctx, c, "reply", supervisor.JobArgs{ID: args[0], RequestID: json.RawMessage(args[1]), Result: json.RawMessage(args[2])}, &result)
	case "resolve-action":
		if len(args) != 2 {
			return errors.New("usage: codexm resolve-action ACTION_ID done|cancelled (after inspecting history)")
		}
		e = supervisor.Request(ctx, c, "resolve-action", map[string]string{"action_id": args[0], "decision": args[1]}, &result)
	case "accounts", "usage", "settings", "shutdown":
		e = supervisor.Request(ctx, c, command, nil, &result)
	default:
		return fmt.Errorf("unknown command %q; codexm help", command)
	}
	if e != nil {
		return e
	}
	if result != nil {
		return output(result)
	}
	return nil
}
func output(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
func has(args []string, value string) bool {
	for _, a := range args {
		if a == value {
			return true
		}
	}
	return false
}
func help() {
	fmt.Fprintln(os.Stdout, `codexm — persistent Codex job manager

codexm [--config FILE]                  Interactive task list / first-run setup
codexm setup --manager NAME             Save manager account explicitly
codexm status --json                    Jobs, health and named tmux sessions
codexm new --cwd DIR --kind chat --prompt 'instruction'
codexm new --kind goal --goal 'objective' [--account NAME]
codexm threads --search TEXT            Search existing conversations
codexm resume THREAD [--account NAME]   Import for observation
codexm resume THREAD --manage           Take ownership and continue
codexm send JOB --text 'followup'        Queue another instruction
codexm steer JOB --text 'correction'    Steer the active turn explicitly
codexm pause|resume-job|stop|complete JOB
codexm read|events|attach JOB
codexm manager JOB --text 'instruction' Short manager model call
codexm manager --all --text 'instruction'
codexm accounts|usage|settings
codexm reply JOB REQUEST_ID_JSON RESULT_JSON
codexm resolve-action ACTION done|cancelled
codexm doctor [--json] [--live]          Runtime/configuration diagnostics
codexm runtime refresh                 Explicit compatible engine adoption
codexm shutdown                        Stop managed workers and supervisor

TUI: n new · r conversations · m manager · i input · p pause · s resume
     a official Codex TUI · /help · /quit detaches, keeps running work.`)
}
func doctor(ctx context.Context, c config.Config, args []string) error {
	result := map[string]any{"version": config.Version, "config": c.Path, "state_dir": c.StateDir(), "manager_account": c.Manager.Account, "manager_selected": c.Manager.Account != "", "checks": map[string]string{}}
	checks := result["checks"].(map[string]string)
	var failures []string
	if e := c.Validate(); e != nil {
		checks["config"] = e.Error()
		failures = append(failures, e.Error())
	} else {
		checks["config"] = "ok"
	}
	for _, name := range []string{"tmux", c.Runtime.Controller} {
		if p, e := exec.LookPath(name); e != nil {
			checks[name] = e.Error()
			failures = append(failures, e.Error())
		} else {
			checks[name] = p
		}
	}
	r, e := cr.Resolve(ctx, c)
	if e != nil {
		checks["engine"] = e.Error()
		failures = append(failures, e.Error())
	} else {
		checks["engine"] = "pinned, digest verified, protocol checked"
		result["codex_version"] = r.Version
		result["codex_binary"] = r.Binary
		if has(args, "--live") {
			a := accounts.New(r).Read(ctx, c.Manager.Account)
			result["manager_availability"] = a
			if a.Error != "" {
				failures = append(failures, a.Error)
			}
		}
	}
	result["supervisor_running"] = supervisor.Ping(ctx, c) == nil
	result["ok"] = len(failures) == 0
	_ = output(result)
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}
