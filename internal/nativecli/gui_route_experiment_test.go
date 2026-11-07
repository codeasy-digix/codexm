//go:build gui_experiment

package nativecli

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	cr "github.com/codeasy-digix/codexm/internal/runtime"

	"golang.org/x/sys/unix"
)

func TestGUIHybridPlanPreservesNativeStateAndFallsBackFromRemovedAccount(t *testing.T) {
	c := fixture(t)
	ctx := context.Background()
	if err := writeReceipt(c, Launch{Thread: fixtureID, Account: "removed", Ready: true}); err != nil {
		t.Fatal(err)
	}
	route, err := InspectGUIRoute(ctx, c, fixtureID, "")
	if err != nil || route.Route != "cli" || route.Account != "default" {
		t.Fatal(route, err)
	}
	saveToolbarAuth(t, c, "sample-b", "test-user", "test-workspace")
	if err := writeReceipt(c, Launch{Thread: fixtureID, Account: "sample-b", Ready: true}); err != nil {
		t.Fatal(err)
	}
	route, err = InspectGUIRoute(ctx, c, fixtureID, "")
	if err != nil || route.Account != "sample-b" {
		t.Fatal(route, err)
	}
	route, err = InspectGUIRoute(ctx, c, fixtureID, "default")
	if err != nil || route.Account != "default" {
		t.Fatal("explicit default must override remembered account", route, err)
	}
	c.AccountRegistry.Removed = []string{"sample-b"}
	route, err = InspectGUIRoute(ctx, c, fixtureID, "")
	if err != nil || route.Account != "default" {
		t.Fatal("removed registration must fall back even when auth is preserved", route, err)
	}
	if _, err = InspectGUIRoute(ctx, c, fixtureID, "sample-b"); err == nil {
		t.Fatal("explicit removed account was accepted")
	}
	for _, p := range []string{filepath.Join(os.Getenv("HOME"), "argv"), filepath.Join(c.Runtime.AccountsDir, "sample-b", "sessions")} {
		if _, e := os.Stat(p); !os.IsNotExist(e) {
			t.Fatal("planning unexpectedly ran a runtime or linked history", p)
		}
	}
}

func TestGUIHybridOfficialPTYRoutesNewAndResume(t *testing.T) {
	product := os.Getenv("CODEXM_GUI_EXPERIMENT_BINARY")
	if os.Getenv("CODEXM_GUI_CONTROL_TEST") != "1" || !filepath.IsAbs(product) {
		t.Skip("requires the isolated official runtime and experimental product binary")
	}
	f := newGUIControlFixture(t, "stdio")
	// The real interactive CLI cannot inherit the fixture's per-process env key
	// from a pre-existing tmux server. Configure only this temporary model fixture.
	path := filepath.Join(f.c.Runtime.SharedCodexHome, "config.toml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b = []byte(strings.Replace(string(b), "env_key='CODEXM_GUI_FIXTURE_KEY'\n", "http_headers = { Authorization = 'Bearer fixture-cli' }\n", 1))
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	f.c.Path = filepath.Join(f.cmd.Dir, "codexm.toml")
	if err := f.c.Save(); err != nil {
		t.Fatal(err)
	}
	saveToolbarAuth(t, f.c, "sample-b", "test-user", "test-workspace")
	id, neighbor := f.thread(), f.thread()
	f.start(id, "GUI_FIXTURE_SEED")
	f.completed(id, 1)
	key, _ := cr.NativeProcessIdentity(f.cmd.Process.Pid)
	route, err := InspectGUIRoute(f.ctx, f.c, id, "sample-b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenGUIRouteCLI(f.ctx, f.c, product, route, true); err == nil {
		t.Fatal("native launcher should preserve an existing stdio owner")
	}
	f.assertOwner(key, id, neighbor)
	if err := f.client.Call(f.ctx, "thread/archive", map[string]any{"threadId": id}, nil); err != nil {
		t.Fatal(err)
	}
	if err := cr.WaitWriterRelease(f.ctx, f.c.Runtime.SharedCodexHome, id); err != nil {
		t.Fatal(err)
	}
	route, err = InspectGUIRoute(f.ctx, f.c, id, "sample-b")
	if err != nil || route.Route != "cli" {
		t.Fatal(route, err)
	}
	l, err := OpenGUIRouteCLI(f.ctx, f.c, product, route, true)
	if err != nil {
		t.Fatal(err)
	}
	var owned []Launch
	owned = append(owned, l)
	t.Cleanup(func() {
		// Each pane was created by this isolated fixture; never remove the workspace.
		for _, p := range owned {
			_ = removeTerminal(context.Background(), p)
		}
	})
	var ready Launch
	for until := time.Now().Add(15 * time.Second); time.Now().Before(until); {
		ready, err = readLast(f.c, id)
		if err == nil && verifyOwnedCLI(f.ctx, f.c, ready) == nil {
			break
		}
		time.Sleep(80 * time.Millisecond)
	}
	if err != nil || verifyOwnedCLI(f.ctx, f.c, ready) != nil || ready.Account != "sample-b" || !ready.RequireFreeWriter || ready.Continue {
		screen, _ := Screen(f.ctx, f.c, l.Session)
		t.Fatal("selected account did not own the same ID", err, screen.Text)
	}
	// Reconnection must not start another CLI or add a continuation prompt.
	before, _ := cr.NativeProcessIdentity(ready.Owner.PID)
	again, err := OpenGUIRouteCLI(f.ctx, f.c, product, route, true)
	if err != nil || again.Pane != l.Pane {
		t.Fatal("same login did not reconnect", again, err)
	}
	saveToolbarAuth(t, f.c, "sample-c", "other-test-user", "test-workspace")
	wrong := route
	wrong.Account = "sample-c"
	if _, err := OpenGUIRouteCLI(f.ctx, f.c, product, wrong, true); err == nil {
		t.Fatal("explicit other login silently reconnected to B")
	}
	if after, _ := cr.NativeProcessIdentity(ready.Owner.PID); before != after {
		t.Fatal("reconnection replaced the native CLI")
	}
	// A separate fresh native TUI starts in the same durable workspace. Native
	// Codex owns ID creation; the launcher creates no extra conversation row.
	newCLI, err := OpenGUIRouteNew(f.ctx, f.c, product, "sample-b", f.cmd.Dir)
	if err != nil {
		t.Fatal(err)
	}
	owned = append(owned, newCLI)
	var view string
	for until := time.Now().Add(12 * time.Second); time.Now().Before(until); {
		view, _ = tmux(f.ctx, CodexTmuxServer, "capture-pane", "-p", "-t", newCLI.Pane)
		if strings.Contains(view, "Codex") && strings.Contains(view, "fixture") {
			break
		}
		time.Sleep(80 * time.Millisecond)
	}
	if !strings.Contains(view, "Codex") || !strings.Contains(view, "fixture") {
		t.Fatal("new native CLI did not render", view)
	}
	current, _ := cr.NativeProcessIdentity(f.cmd.Process.Pid)
	writers, err := writerProcesses(f.ctx, f.c.Runtime.SharedCodexHome, neighbor)
	if current != key || err != nil || len(writers) != 1 || writers[0] != f.cmd.Process.Pid || f.requests() != 1 {
		t.Fatal("new/resume route disturbed original owner or submitted implicit input", err)
	}
	t.Log("official tmux CLI resumed same ID with B, rejected wrong-account reconnect, created new TUI without a prompt, and preserved neighboring owner")
	if runtime.GOOS == "darwin" {
		testGUIHybridNewViewer(t, f, product)
	}
}

func testGUIHybridNewViewer(t *testing.T, f *guiControlFixture, product string) {
	t.Helper()
	cmd := exec.CommandContext(f.ctx, "/usr/bin/script", "-q", "/dev/null", product, "gui", "new", "--account", "sample-b", "--cwd", f.cmd.Dir)
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if key != "TMUX" && key != "TMUX_PANE" && key != "CODEXM_CONFIG" && key != "TERM" {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "CODEXM_CONFIG="+f.c.Path, "TERM=xterm-256color")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	reaped := false
	var owned Launch
	t.Cleanup(func() {
		in.Close()
		if owned.Pane != "" {
			_ = removeTerminal(context.Background(), owned)
		}
		// Only this script child; no signal to a user CLI or GUI.
		if !reaped {
			_ = cmd.Process.Kill()
			<-done
		}
	})
	var viewer string
	for until := time.Now().Add(12 * time.Second); time.Now().Before(until); {
		clients, _ := tmux(f.ctx, CodexTmuxServer, "list-clients", "-F", "#{client_pid}|#{session_name}|#{window_id}|#{pane_id}")
		for _, line := range strings.Split(strings.TrimSpace(clients), "\n") {
			parts := strings.Split(line, "|")
			if len(parts) != 4 || !viewerSessionName.MatchString(parts[1]) {
				continue
			}
			pid, _ := strconv.Atoi(parts[0])
			identity, e := inspectProcess(f.ctx, pid)
			if e == nil && processUnderPane(f.ctx, identity, cmd.Process.Pid) {
				viewer = parts[1]
				owned = Launch{TmuxServer: CodexTmuxServer, TmuxSession: WorkspaceSession, Window: parts[2], Pane: parts[3]}
				break
			}
		}
		if viewer != "" {
			break
		}
		time.Sleep(80 * time.Millisecond)
	}
	if viewer == "" {
		t.Fatal("new CLI did not attach through a dedicated grouped viewer")
	}
	// Detach this test client through its own PTY, never another client.
	if _, err := in.Write([]byte{'\x02', 'd'}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		reaped = true
		if err != nil {
			t.Fatal("viewer detach", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("viewer did not detach")
	}
	if _, err := tmux(f.ctx, CodexTmuxServer, "has-session", "-t", "="+viewer); err == nil {
		t.Fatal("disconnected viewer was not reaped")
	}
	status, err := tmux(f.ctx, CodexTmuxServer, "display-message", "-p", "-t", owned.Pane, "#{pane_dead}|#{pane_current_command}")
	if err != nil || strings.TrimSpace(status) != "0|codex" {
		t.Fatal("detaching the viewer stopped its native CLI", status, err)
	}
	t.Log("new-chat PTY attached an independent grouped viewer; detach removed only the viewer and preserved the native CLI")
}

func TestGUIHybridRefusesHeldWriterWithoutArchiveOrRestart(t *testing.T) {
	c := fixture(t)
	dir := filepath.Join(c.Runtime.SharedCodexHome, "thread-writer-locks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, fixtureID+".lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	route, err := InspectGUIRoute(ctx, c, fixtureID, "default")
	if err != nil || route.Route != "existing-writer" {
		t.Fatal(route, err)
	}
	if _, err := OpenGUIRouteCLI(ctx, c, "/unused", route, true); err == nil {
		t.Fatal("held writer was taken over")
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), "argv")); !os.IsNotExist(err) {
		t.Fatal("native mutation ran for a held writer")
	}
	if _, err := os.Stat(filepath.Join(dir, fixtureID+".lock")); err != nil {
		t.Fatal("native lock was removed", err)
	}
}
