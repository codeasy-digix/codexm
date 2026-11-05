package nativecli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/codeasy-digix/codexm/internal/config"
)

func windowLabel(l Launch) string {
	label := []rune(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, l.Title))
	if len(label) > 28 {
		label = label[:28]
	}
	if len(label) == 0 {
		return l.Session
	}
	return "chat-" + l.Thread[:8] + " " + string(label)
}

func containerSession(l Launch) string {
	if l.TmuxSession != "" {
		return l.TmuxSession
	}
	return l.Session // Older receipts used a separate session per conversation.
}

// Link an existing default-server window into the workspace. The original
// session and its clients remain intact; no process or PTY is recreated.
func workspaceView(ctx context.Context, l Launch) (Launch, error) {
	if l.TmuxServer != "" && l.TmuxServer != "default" {
		return l, nil
	}
	if err := ensureWorkspace(ctx); err != nil {
		return l, err
	}
	window, err := tmux(ctx, "default", "display-message", "-p", "-t", l.Pane, "#{window_id}")
	if err != nil {
		return l, err
	}
	window = strings.TrimSpace(window)
	windows, err := tmux(ctx, "default", "list-windows", "-t", WorkspaceSession, "-F", "#{window_id}")
	if err != nil {
		return l, err
	}
	if !strings.Contains("\n"+windows, "\n"+window+"\n") {
		if _, err = tmux(ctx, "default", "link-window", "-d", "-s", window, "-t", WorkspaceSession+":"); err != nil {
			return l, err
		}
	}
	l.TmuxSession, l.Window = WorkspaceSession, window
	return l, nil
}

func ensureWorkspace(ctx context.Context) error {
	if _, err := tmux(ctx, CodexTmuxServer, "has-session", "-t", "="+WorkspaceSession); err == nil {
		_, err = tmux(ctx, CodexTmuxServer, "set-option", "-t", WorkspaceSession, "destroy-unattached", "off")
		return err
	}
	_, err := tmux(ctx, CodexTmuxServer, "new-session", "-d", "-s", WorkspaceSession, "-n", "home")
	if err != nil {
		// Another launcher may have created the shared workspace concurrently.
		if _, check := tmux(ctx, CodexTmuxServer, "has-session", "-t", "="+WorkspaceSession); check == nil {
			err = nil
		}
	}
	if err == nil {
		_, err = tmux(ctx, CodexTmuxServer, "set-option", "-t", WorkspaceSession, "destroy-unattached", "off")
	}
	return err
}

func removeTerminal(ctx context.Context, l Launch) error {
	// Never kill a shared session, even for a legacy receipt whose window moved.
	_, err := tmux(ctx, l.TmuxServer, "kill-pane", "-t", l.Pane)
	return err
}

// tmux cannot transfer PTYs across servers. A workspace connection window keeps
// the old process alive and gives the user one entrypoint during that transition.
func legacyPortal(ctx context.Context, l Launch) (Launch, error) {
	if err := ensureWorkspace(ctx); err != nil {
		return l, err
	}
	panes, err := tmux(ctx, "default", "list-panes", "-s", "-t", WorkspaceSession, "-F", "#{pane_id}|#{@codexm-source-server}|#{@codexm-source-pane}|#{pane_dead}")
	if err != nil {
		return l, err
	}
	for _, line := range strings.Split(strings.TrimSpace(panes), "\n") {
		f := strings.Split(line, "|")
		if len(f) == 4 && f[1] == l.TmuxServer && f[2] == l.Pane && f[3] == "0" {
			l.Pane, l.TmuxServer, l.TmuxSession = f[0], "default", WorkspaceSession
			l.Window = ""
			return l, nil
		}
	}
	bin, err := exec.LookPath("tmux")
	if err != nil {
		return l, err
	}
	pane, err := tmux(ctx, "default", "new-window", "-d", "-P", "-F", "#{pane_id}", "-t", WorkspaceSession+":", "-n", "legacy-"+l.Thread[:8], "/usr/bin/env", "-u", "TMUX", bin, "-L", l.TmuxServer, "attach-session", "-t", l.Pane)
	if err != nil {
		return l, err
	}
	pane = strings.TrimSpace(pane)
	for key, value := range map[string]string{"@codexm-source-server": l.TmuxServer, "@codexm-source-pane": l.Pane} {
		if _, err = tmux(ctx, "default", "set-option", "-w", "-t", pane, key, value); err != nil {
			return l, err
		}
	}
	l.Pane, l.TmuxServer, l.TmuxSession = pane, "default", WorkspaceSession
	l.Window = ""
	return l, nil
}

// A pane may start a login shell or npm wrapper. Follow verified parent PIDs;
// names, pane titles and session age are not evidence of a conversation owner.
func processUnderPane(ctx context.Context, owner processIdentity, panePID int) bool {
	for i := 0; i < 24; i++ {
		if owner.PID == panePID {
			return true
		}
		if owner.Parent <= 1 {
			return false
		}
		var err error
		owner, err = inspectProcess(ctx, owner.Parent)
		if err != nil {
			return false
		}
	}
	return false
}

// Discover a standard interactive CLI, including one launched outside codexm.
// This is read-only and grants no termination or account-handoff authority.
func discoverTerminal(ctx context.Context, c config.Config, id string) (*Launch, error) {
	owners, err := writerProcesses(ctx, config.Expand(c.Runtime.SharedCodexHome), id)
	if os.IsNotExist(err) || (err == nil && len(owners) == 0) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(owners) != 1 {
		return nil, errors.New("multiple chat writers found; no terminal was selected")
	}
	owner, err := inspectProcess(ctx, owners[0])
	if err != nil {
		return nil, err
	}
	if filepath.Base(owner.Executable) != "codex" {
		return nil, nil
	}
	// Reject shared app-server/daemon ownership even if it was started in tmux.
	argv, err := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(owner.PID), "-o", "args=").Output()
	if err != nil {
		return nil, err
	}
	for _, arg := range strings.Fields(string(argv)) {
		if arg == "app-server" || arg == "daemon" || arg == "exec" {
			return nil, nil
		}
	}
	files, err := exec.CommandContext(ctx, "lsof", "-nP", "-a", "-p", strconv.Itoa(owner.PID), "-F", "pn").Output()
	if err != nil {
		return nil, errors.New("cannot verify the CLI's loaded conversations")
	}
	lockDir, err := filepath.EvalSymlinks(filepath.Join(config.Expand(c.Runtime.SharedCodexHome), "thread-writer-locks"))
	if err != nil {
		return nil, err
	}
	loaded := map[string]bool{}
	for _, line := range strings.Split(string(files), "\n") {
		if !strings.HasPrefix(line, "n") {
			continue
		}
		file := strings.TrimPrefix(line, "n")
		name := strings.TrimSuffix(filepath.Base(file), ".lock")
		if filepath.Dir(file) == lockDir && threadID.MatchString(name) {
			loaded[name] = true
		}
	}
	if len(loaded) != 1 || !loaded[id] {
		return nil, errors.New("CLI has another or multiple loaded chats; no terminal was selected")
	}
	var found *Launch
	for _, server := range []string{"default", "codex"} {
		panes, e := tmux(ctx, server, "list-panes", "-a", "-F", "#{session_name}|#{window_id}|#{pane_id}|#{pane_pid}|#{pane_dead}")
		if e != nil {
			continue
		} // An unused legacy socket need not exist.
		lines := strings.Split(strings.TrimSpace(panes), "\n")
		// Group members repeat the same pane. Receipts must outlive viewers.
		sort.SliceStable(lines, func(i, j int) bool {
			return strings.HasPrefix(lines[i], WorkspaceSession+"|") && !strings.HasPrefix(lines[j], WorkspaceSession+"|")
		})
		seen := map[string]bool{}
		for _, line := range lines {
			parts := strings.Split(line, "|")
			if len(parts) != 5 || parts[4] != "0" || seen[parts[2]] || viewerSessionName.MatchString(parts[0]) {
				continue
			}
			seen[parts[2]] = true
			pid, e := strconv.Atoi(parts[3])
			if e != nil || !processUnderPane(ctx, owner, pid) {
				continue
			}
			if found != nil {
				return nil, errors.New("chat matches more than one terminal; no terminal was selected")
			}
			l := Launch{Thread: id, Account: "running", Binary: owner.Executable, Ready: true, External: true, TmuxServer: server, TmuxSession: parts[0], Window: parts[1], Pane: parts[2], Owner: &owner}
			if last, e := readLast(c, id); e == nil && last.Owner.PID == owner.PID && last.Owner.Started == owner.Started {
				l.Account = last.Account
			}
			l.Session = terminalName(c, id, l.Account)
			found = &l
		}
	}
	if found == nil {
		return nil, nil
	}
	if !sameProcess(ctx, owner) {
		return nil, errors.New("CLI exited while locating its terminal; retry")
	}
	if err := saveSession(c, *found); err != nil {
		return nil, fmt.Errorf("record terminal location: %w", err)
	}
	return found, nil
}
