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
)

const accountHelp = `codexm account — saved logins and the native Codex default login

codexm account                         Show saved logins and the native login
codexm account --list                  List existing local logins
codexm account NAME                    Check/login and register a saved nickname
codexm account NAME default            Change the native Codex default login
codexm account NAME --set-default      Flag form of the native default change
codexm account NAME --login --device   Log in with a device code (SSH supported)
codexm account NAME --login --browser  Log in with the browser
codexm setup --manager NAME            Choose the separate manager account

Worker order is configured in the management panel. Registration leaves existing
order unchanged and adds a missing nickname before the fallback account. Explicit
default changes only the shared Codex home's file login, for subsequent native
launches using that home. Existing terminals, GUI processes and workers keep their
current connection; they are not restarted. View live quotas with codexm accounts.
`

const accountUsage = "usage: codexm account NAME [default|--set-default] [--login] [--browser|--device]"

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
		native := accounts.CurrentNickname(c, config.Expand(c.Runtime.SharedCodexHome), "")
		if native == "" {
			native = "unrecognized or not signed in"
		}
		fmt.Printf("Native login: %s · Manager: %s\n", native, c.Manager.Account)
		for _, n := range names {
			suffix := ""
			if n == native {
				suffix = " (native login)"
			}
			fmt.Println(n + suffix)
		}
		return nil
	}
	name := args[0]
	if !config.Nickname.MatchString(name) || name == "default" {
		return errors.New(accountUsage)
	}
	options := args[1:]
	positionalDefault := len(options) > 0 && options[0] == "default"
	if positionalDefault {
		options = options[1:]
	}
	f := flag.NewFlagSet("codexm account", flag.ContinueOnError)
	login := f.Bool("login", false, "log in again")
	browser := f.Bool("browser", false, "browser sign-in")
	device := f.Bool("device", false, "device-code sign-in")
	nativeDefault := f.Bool("set-default", false, "change the native Codex default login")
	if e := f.Parse(options); e != nil {
		return e
	}
	if len(f.Args()) != 0 {
		return fmt.Errorf("unexpected account argument %q; %s", f.Arg(0), accountUsage)
	}
	if *browser && *device {
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
	if *login || *browser || *device || r.Command(ctx, name, "login", "status").Run() != nil {
		deviceLogin := *device || !*browser && (os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "")
		args := []string{"login"}
		if deviceLogin {
			args = append(args, "--device-auth")
		}
		cmd := r.Command(ctx, name, args...)
		cmd.Dir = filepath.Join(config.Expand(c.Runtime.AccountsDir), name)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if e := cmd.Run(); e != nil {
			return errors.New("login did not complete; native default and worker order were preserved")
		}
		if e := r.Command(ctx, name, "login", "status").Run(); e != nil {
			return errors.New("login completed without a usable named account; native default was preserved")
		}
	}
	if positionalDefault || *nativeDefault {
		backup, e := accounts.SetNativeDefault(c, name)
		if e != nil {
			return e
		}
		fmt.Printf("Native Codex default login: %s (saved for %s).\n", name, config.Expand(c.Runtime.SharedCodexHome))
		if backup != "" {
			fmt.Printf("Previous login backup: %s\n", backup)
		}
		fmt.Println("Native file login was changed. Running GUI/daemon processes may retain cached authentication until restarted. Worker order and existing jobs were not changed.")
		return nil
	}
	if e := registerAccount(c, name); e != nil {
		return e
	}
	fmt.Printf("Saved account: %s. Existing priority order was preserved; native Codex login was not changed.\n", name)
	return nil
}

func registerAccount(c config.Config, name string) error {
	_, _, e := config.Update(c.Path, "", func(c *config.Config) error {
		if !slices.Contains(c.WorkerAccounts.Priority, name) {
			position := len(c.WorkerAccounts.Priority)
			if fallback := slices.Index(c.WorkerAccounts.Priority, c.WorkerAccounts.FallbackAccount); fallback >= 0 {
				position = fallback
			}
			c.WorkerAccounts.Priority = slices.Insert(c.WorkerAccounts.Priority, position, name)
		}
		return nil
	})
	return e
}
