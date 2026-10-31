package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/nativecli"
	"github.com/codeasy-digix/codexm/internal/shellinit"
	"github.com/codeasy-digix/codexm/internal/webbundle"
	"github.com/codeasy-digix/codexm/internal/webhost"
	"golang.org/x/term"
)

const compactHelp = `codexm — local chats, accounts, official Codex CLI

codexm                                      Choose a chat, then an account
codexm list [--json] [--archived]             Chats across every local project
codexm resume THREAD --account NAME          Resume in this terminal
codexm resume THREAD --account NAME --terminal  Open a named tmux terminal
codexm account [NAME] [--login|--set-default]  Native account selection (unchanged)
codexm accounts                              Saved local account information
codexm terminal SESSION                     Attach to a CLI opened from the web
codexm web [status|stop|logs]                 Local web: chat list and native terminal
codexm doctor                               Check native CLI and local paths
codexm shell-init zsh                        Shell integration

Resume runs official archive/unarchive (unarchive only for archived chats), then
codex --no-daemon --no-alt-screen resume THREAD. Codex owns all subsequent work.
No manager model, automatic account rotation, scheduling or history sync.
Existing manager data is read-only metadata; old worker services are not started.
`

func Main(args []string) error {
	f := flag.NewFlagSet("codexm", flag.ContinueOnError)
	path := f.String("config", "", "configuration file")
	version := f.Bool("version", false, "version")
	f.Usage = func() { fmt.Print(compactHelp) }
	if e := f.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return nil
		}
		return e
	}
	if *version {
		fmt.Println("codexm " + config.Version)
		return nil
	}
	args = f.Args()
	command := ""
	if len(args) > 0 {
		command, args = args[0], args[1:]
	}
	if command == "history-sync" {
		return errors.New("history-sync has been removed; codexm does not synchronize conversations across machines")
	}
	if command == "shell-init" {
		if len(args) != 1 || args[0] != "zsh" {
			return errors.New("usage: codexm shell-init zsh")
		}
		fmt.Print(shellinit.Zsh)
		return nil
	}
	if command == "__install-web" {
		if len(args) != 2 {
			return errors.New("expected source and destination")
		}
		m, e := webbundle.Copy(args[0], args[1])
		if e == nil {
			fmt.Println(m.Digest)
		}
		return e
	}
	if command == "__migrate-config" {
		return nil
	} // preserve old policy for still-running legacy workers
	if command == "__prompt-command" {
		// Retired plugin hooks must never block or submit ordinary native prompts.
		fmt.Println("{}")
		return nil
	}
	if command == "help" {
		fmt.Print(compactHelp)
		return nil
	}
	allowed := map[string]bool{"": true, "list": true, "status": true, "resume": true, "open": true, "account": true, "accounts": true, "terminal": true, "web": true, "__web": true, "__native-run": true, "bridge": true, "doctor": true}
	if !allowed[command] {
		return fmt.Errorf("%q is excluded from compact codexm; use list, resume, account or web (existing work is preserved)", command)
	}
	c, e := nativecli.Load(*path)
	if e != nil {
		return e
	}
	ctx := context.Background()
	self, e := os.Executable()
	if e != nil {
		return e
	}
	self, _ = filepath.EvalSymlinks(self)
	switch command {
	case "account":
		return accountCommand(ctx, c, args)
	case "accounts":
		return accountCommand(ctx, c, []string{"--list"})
	case "web":
		return webhost.Command(ctx, c, self, args, os.Stdout)
	case "__web":
		if len(args) != 1 {
			return errors.New("missing web launch spec")
		}
		webctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		defer stop()
		return webhost.Run(webctx, c, args[0])
	case "__native-run":
		if len(args) != 1 {
			return errors.New("missing native launch spec")
		}
		return nativecli.RunSession(ctx, c, args[0])
	case "bridge":
		if len(args) != 1 || args[0] != "--stdio" {
			return errors.New("usage: codexm bridge --stdio")
		}
		return nativecli.Serve(ctx, c, self, os.Stdin, os.Stdout)
	case "doctor", "status":
		f := flag.NewFlagSet(command, flag.ContinueOnError)
		jsonOutput := f.Bool("json", false, "JSON summary")
		if err := f.Parse(args); err != nil {
			return err
		}
		if f.NArg() != 0 {
			return errors.New("unexpected status argument")
		}
		r, e := nativecli.Resolver(c)
		if e != nil {
			return e
		}
		list, e := nativecli.List(ctx, c)
		if e != nil {
			return e
		}
		archived := 0
		for _, entry := range list.Sessions {
			if entry.Archived {
				archived++
			}
		}
		if *jsonOutput {
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"mode": "native-cli", "codex": r.Binary, "chats": len(list.Sessions), "archived": archived, "history": config.Expand(c.Runtime.SharedCodexHome), "manager": false, "scheduler": false, "history_sync": false})
		}
		fmt.Printf("Mode: official CLI handoff\nCodex: %s\nLocal chats: %d (%d archived)\nHistory: %s\nManager/scheduler/history-sync: excluded\n", r.Binary, len(list.Sessions), archived, config.Expand(c.Runtime.SharedCodexHome))
		return nil
	case "terminal":
		if len(args) != 1 {
			return errors.New("usage: codexm terminal SESSION")
		}
		if _, e := nativecli.Screen(ctx, c, args[0]); e != nil {
			return e
		}
		p, e := exec.LookPath("tmux")
		if e != nil {
			return e
		}
		return syscall.Exec(p, []string{p, "attach-session", "-t", "=" + args[0]}, os.Environ())
	case "list":
		f := flag.NewFlagSet("list", flag.ContinueOnError)
		j := f.Bool("json", false, "JSON")
		archived := f.Bool("archived", false, "include archived chats")
		search := f.String("search", "", "filter titles, projects, accounts and IDs")
		f.Bool("all", true, "all project directories")
		if e := f.Parse(args); e != nil {
			return e
		}
		if f.NArg() != 0 {
			return errors.New("unexpected list argument")
		}
		list, e := nativecli.List(ctx, c)
		if e != nil {
			return e
		}
		list.Sessions = filterChats(list.Sessions, *search, *archived)
		if *j {
			return json.NewEncoder(os.Stdout).Encode(list)
		}
		fmt.Printf("Codex conversations: %d\n", len(list.Sessions))
		printChats(list.Sessions, 0, len(list.Sessions))
		return nil
	case "resume", "open":
		if len(args) == 0 {
			return errors.New("usage: codexm resume THREAD --account NAME")
		}
		id := args[0]
		f := flag.NewFlagSet("resume", flag.ContinueOnError)
		name := f.String("account", "", "saved nickname or default")
		background := f.Bool("terminal", command == "open", "named tmux terminal")
		if e := f.Parse(args[1:]); e != nil {
			return e
		}
		if f.NArg() != 0 {
			return errors.New("unexpected resume argument")
		}
		if *name == "" {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return errors.New("--account NAME is required without an interactive terminal")
			}
			*name, e = chooseAccount(c, bufio.NewScanner(os.Stdin))
			if e != nil {
				return e
			}
		}
		if *background {
			l, e := nativecli.Open(ctx, c, self, id, *name)
			if e == nil {
				fmt.Printf("Native terminal: %s\ntmux attach -t %s\n", l.Session, l.Session)
			}
			return e
		}
		l, e := nativecli.Plan(ctx, c, id, *name)
		if e != nil {
			return e
		}
		return nativecli.Resume(ctx, c, l)
	default:
		if len(args) != 0 {
			return errors.New("unexpected argument")
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("interactive terminal required; use codexm list --json")
		}
		return pickChat(ctx, c)
	}
}
func filterChats(all []catalog.Entry, search string, archive bool) []catalog.Entry {
	out := []catalog.Entry{}
	search = strings.ToLower(search)
	for _, e := range all {
		if e.Archived && !archive {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(e.Title+" "+e.ID+" "+e.Cwd+" "+e.Account), search) {
			continue
		}
		out = append(out, e)
	}
	return out
}
func printChats(all []catalog.Entry, start, end int) {
	for i := start; i < end && i < len(all); i++ {
		e := all[i]
		account := e.Account
		if account == "" {
			account = "unknown"
		}
		arch := ""
		if e.Archived {
			arch = " [archived]"
		}
		fmt.Printf("%3d. %s%s\n     %s · last account: %s · %s\n", i+1, compactLine(e.Title), arch, compactLine(e.ID), compactLine(account), compactLine(e.Cwd))
	}
}
func compactLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
}
func chooseAccount(c config.Config, s *bufio.Scanner) (string, error) {
	a, e := nativecli.Accounts(c)
	if e != nil {
		return "", e
	}
	fmt.Println("\nResume with account: 0. native default")
	for i, x := range a {
		fmt.Printf("%d. %s\n", i+1, x.Name)
	}
	for {
		fmt.Print("Account number (q: cancel): ")
		if !s.Scan() {
			return "", errors.New("cancelled")
		}
		v := strings.TrimSpace(s.Text())
		if v == "q" {
			return "", errors.New("cancelled")
		}
		n, e := strconv.Atoi(v)
		if e == nil && n >= 0 && n <= len(a) {
			if n == 0 {
				return "default", nil
			}
			return a[n-1].Name, nil
		}
		fmt.Println("Choose an account number.")
	}
}
func pickChat(ctx context.Context, c config.Config) error {
	s := bufio.NewScanner(os.Stdin)
	page := 0
	query := ""
	archived := false
	for {
		list, e := nativecli.List(ctx, c)
		if e != nil {
			return e
		}
		all := filterChats(list.Sessions, query, archived)
		if page*20 >= len(all) {
			page = 0
		}
		fmt.Printf("\nLocal chats: %d · page %d · filter %q\n", len(all), page+1, query)
		printChats(all, page*20, (page+1)*20)
		fmt.Print("\nNumber: choose · /text: search · n/p: page · a: archives · r: refresh · q: quit\n> ")
		if !s.Scan() {
			return s.Err()
		}
		v := strings.TrimSpace(s.Text())
		switch v {
		case "q":
			return nil
		case "n":
			page++
			continue
		case "p":
			if page > 0 {
				page--
			}
			continue
		case "r":
			continue
		case "a":
			archived = !archived
			page = 0
			continue
		}
		if strings.HasPrefix(v, "/") {
			query = strings.TrimPrefix(v, "/")
			page = 0
			continue
		}
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > len(all) {
			fmt.Println("Choose a chat number.")
			continue
		}
		name, e := chooseAccount(c, s)
		if e != nil {
			continue
		}
		l, e := nativecli.Plan(ctx, c, all[n-1].ID, name)
		if e != nil {
			fmt.Println(e)
			continue
		}
		return nativecli.Resume(ctx, c, l)
	}
}
