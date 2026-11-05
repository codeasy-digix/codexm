package nativecli

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
)

// Real tmux group/client semantics, isolated from the user's default socket,
// config and credentials. Control clients exercise the same attach lifecycle
// without adding a PTY dependency or sending any Codex input.
func isolatedViewerWorkspace(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required for viewer integration tests")
	}
	// Keep the Unix socket path below macOS's length limit.
	dir, err := os.MkdirTemp("/tmp", "codexm-view-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("TERM", "xterm-256color")
	viewerTmux(t, "-f", "/dev/null", "new-session", "-d", "-s", WorkspaceSession, "-n", "home", "/bin/sleep", "120")
	t.Cleanup(func() { _, _ = tmux(context.Background(), "default", "kill-server") })
}

func viewerTmux(t *testing.T, args ...string) string {
	t.Helper()
	out, err := tmux(context.Background(), "default", args...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out)
}

func awaitViewer(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("tmux viewer state did not converge")
}

type viewerClient struct {
	name string
	cmd  *exec.Cmd
	in   io.WriteCloser
	done chan error
}

func startViewer(t *testing.T, l Launch) *viewerClient {
	t.Helper()
	name, err := newViewerName()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("tmux", tmuxArgs("default", append([]string{"-C"}, viewerAttachArgs(name, l)...)...)...)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(t.TempDir(), "client.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &viewerClient{name: name, cmd: cmd, in: in, done: make(chan error, 1)}
	go func() { c.done <- cmd.Wait(); close(c.done) }()
	t.Cleanup(func() {
		if t.Failed() {
			data, _ := os.ReadFile(log.Name())
			t.Logf("viewer client: %s", data)
			state, _ := tmux(context.Background(), "default", "list-clients", "-F", "#{client_name}|#{session_name}|#{pane_id}")
			t.Logf("clients: %s", state)
		}
		_ = in.Close()
		select {
		case <-c.done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-c.done
		}
		_ = log.Close()
	})
	awaitViewer(t, func() bool {
		out, _ := tmux(context.Background(), "default", "list-clients", "-F", "#{session_name}|#{pane_id}")
		return strings.Contains(out, name+"|"+l.Pane+"\n")
	})
	return c
}

func viewerJob(t *testing.T, name string) Launch {
	t.Helper()
	info := viewerTmux(t, "new-window", "-d", "-P", "-F", "#{window_id}|#{pane_id}", "-t", WorkspaceSession+":", "-n", name, "/bin/sleep", "120")
	f := strings.Split(info, "|")
	return Launch{TmuxServer: "default", TmuxSession: WorkspaceSession, Window: f[0], Pane: f[1]}
}

func TestIndependentWorkspaceViewers(t *testing.T) {
	isolatedViewerWorkspace(t)
	a, b := viewerJob(t, "chat-a"), viewerJob(t, "chat-b")
	before := viewerTmux(t, "list-panes", "-s", "-t", "=codexm", "-F", "#{window_id}|#{pane_id}|#{pane_pid}")
	anchor := viewerTmux(t, "display-message", "-p", "-t", "=codexm:", "#{window_id}")
	first, second := startViewer(t, a), startViewer(t, b)
	assertViews := func(aPane, bPane string) {
		t.Helper()
		if viewerTmux(t, "display-message", "-p", "-t", "="+first.name+":", "#{pane_id}") != aPane || viewerTmux(t, "display-message", "-p", "-t", "="+second.name+":", "#{pane_id}") != bPane {
			t.Fatal("one client changed the other client's selected chat")
		}
		if viewerTmux(t, "display-message", "-p", "-t", "=codexm:", "#{window_id}") != anchor {
			t.Fatal("viewer changed the workspace's selected window")
		}
	}
	assertViews(a.Pane, b.Pane)
	viewerTmux(t, "select-window", "-t", "="+second.name+":"+a.Window)
	assertViews(a.Pane, a.Pane) // Same chat shares exactly the same pane/process.
	viewerTmux(t, "select-window", "-t", "="+second.name+":"+b.Window)
	assertViews(a.Pane, b.Pane)
	if got := viewerTmux(t, "display-message", "-p", "-t", paneTarget(a), "#{session_name}"); got != WorkspaceSession {
		t.Fatal("durable pane lookup resolved to a viewer", got)
	}
	_ = first.in.Close()
	awaitViewer(t, func() bool {
		_, err := tmux(context.Background(), "default", "has-session", "-t", "="+first.name)
		return err != nil
	})
	third := startViewer(t, a)
	// Abrupt terminal/client loss must also reap just its view.
	_ = third.cmd.Process.Kill()
	awaitViewer(t, func() bool {
		_, err := tmux(context.Background(), "default", "has-session", "-t", "="+third.name)
		return err != nil
	})
	if got := viewerTmux(t, "list-panes", "-s", "-t", "=codexm", "-F", "#{window_id}|#{pane_id}|#{pane_pid}"); got != before {
		t.Fatal("attach/reconnect/detach changed a job or its PTY")
	}
	if viewerTmux(t, "display-message", "-p", "-t", "="+second.name+":", "#{pane_id}") != b.Pane {
		t.Fatal("disconnect moved the other terminal")
	}
	_ = second.in.Close()
	awaitViewer(t, func() bool { return viewerTmux(t, "list-sessions", "-F", "#{session_name}") == WorkspaceSession })
}

func TestSwitchViewerAndFailedSwitchCleanup(t *testing.T) {
	isolatedViewerWorkspace(t)
	a, b := viewerJob(t, "chat-a"), viewerJob(t, "chat-b")
	first, second := startViewer(t, a), startViewer(t, b)
	client, err := invokingClient(context.Background(), a.Pane)
	if err != nil {
		t.Fatal(err)
	}
	if err = switchClientToViewer(context.Background(), client, b); err != nil {
		t.Fatal(err)
	}
	awaitViewer(t, func() bool {
		_, err := tmux(context.Background(), "default", "has-session", "-t", "="+first.name)
		return err != nil
	})
	if viewerTmux(t, "display-message", "-p", "-t", "="+second.name+":", "#{pane_id}") != b.Pane {
		t.Fatal("switch affected a different viewer")
	}
	if _, err = invokingClient(context.Background(), b.Pane); err == nil {
		t.Fatal("ambiguous shared input was allowed to switch an arbitrary client")
	}
	if _, err = invokingClient(context.Background(), a.Pane); err == nil {
		t.Fatal("detached picker was allowed to switch an unrelated client")
	}
	before := viewerTmux(t, "list-sessions", "-F", "#{session_id}")
	if err = switchClientToViewer(context.Background(), "nonexistent-viewer", a); err == nil {
		t.Fatal("missing client was accepted")
	}
	if after := viewerTmux(t, "list-sessions", "-F", "#{session_id}"); after != before {
		t.Fatal("failed switch left an orphan view")
	}
}

func TestNoninteractiveViewerDoesNotLeakSession(t *testing.T) {
	isolatedViewerWorkspace(t)
	a := viewerJob(t, "chat-a")
	name, _ := newViewerName()
	if _, err := tmux(context.Background(), "default", viewerAttachArgs(name, a)...); err == nil {
		t.Fatal("noninteractive client unexpectedly attached")
	}
	if got := viewerTmux(t, "list-sessions", "-F", "#{session_name}"); got != WorkspaceSession {
		t.Fatal("failed terminal open left an orphan viewer", got)
	}
}

func TestHandoffKeepsIndependentViewers(t *testing.T) {
	isolatedViewerWorkspace(t)
	a, b, next := viewerJob(t, "old-chat"), viewerJob(t, "other-chat"), viewerJob(t, "replacement")
	a.Session = "codexm-cli-0123456789abcdef"
	c := config.Config{Path: filepath.Join(t.TempDir(), "config.toml")}
	viewerTmux(t, "set-option", "-w", "-t", a.Pane, "@codexm-terminal", a.Session)
	viewerTmux(t, "set-option", "-w", "-t", a.Pane, "@codexm-config", c.Path)
	first, second, neighbor := startViewer(t, a), startViewer(t, a), startViewer(t, b)
	before := viewerTmux(t, "display-message", "-p", "-t", paneTarget(b), "#{pane_pid}")
	pid, err := strconv.Atoi(viewerTmux(t, "display-message", "-p", "-t", paneTarget(a), "#{pane_pid}"))
	if err != nil {
		t.Fatal(err)
	}
	a.Owner = &processIdentity{PID: pid}
	viewerTmux(t, "set-option", "-w", "-t", a.Pane, "remain-on-exit", "on")
	// Stop only this test's /bin/sleep, representing a verified completed handoff.
	if err = syscall.Kill(pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	awaitViewer(t, func() bool { return viewerTmux(t, "display-message", "-p", "-t", paneTarget(a), "#{pane_dead}") == "1" })
	followTerminal(context.Background(), c, a, next)
	awaitViewer(t, func() bool {
		clients := viewerTmux(t, "list-clients", "-F", "#{session_name}|#{pane_id}")
		return strings.Count(clients, "|"+next.Pane) == 2 && strings.Contains(clients, neighbor.name+"|"+b.Pane) && !strings.Contains(clients, first.name) && !strings.Contains(clients, second.name)
	})
	if got := viewerTmux(t, "display-message", "-p", "-t", paneTarget(b), "#{pane_pid}"); got != before {
		t.Fatal("handoff restarted another chat")
	}
	panes := viewerTmux(t, "list-panes", "-s", "-t", "=codexm", "-F", "#{pane_id}")
	if strings.Contains("\n"+panes+"\n", "\n"+a.Pane+"\n") {
		t.Fatal("completed handoff left the old pane behind")
	}
}

func TestScreenUsesDurableWorkspaceWithActiveViewers(t *testing.T) {
	isolatedViewerWorkspace(t)
	c := fixture(t)
	l := viewerJob(t, "owned-chat")
	l.Thread, l.Account, l.Ready = fixtureID, "default", true
	l.Session = terminalName(c, l.Thread, l.Account)
	viewerTmux(t, "set-option", "-w", "-t", l.Pane, "@codexm-terminal", l.Session)
	viewerTmux(t, "set-option", "-w", "-t", l.Pane, "@codexm-config", c.Path)
	if err := saveSession(c, l); err != nil {
		t.Fatal(err)
	}
	first, second := startViewer(t, l), startViewer(t, l)
	for i := 0; i < 2; i++ {
		got, err := Screen(context.Background(), c, l.Session)
		if err != nil || !got.Alive || got.Launch.TmuxSession != WorkspaceSession {
			t.Fatalf("active viewer changed durable ownership: %+v %v", got, err)
		}
		_ = first.in.Close()
	}
	_ = second.in.Close()
}

// Execute the production Attach path in two real, distinct PTYs. The outer
// terminals and all job processes belong to this test's private tmux server.
func TestViewerAttachThroughSeparatePTYs(t *testing.T) {
	isolatedViewerWorkspace(t)
	c := fixture(t)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	a, b := viewerJob(t, "chat-a"), viewerJob(t, "chat-b")
	for i, l := range []*Launch{&a, &b} {
		l.Thread, l.Account, l.Ready = fixtureID, "default", true
		l.Session = "codexm-cli-" + strings.Repeat(strconv.Itoa(i+1), 16)
		viewerTmux(t, "set-option", "-w", "-t", l.Pane, "@codexm-terminal", l.Session)
		viewerTmux(t, "set-option", "-w", "-t", l.Pane, "@codexm-config", c.Path)
		if err := saveSession(c, *l); err != nil {
			t.Fatal(err)
		}
	}
	jobs := viewerTmux(t, "list-panes", "-s", "-t", "=codexm", "-F", "#{pane_id}|#{pane_pid}")
	viewerTmux(t, "new-session", "-d", "-s", "test-terminals", "/bin/sleep", "120")
	clientTTYs := map[string]string{}
	var terminals []string
	for _, l := range []Launch{a, b} {
		terminal := viewerTmux(t, "new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "test-terminals:", "-e", "PATH="+os.Getenv("PATH"), "/usr/bin/env", "-u", "TMUX", "-u", "TMUX_PANE",
			"CODEXM_VIEWER_TEST_CONFIG="+c.Path, "CODEXM_VIEWER_TEST_SESSION="+l.Session, os.Args[0], "-test.run=^TestViewerAttachHelper$")
		terminals = append(terminals, terminal)
		tty := viewerTmux(t, "display-message", "-p", "-t", terminal, "#{pane_tty}")
		clientTTYs[tty] = l.Pane
		awaitViewer(t, func() bool {
			clients := viewerTmux(t, "list-clients", "-F", "#{client_tty}|#{pane_id}")
			for tty, pane := range clientTTYs {
				if !strings.Contains(clients, tty+"|"+pane) {
					return false
				}
			}
			return true
		})
	}
	for _, terminal := range terminals {
		viewerTmux(t, "kill-pane", "-t", terminal)
	}
	awaitViewer(t, func() bool {
		return !strings.Contains(viewerTmux(t, "list-sessions", "-F", "#{session_name}"), "codexm-view-")
	})
	if got := viewerTmux(t, "list-panes", "-s", "-t", "=codexm", "-F", "#{pane_id}|#{pane_pid}"); got != jobs {
		t.Fatal("real terminal loss restarted or removed native work")
	}
}

func TestViewerAttachHelper(t *testing.T) {
	file := os.Getenv("CODEXM_VIEWER_TEST_CONFIG")
	if file == "" {
		return
	}
	c, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if err = Attach(context.Background(), c, os.Getenv("CODEXM_VIEWER_TEST_SESSION")); err != nil {
		t.Fatal(err)
	}
}
