package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/nativecli"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/shellinit"
)

const accountHelp = `codexm account — native Codex accounts

codexm account                         Show this terminal's login and the native default
codexm account --list                  List existing local logins
codexm account NAME                    Select native Codex's account in this terminal
codexm account NAME --set-default      Save native Codex's default for new terminals
codexm account NAME default            Legacy alias for --set-default
codexm account NAME --login --device   Log in with a device code (SSH supported)
codexm account NAME --login --browser  Log in with the browser
codexm account NAME --remove           Remove codexm registration; keep login and history

Load the zsh adapter once: source <(command codexm shell-init zsh)
Account selection changes this shell's environment for subsequent native launches.
--set-default changes the shared native file login and preserves an explicit
terminal selection. A new independent terminal uses that native default; child
shells inherit their parent's selection. Running Codex/GUI/daemon connections are
not restarted. Account selection does not start a manager or change other running
CLIs. Choose an account again when opening a chat. The local web can refresh usage.
`

const accountUsage = "usage: codexm account NAME [default|--set-default] [--login] [--browser|--device] | NAME --remove"

func accountCommand(ctx context.Context, c config.Config, args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Print(accountHelp)
		return nil
	}
	if len(args) == 0 || len(args) == 1 && args[0] == "--list" {
		names, e := accounts.SavedLogins(c)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		native := accounts.CurrentNickname(c, config.Expand(c.Runtime.SharedCodexHome), "")
		if native == "" {
			native = "unrecognized or not signed in"
		}
		home := os.Getenv("CODEX_HOME")
		if home == "" {
			home = config.Expand(c.Runtime.SharedCodexHome)
		}
		current := accounts.CurrentNickname(c, home, "")
		if current == "" {
			current = "unrecognized or not signed in"
		}
		fmt.Printf("Native Codex in this terminal: %s\nNative default for new terminals / GUI: %s\n", current, native)
		for _, n := range names {
			suffix := ""
			if n == current {
				suffix += " (this terminal)"
			}
			if n == native {
				suffix += " (native default)"
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
	remove := f.Bool("remove", false, "remove codexm registration while keeping credentials and history")
	if e := f.Parse(options); e != nil {
		return e
	}
	if len(f.Args()) != 0 {
		return fmt.Errorf("unexpected account argument %q; %s", f.Arg(0), accountUsage)
	}
	if *browser && *device {
		return errors.New("choose one login method: --browser or --device")
	}
	if *remove {
		if *login || *browser || *device || positionalDefault || *nativeDefault {
			return errors.New("--remove cannot be combined with sign-in or default-login options")
		}
		if err := nativecli.RemoveAccount(c, name); err != nil {
			return err
		}
		fmt.Printf("Removed registration: %s. Login files, native default, conversations and historical job records were preserved.\n", name)
		return nil
	}
	var result *os.File
	if !positionalDefault && !*nativeDefault {
		var err error
		result, err = shellinit.OpenResult()
		if err != nil {
			return err
		}
		defer result.Close()
	}
	lock, e := cr.AccountLoginLock(c, name)
	if e != nil {
		return e
	}
	defer lock.Close()
	r, e := nativecli.Resolver(c)
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
			return errors.New("login did not complete; existing native authentication was preserved")
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
		fmt.Printf("Native Codex default for new terminals / GUI: %s (saved for %s).\n", name, config.Expand(c.Runtime.SharedCodexHome))
		if backup != "" {
			fmt.Printf("Previous login backup: %s\n", backup)
		}
		fmt.Println("An explicit terminal selection was preserved. Running Codex/GUI/daemon connections keep cached authentication until restarted. Existing Codex sessions were not changed.")
		return nil
	}
	if e := accounts.ValidateNativeAccount(c, name); e != nil {
		return e
	}
	if e := shellinit.Select(result, name, filepath.Join(config.Expand(c.Runtime.AccountsDir), name), config.Expand(c.Runtime.SharedCodexHome)); e != nil {
		return e
	}
	return result.Close()
}
