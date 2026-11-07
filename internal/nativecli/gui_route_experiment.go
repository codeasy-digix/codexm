//go:build gui_experiment

package nativecli

// The experimental GUI router reuses the established account environment and
// terminal launcher. It deliberately cannot enter prepareInteractive (which
// offers whole-GUI restart) or release an existing writer.
import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

type GUIRoute struct {
	Route   string `json:"route"`
	Thread  string `json:"thread_id"`
	Account string `json:"account"`
	Detail  string `json:"detail"`
	App     string `json:"app,omitempty"`
}

func validateGUIAccount(c config.Config, account string) error {
	if account == "default" {
		return nil
	}
	if slices.Contains(c.AccountRegistry.Removed, account) {
		return errors.New("selected account was removed from the registry; add it again before using it")
	}
	return accounts.ValidateNativeAccount(c, account)
}

// InspectGUIRoute is read-only. A saved nickname is not evidence of the GUI's
// in-memory login, so every explicit --account requests the native CLI path.
func InspectGUIRoute(ctx context.Context, c config.Config, id, requested string) (GUIRoute, error) {
	r := GUIRoute{Thread: id, Account: requested}
	if !threadID.MatchString(id) {
		return r, errors.New("a complete chat UUID is required")
	}
	if requested != "" && requested != "default" {
		if err := validateGUIAccount(c, requested); err != nil {
			return r, err
		}
	}
	list, err := List(ctx, c)
	if err != nil {
		return r, err
	}
	found := false
	for _, chat := range list.Sessions {
		if chat.ID == id {
			found = true
			if requested == "" {
				r.Account = "default"
				if chat.Account != "" && chat.Account != "default" && validateGUIAccount(c, chat.Account) == nil {
					r.Account = chat.Account
				}
			}
			break
		}
	}
	if !found {
		return r, errors.New("chat is not in this machine's local conversation index")
	}
	owners, err := writerProcesses(ctx, config.Expand(c.Runtime.SharedCodexHome), id)
	if err != nil && !os.IsNotExist(err) {
		return r, err
	}
	if len(owners) > 1 {
		return r, errors.New("multiple writers found; no route was selected")
	}
	if len(owners) == 1 {
		if owner, err := findGUIOwner(ctx, c, id); err == nil {
			home, homeErr := os.UserHomeDir()
			native, nativeErr := filepath.EvalSymlinks(filepath.Join(home, ".codex"))
			shared, sharedErr := filepath.EvalSymlinks(config.Expand(c.Runtime.SharedCodexHome))
			if homeErr != nil || nativeErr != nil || sharedErr != nil || native != shared {
				return r, errors.New("GUI routing requires the desktop's native ~/.codex store; the configured catalog differs")
			}
			r.App = owner.App
			if requested == "" {
				r.Route, r.Account = "gui", "gui-current"
				r.Detail = "The verified GUI already owns this chat; keep its current login."
			} else {
				r.Route = "needs-gui-release"
				r.Detail = "Archive this chat in its owning GUI, then rerun this command. Only an owner-side release permits the selected account; the GUI will not be restarted."
			}
			return r, nil
		}
		r.Route = "existing-writer"
		r.Detail = "Reconnect only to a verified interactive CLI; an explicit account must match its recorded live login. Other writers are preserved."
		return r, nil
	}
	r.Route, r.Detail = "cli", "Resume the same ID in the official CLI with the selected or last registered account; no GUI restart."
	return r, nil
}

// OpenGUIRouteCLI never asks to stop the GUI and never archives a live chat.
// The durable launch also requires a free writer, covering the preparation race.
func OpenGUIRouteCLI(ctx context.Context, c config.Config, self string, route GUIRoute, explicit bool) (Launch, error) {
	if route.Route == "needs-gui-release" || route.Route == "gui" {
		return Launch{}, errors.New(route.Detail)
	}
	if existing, err := ReusableTerminal(ctx, c, route.Thread, route.Account); err != nil {
		return Launch{}, err
	} else if existing != nil {
		if explicit {
			last, err := readLast(c, route.Thread)
			key, keyErr := authKey(c, route.Account)
			if err != nil || keyErr != nil || last.AuthKey == "" || key != last.AuthKey || verifyOwnedCLI(ctx, c, last) != nil {
				return Launch{}, errors.New("the existing CLI's live login cannot be verified as the requested account; use an explicit codexm handoff or release it in its owner; no input was sent")
			}
		}
		return *existing, nil
	}
	if cr.WriterBusy(config.Expand(c.Runtime.SharedCodexHome), route.Thread) {
		return Launch{}, errors.New("chat still has a writer; release only this chat in its owning GUI/CLI, then retry; this experiment never restarts the GUI or stops another CLI")
	}
	return openContinuation(ctx, c, self, route.Thread, route.Account, false, "", true)
}

func experimentalNewConfig(c config.Config, account, cwd string) (string, string, error) {
	if account == "" {
		account = "default"
	}
	if account != "default" {
		if err := validateGUIAccount(c, account); err != nil {
			return "", "", err
		}
		if err := cr.EnsureAccountHome(c, account); err != nil {
			return "", "", err
		}
	}
	if !filepath.IsAbs(cwd) {
		return "", "", errors.New("new chat requires an absolute working directory")
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return "", "", errors.New("new chat working directory is unavailable")
	}
	r, err := Resolver(c)
	return account, r.Binary, err
}

// RunGUIRouteNew is a one-shot official CLI exec inside the created tmux pane.
// Native Codex creates the ID. There is no fabricated catalog row or prompt.
func RunGUIRouteNew(c config.Config, account, cwd string) error {
	account, binary, err := experimentalNewConfig(c, account, cwd)
	if err != nil {
		return err
	}
	if err := os.Chdir(cwd); err != nil {
		return err
	}
	return syscall.Exec(binary, []string{binary, "--no-daemon", "--no-alt-screen"}, environment(c, account))
}

func OpenGUIRouteNew(ctx context.Context, c config.Config, self, account, cwd string) (Launch, error) {
	l := Launch{Cwd: cwd, TmuxServer: CodexTmuxServer, TmuxSession: WorkspaceSession}
	var err error
	l.Account, l.Binary, err = experimentalNewConfig(c, account, cwd)
	if err != nil {
		return l, err
	}
	if err := ensureWorkspace(ctx); err != nil {
		return l, err
	}
	label, err := newViewerName()
	if err != nil {
		return l, err
	}
	args := []string{"new-window", "-d", "-P", "-F", "#{pane_id}", "-t", WorkspaceSession + ":", "-n", strings.Replace(label, "codexm-view-", "new-", 1), "-c", cwd}
	// Same environment boundary as the established launcher. The helper resolves
	// auth in the child, never in shell text or tmux's persistent environment.
	for _, key := range []string{"PATH", "HOME", "LANG", "LC_ALL", "LC_CTYPE", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
		if value, ok := os.LookupEnv(key); ok {
			args = append(args, "-e", key+"="+value)
		}
	}
	args = append(args, self, "--config", c.Path, "gui", "__new", l.Account, cwd)
	pane, err := tmux(ctx, CodexTmuxServer, args...)
	if err != nil {
		return l, err
	}
	l.Pane = strings.TrimSpace(pane)
	window, err := tmux(ctx, CodexTmuxServer, "display-message", "-p", "-t", l.Pane, "#{window_id}")
	l.Window = strings.TrimSpace(window)
	// From this point the native process may already own a chat. Never clean up
	// its pane on an observation/attachment failure.
	return l, err
}

// New chats have no ID/receipt yet. Reuse the same independent viewer mechanics
// without inventing a fake native conversation just to satisfy Attach's catalog.
func AttachGUIRouteNew(ctx context.Context, l Launch) error {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	socket, err := tmux(ctx, CodexTmuxServer, "display-message", "-p", "-t", l.Pane, "#{socket_path}")
	if err != nil {
		return err
	}
	command, env := attachMode(os.Getenv("TMUX"), strings.TrimSpace(socket), os.Environ())
	if command == "switch-client" {
		client, err := invokingClient(ctx, os.Getenv("TMUX_PANE"))
		if err != nil {
			return fmt.Errorf("native CLI remains in codexm window %s: %w", l.Window, err)
		}
		return switchClientToViewer(ctx, client, l)
	}
	name, err := newViewerName()
	if err != nil {
		return err
	}
	return syscall.Exec(bin, append([]string{bin}, tmuxArgs(CodexTmuxServer, viewerAttachArgs(name, l)...)...), env)
}
