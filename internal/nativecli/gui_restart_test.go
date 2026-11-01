package nativecli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/codeasy-digix/codexm/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGUIRestartRequiresFreshInteractiveConsent(t *testing.T) {
	owner := guiOwner{App: "/Applications/ChatGPT.app", Process: processIdentity{PID: 123}}
	for _, tt := range []struct {
		input             string
		interactive, want bool
	}{
		{"yes\n", true, true}, {"Y\n", true, true}, {"예\n", true, true},
		{"\n", true, false}, {"n\n", true, false}, {"yes", true, false}, {"", true, false},
		{"yes\n", false, false}, {"always\n", true, false},
	} {
		var out bytes.Buffer
		got := confirmGUIRestart(strings.NewReader(tt.input), &out, tt.interactive, fixtureID, owner)
		if got != tt.want {
			t.Fatalf("input=%q interactive=%v got=%v", tt.input, tt.interactive, got)
		}
		if tt.interactive && (!strings.Contains(out.String(), "다른 GUI 대화") || !strings.Contains(out.String(), "자동 재개되지") || !strings.Contains(out.String(), fixtureID)) {
			t.Fatal("missing affected work / chat warning")
		}
		if !tt.interactive && out.Len() != 0 {
			t.Fatal("noninteractive launch attempted a prompt")
		}
	}
	// A previous yes does not change the next invocation's default.
	if confirmGUIRestart(strings.NewReader("\n"), &bytes.Buffer{}, true, fixtureID, owner) {
		t.Fatal("consent persisted")
	}
}
func TestGUIHandoffAbortsBeforeResumeAndArmsRestorationOnFailure(t *testing.T) {
	for _, stage := range []string{"changed-owner", "signal-failed", "writer-busy", "prepare-failed", "success"} {
		t.Run(stage, func(t *testing.T) {
			g := &guiRecovery{dir: t.TempDir()}
			sequence := []string{}
			err := executeGUIHandoff(g, guiHandoffActions{
				verify: func() bool { sequence = append(sequence, "verify"); return stage != "changed-owner" },
				stop: func() error {
					sequence = append(sequence, "stop")
					if !markerExists(filepath.Join(g.dir, "stop-requested")) {
						t.Fatal("recovery not armed before shutdown")
					}
					if stage == "signal-failed" {
						return errors.New("denied")
					}
					return nil
				},
				wait: func() error {
					sequence = append(sequence, "wait")
					if stage == "writer-busy" {
						return errors.New("busy")
					}
					return nil
				},
				prepare: func() error {
					sequence = append(sequence, "prepare")
					if stage == "prepare-failed" {
						return errors.New("unarchive failed")
					}
					return nil
				},
			})
			expected := map[string]string{"changed-owner": "verify", "signal-failed": "verify,stop", "writer-busy": "verify,stop,wait", "prepare-failed": "verify,stop,wait,prepare", "success": "verify,stop,wait,prepare"}[stage]
			if strings.Join(sequence, ",") != expected {
				t.Fatalf("wrong sequence: %v", sequence)
			}
			if (err == nil) != (stage == "success") || markerExists(filepath.Join(g.dir, "failed")) != (stage != "success") {
				t.Fatalf("failure lost: %v", err)
			}
			if stage == "changed-owner" && markerExists(filepath.Join(g.dir, "stop-requested")) {
				t.Fatal("stale owner was stopped")
			}
		})
	}
}
func TestGUIRecoveryWaitsForCLIAndRestoresOnFailures(t *testing.T) {
	cases := []struct {
		name                                        string
		stopped, caller, gui, failed, timeout, owns bool
		want                                        string
	}{
		{"armed-before-shutdown", false, true, true, false, false, false, "wait"},
		{"cancelled-before-shutdown", false, true, true, true, false, false, "cancel"},
		{"caller-crashed-before-shutdown", false, false, true, false, false, false, "cancel"},
		{"wait-for-native-lock", true, true, false, false, false, false, "wait"},
		{"CLI-acquired-lock", true, true, false, false, false, true, "reopen"},
		{"CLI-launch-failed", true, true, false, true, false, false, "reopen"},
		{"caller-crashed-after-shutdown", true, false, false, false, false, false, "reopen"},
		{"CLI-startup-timeout", true, true, false, false, true, false, "reopen"},
		{"GUI-ignores-termination", true, true, true, true, false, false, "still-running"},
		{"GUI-not-exited-yet", true, true, true, false, false, true, "wait"},
	}
	for _, tt := range cases {
		if got := guiRecoveryAction(tt.stopped, tt.caller, tt.gui, tt.failed, tt.timeout, tt.owns); got != tt.want {
			t.Errorf("%s: %s", tt.name, got)
		}
	}
}
func TestGUIRecoveryRejectsUnownedOrStaleRequests(t *testing.T) {
	c := fixture(t)
	for _, file := range []string{filepath.Join(t.TempDir(), "request.json"), filepath.Join(root(c), "gui-restarts", "../../outside", "request.json")} {
		if err := RunGUIReopen(context.Background(), c, file); err == nil {
			t.Fatal("unowned recovery accepted")
		}
	}
	dir := filepath.Join(root(c), "gui-restarts", "codexm-gui-reopen-1234567890abcdef")
	os.MkdirAll(dir, 0700)
	spec := guiRestartSpec{Thread: fixtureID, Shared: c.Runtime.SharedCodexHome, Session: filepath.Base(dir), Created: 1}
	file := filepath.Join(dir, "request.json")
	writeJSON(file, spec)
	if err := RunGUIReopen(context.Background(), c, file); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale request accepted: %v", err)
	}
}
func TestGUIOwnerReadOnlyProbe(t *testing.T) {
	id := os.Getenv("CODEXM_GUI_OWNER_TEST_ID")
	if id == "" {
		t.Skip("opt-in read-only inspection of a GUI-owned thread")
	}
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := findGUIOwner(context.Background(), c, id)
	if err != nil {
		t.Fatal(err)
	}
	if !sameProcess(context.Background(), owner.Process) {
		t.Fatal("GUI changed during inspection")
	}
	changed := owner.Process
	changed.Started = "old incarnation"
	if sameProcess(context.Background(), changed) {
		t.Fatal("reused PID accepted")
	}
	t.Logf("verified GUI PID %d and writer PID %d; no signals sent", owner.Process.PID, owner.Writer.PID)
}

// Exercise the actual bounded helper handshake without ever requesting GUI stop.
func TestGUIReopenHelperArmsAndCancelsWithoutStoppingGUI(t *testing.T) {
	id, self := os.Getenv("CODEXM_GUI_OWNER_TEST_ID"), os.Getenv("CODEXM_TUI_TEST_BINARY")
	if id == "" || self == "" {
		t.Skip("opt-in installed GUI read-only/helper cancellation test")
	}
	ctx := context.Background()
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := findGUIOwner(ctx, c, id)
	if err != nil {
		t.Fatal(err)
	}
	c.Path = filepath.Join(t.TempDir(), "config.toml")
	c.Storage.StateDir = filepath.Join(filepath.Dir(c.Path), "state")
	minimal := fmt.Sprintf("[runtime]\nshared_codex_home=%q\n[storage]\nstate_dir=%q\n", config.Expand(c.Runtime.SharedCodexHome), c.Storage.StateDir)
	if err = os.WriteFile(c.Path, []byte(minimal), 0600); err != nil {
		t.Fatal(err)
	}
	caller, err := inspectProcess(ctx, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, 8)
	if _, err = rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	session := "codexm-gui-reopen-" + hex.EncodeToString(nonce)
	dir := filepath.Join(root(c), "gui-restarts", session)
	os.MkdirAll(dir, 0700)
	file := filepath.Join(dir, "request.json")
	spec := guiRestartSpec{Owner: owner, Caller: caller, Thread: id, Shared: config.Expand(c.Runtime.SharedCodexHome), Session: session, Created: time.Now().Unix()}
	if err = writeJSON(file, spec); err != nil {
		t.Fatal(err)
	}
	if _, err = tmux(ctx, CodexTmuxServer, "new-session", "-d", "-s", session, "-e", "PATH="+os.Getenv("PATH"), self, "--config", c.Path, "__gui-reopen", file); err != nil {
		t.Fatal(err)
	}
	defer tmux(ctx, CodexTmuxServer, "kill-session", "-t", "="+session)
	if _, err = tmux(ctx, CodexTmuxServer, "set-option", "-w", "-t", session, "remain-on-exit", "off"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !markerExists(filepath.Join(dir, "ready.json")) {
		if markerExists(filepath.Join(dir, "result.json")) || time.Now().After(deadline) {
			b, _ := os.ReadFile(filepath.Join(dir, "result.json"))
			t.Fatalf("helper readiness failed: %s", b)
		}
		time.Sleep(50 * time.Millisecond)
	}
	(&guiRecovery{dir: dir}).fail()
	for !markerExists(filepath.Join(dir, "result.json")) {
		if time.Now().After(deadline) {
			t.Fatal("helper did not cancel")
		}
		time.Sleep(50 * time.Millisecond)
	}
	var result struct{ Error string }
	b, _ := os.ReadFile(filepath.Join(dir, "result.json"))
	if err = json.Unmarshal(b, &result); err != nil || result.Error != "" {
		t.Fatalf("cancel result: %s %v", b, err)
	}
	if markerExists(filepath.Join(dir, "stop-requested")) || !sameProcess(ctx, owner.Process) || !sameProcess(ctx, owner.Writer) {
		t.Fatal("cancellation affected GUI ownership")
	}
	t.Log("real recovery helper acknowledged readiness and cancelled; GUI/writer PIDs unchanged")
}
