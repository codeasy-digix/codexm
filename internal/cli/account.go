package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

const accountHelp = `codexm account — named accounts for codexm workers

codexm account                         Show the saved default worker and accounts
codexm account --list                  List existing local logins
codexm account NAME                    Save the default for new/imported jobs
codexm account NAME --login --device   Log in with a device code (SSH supported)
codexm account NAME --login --browser  Log in with the browser
codexm setup --manager NAME            Choose the separate manager account

Existing jobs keep their own account. Native codex, its authentication and shell
environment are unchanged. View live quotas with codexm accounts or /accounts.
`

func accountCommand(ctx context.Context, c config.Config, args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Print(accountHelp)
		return nil
	}
	if len(args) == 0 || len(args) == 1 && args[0] == "--list" {
		names, e := accounts.List(c)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		fmt.Printf("Default worker: %s · Manager: %s\n", c.DefaultWorker(), c.Manager.Account)
		for _, n := range names {
			suffix := ""
			if n == c.DefaultWorker() {
				suffix = " (default worker)"
			}
			fmt.Println(n + suffix)
		}
		return nil
	}
	name := args[0]
	if !config.Nickname.MatchString(name) || name == "default" {
		return errors.New("usage: codexm account NAME [--login] [--browser|--device]")
	}
	f := flag.NewFlagSet("codexm account", flag.ContinueOnError)
	login := f.Bool("login", false, "log in again")
	browser := f.Bool("browser", false, "browser sign-in")
	device := f.Bool("device", false, "device-code sign-in")
	_ = f.Bool("set-default", false, "save codexm's default worker (also the default action)")
	if e := f.Parse(args[1:]); e != nil {
		return e
	}
	if len(f.Args()) != 0 || *browser && *device {
		return errors.New("choose one login method: --browser or --device")
	}
	lock, e := cr.AccountLoginLock(c, name)
	if e != nil {
		return e
	}
	defer lock.Close()
	r, e := cr.Resolve(ctx, c)
	if e != nil {
		return e
	}
	status := r.Command(ctx, name, "login", "status")
	if *login || *browser || *device || status.Run() != nil {
		options := []string{"login"}
		if *device || !*browser && (os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "") {
			options = append(options, "--device-auth")
		}
		cmd := r.Command(ctx, name, options...)
		cmd.Dir = filepath.Join(config.Expand(c.Runtime.AccountsDir), name)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if e := cmd.Run(); e != nil {
			return errors.New("login did not complete; codexm's default account was preserved")
		}
		if e := r.Command(ctx, name, "login", "status").Run(); e != nil {
			return errors.New("login completed without a usable named account; default was preserved")
		}
	}
	if supervisor.Ping(ctx, c) == nil {
		if e := supervisor.Request(ctx, c, "set-worker-account", supervisor.JobArgs{Account: name}, nil); e != nil {
			return e
		}
	} else {
		if e := saveWorkerAccount(c, name); e != nil {
			return e
		}
	}
	fmt.Printf("Default codexm worker: %s (saved). Existing jobs keep their accounts.\nNative codex was not changed.\n", name)
	return nil
}

func saveWorkerAccount(c config.Config, name string) error {
	if latest, e := config.Load(c.Path); e == nil {
		c = latest
	}
	c.WorkerAccounts.DefaultAccount = name
	if !slices.Contains(c.WorkerAccounts.Priority, name) {
		position := len(c.WorkerAccounts.Priority)
		if c.WorkerAccounts.FallbackAccount != "" {
			position--
		}
		c.WorkerAccounts.Priority = slices.Insert(c.WorkerAccounts.Priority, position, name)
	}
	return c.Save()
}
