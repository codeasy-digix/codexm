//go:build gui_experiment

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/codeasy-digix/codexm/internal/nativecli"
	"golang.org/x/term"
)

const daemonExperimentHelp = `
Experimental only (official CLI 0.167.0):
  codexm daemon start --account NAME
  codexm daemon status --account NAME
  codexm daemon new --account NAME [--cwd ABS] [--terminal]
  codexm daemon resume THREAD --account NAME [--terminal]
  codexm daemon switch THREAD --from NAME --account NAME [--interrupt] [--request-id UUID]
  codexm daemon request REQUEST_ID
Named accounts only. No GUI restart, default change, automatic prompt, or service restart.
switch preserves the ID; --interrupt stops only the selected active turn.
Close other viewers of the selected chat before switch. Uncertain requests are never replayed.
`

func runDaemonExperiment(args []string, configPath string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Print(daemonExperimentHelp)
		return nil
	}
	c, err := nativecli.Load(configPath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	action, args := args[0], args[1:]
	if action == "__run" {
		if len(args) != 1 {
			return errors.New("invalid daemon terminal helper arguments")
		}
		return nativecli.RunDaemonTerminal(ctx, c, args[0])
	}
	if action == "request" {
		if len(args) != 1 {
			return errors.New("daemon request requires one request UUID")
		}
		r, err := nativecli.ReadDaemonTransfer(c, args[0])
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	if action != "start" && action != "status" && action != "new" && action != "resume" && action != "switch" {
		return errors.New("unknown experimental daemon command; use daemon help")
	}
	id := ""
	if action == "resume" || action == "switch" {
		if len(args) == 0 {
			return errors.New("complete thread UUID required")
		}
		id, args = args[0], args[1:]
	}
	f := flag.NewFlagSet("daemon "+action, flag.ContinueOnError)
	account := f.String("account", "", "saved named account (required)")
	var from, request, cwd string
	var terminal, interrupt bool
	if action == "switch" {
		f.StringVar(&from, "from", "", "source daemon account")
		f.StringVar(&request, "request-id", "", "durable operation UUID; reuse inspects, never replays")
		f.BoolVar(&interrupt, "interrupt", false, "interrupt the selected active turn before transferring")
	}
	if action == "new" {
		f.StringVar(&cwd, "cwd", "", "absolute project directory")
	}
	if action == "new" || action == "resume" {
		f.BoolVar(&terminal, "terminal", false, "create a named frontend window without attaching")
	}
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || *account == "" {
		return errors.New("--account NAME is required; unexpected arguments are not forwarded")
	}
	switch action {
	case "start", "status":
		var info nativecli.DaemonInfo
		if action == "start" {
			info, err = nativecli.StartDaemonExperiment(ctx, c, *account)
		} else {
			info, err = nativecli.DaemonExperimentStatus(ctx, c, *account)
		}
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(info)
	case "switch":
		if from == "" {
			return errors.New("--from NAME is required")
		}
		if request == "" {
			request, err = nativecli.DaemonRequestID()
			if err != nil {
				return err
			}
		}
		fmt.Fprintln(os.Stderr, "Daemon transfer request:", request)
		r, err := nativecli.TransferDaemonChat(ctx, c, request, id, from, *account, interrupt)
		if r.Request != "" {
			if e := json.NewEncoder(os.Stdout).Encode(r); e != nil {
				return errors.Join(err, e)
			}
		}
		if err == nil && r.State != "completed" {
			return fmt.Errorf("transfer %s remains %s; request inspected without replay", r.Request, r.State)
		}
		return err
	}
	if !terminal && !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("interactive terminal required; use --terminal to leave the frontend in a named window")
	}
	if action == "new" && cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return err
	}
	l, err := nativecli.OpenDaemonTerminal(ctx, c, self, *account, id, cwd)
	if err != nil {
		return err
	}
	fmt.Printf("Official daemon account: %s; frontend: default/codexm %s. Closing the terminal does not stop its daemon.\n", l.Account, l.Window)
	if terminal {
		return nil
	}
	return nativecli.AttachGUIRouteNew(ctx, l)
}
