package nativecli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A real detached PTY and a native fixture with a real writer lock. This tests
// adoption of a CLI launched by a shell, without any codexm launch receipt.
func TestDiscoveredShellCLIIsReusedWithoutRestart(t *testing.T) {
	if os.Getenv("CODEXM_TUI_TEST_BINARY") == "" {
		t.Skip("opt-in real tmux verification")
	}
	realHome := os.Getenv("HOME")
	c := fixture(t)
	home := os.Getenv("HOME")
	c.Runtime.CodexBinary = filepath.Join(home, "codex")
	source, _ := filepath.Abs("testdata/switch_fixture.go")
	build := exec.Command("go", "build", "-o", c.Runtime.CodexBinary, source)
	build.Env = append(os.Environ(), "HOME="+realHome)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("fixture build: %v %s", err, out)
	}
	ctx := context.Background()
	if err := ensureWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	// The shell remains the pane PID while the native CLI is its child.
	pane, err := tmux(ctx, "default", "new-window", "-d", "-P", "-F", "#{pane_id}", "-t", WorkspaceSession+":", "-n", "verify-discovery", "-e", "HOME="+home, "-e", "CODEX_SQLITE_HOME="+c.Runtime.SharedCodexHome, "/bin/sh", "-c", `"$1" resume "$2"; exit $?`, "fixture-shell", c.Runtime.CodexBinary, fixtureID)
	if err != nil {
		t.Fatal(err)
	}
	pane = strings.TrimSpace(pane)
	t.Cleanup(func() { _, _ = tmux(context.Background(), "default", "kill-pane", "-t", pane) })
	var found *Launch
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		found, err = discoverTerminal(ctx, c, fixtureID)
		if err == nil && found != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil || found == nil {
		t.Fatalf("CLI not found: %v", err)
	}
	if found.Pane != pane || found.TmuxSession != WorkspaceSession || !found.External || found.Account != "running" {
		t.Fatalf("wrong location: %+v", found)
	}
	if !sameProcess(ctx, *found.Owner) {
		t.Fatal("discovery restarted the CLI")
	}
	before, _ := os.ReadFile(filepath.Join(home, "events.jsonl"))
	reused, err := ReusableTerminal(ctx, c, fixtureID, "default")
	if err != nil || reused == nil || reused.Owner.PID != found.Owner.PID {
		t.Fatalf("cannot reuse: %+v %v", reused, err)
	}
	after, _ := os.ReadFile(filepath.Join(home, "events.jsonl"))
	if string(before) != string(after) {
		t.Fatal("reconnect submitted work or restarted the CLI")
	}
	if err := Close(ctx, c, reused.Session); err == nil {
		t.Fatal("discovery accidentally granted termination authority")
	}
	if !sameProcess(ctx, *found.Owner) {
		t.Fatal("external CLI was stopped")
	}
	// An unrelated CLI must not be selected for another conversation UUID.
	if other, err := discoverTerminal(ctx, c, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"); err != nil || other != nil {
		t.Fatalf("wrong chat matched: %+v %v", other, err)
	}
	t.Logf("reused existing PID %s in %s without resume/input", strconv.Itoa(found.Owner.PID), pane)
}
