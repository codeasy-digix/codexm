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
	"syscall"

	"github.com/codeasy-digix/codexm/internal/nativecli"
	"golang.org/x/term"
)

const guiExperimentHelp = "\nExperimental build: codexm gui [help|resume|new|draft|list|open|send|status]\n"

func runGUIExperiment(command string, args []string, configPath string) (bool, error) {
	if command != "gui" {
		return false, nil
	}
	if configPath != "" && (len(args) == 0 || args[0] != "__new") {
		return true, errors.New("the GUI experiment uses the native desktop home, not --config or a selected CLI account")
	}
	self, err := os.Executable()
	if err != nil {
		return true, err
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return true, err
	}
	if len(args) > 0 {
		switch args[0] {
		case "resume", "new", "__new":
			return true, runGUIHybrid(self, args, configPath)
		}
	}
	return true, execGUICompanion(self, args)
}

func execGUICompanion(self string, args []string) error {
	binary := filepath.Join(filepath.Dir(self), "codexm-gui")
	info, err := os.Stat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return errors.New("matching codexm-gui is missing; build with scripts/build-gui-experiment.sh (installed codexm is unchanged)")
	}
	return syscall.Exec(binary, append([]string{binary}, args...), os.Environ())
}

type guiHybridOptions struct {
	action, thread, account, cwd string
	terminal, plan               bool
}

func parseGUIHybrid(args []string) (guiHybridOptions, error) {
	o := guiHybridOptions{action: args[0]}
	args = args[1:]
	if o.action == "resume" {
		if len(args) == 0 {
			return o, errors.New("usage: codexm gui resume THREAD [--account NAME] [--plan|--terminal]")
		}
		o.thread, args = args[0], args[1:]
	}
	f := flag.NewFlagSet("gui "+o.action, flag.ContinueOnError)
	f.StringVar(&o.account, "account", "", "explicit account: use CLI, never change GUI login")
	f.BoolVar(&o.terminal, "terminal", false, "leave native CLI running in a named tmux window")
	if o.action == "new" {
		f.StringVar(&o.cwd, "cwd", "", "absolute project directory; default is current directory")
	} else {
		f.BoolVar(&o.plan, "plan", false, "inspect route without opening a window or submitting input")
	}
	if err := f.Parse(args); err != nil {
		return o, err
	}
	if f.NArg() != 0 || (o.plan && o.terminal) {
		return o, errors.New("unexpected or conflicting GUI route arguments")
	}
	if o.account == "" {
		explicit := false
		f.Visit(func(v *flag.Flag) {
			if v.Name == "account" {
				explicit = true
			}
		})
		if explicit {
			return o, errors.New("--account requires a saved nickname or default")
		}
	}
	return o, nil
}

func runGUIHybrid(self string, args []string, configPath string) error {
	c, err := nativecli.Load(configPath)
	if err != nil {
		return err
	}
	if args[0] == "__new" {
		if len(args) != 3 {
			return errors.New("invalid native new-chat helper arguments")
		}
		return nativecli.RunGUIRouteNew(c, args[1], args[2])
	}
	o, err := parseGUIHybrid(args)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if o.action == "resume" {
		route, err := nativecli.InspectGUIRoute(ctx, c, o.thread, o.account)
		if err != nil {
			return err
		}
		if o.plan {
			return json.NewEncoder(os.Stdout).Encode(route)
		}
		if route.Route == "gui" {
			fmt.Fprintln(os.Stderr, "Keeping the existing GUI owner and its current login; no account override or input submitted.")
			return execGUICompanion(self, []string{"open", o.thread, "--app", route.App})
		}
		if route.Route == "needs-gui-release" {
			return errors.New(route.Detail)
		}
		if !o.terminal && !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("interactive terminal required for native CLI attachment; use --terminal to leave a window running")
		}
		l, err := nativecli.OpenGUIRouteCLI(ctx, c, self, route, o.account != "")
		if err != nil {
			return err
		}
		fmt.Printf("Native account: %s; terminal: %s\n%s\n", l.Account, l.Session, nativecli.AttachCommand(l))
		if o.terminal {
			return nil
		}
		return nativecli.Attach(ctx, c, l.Session)
	}
	if !o.terminal && !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("interactive terminal required for a new CLI chat; use --terminal to leave a window running")
	}
	if o.cwd == "" {
		o.cwd, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	l, err := nativecli.OpenGUIRouteNew(ctx, c, self, o.account, o.cwd)
	if err != nil {
		return err
	}
	fmt.Printf("New official CLI: account %s, default/codexm window %s. Once saved, select the chat with codexm.\n", l.Account, l.Window)
	if o.terminal {
		return nil
	}
	return nativecli.AttachGUIRouteNew(ctx, l)
}
