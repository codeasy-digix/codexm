package nativecli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
)

func TestSwitchThresholdRequiresBothKnownWindows(t *testing.T) {
	minutes5, minutesWeek := 300, 10080
	for _, tt := range []struct {
		name            string
		used5, usedWeek int
		absent          bool
		low             bool
		unknown         bool
	}{
		{"five hour exactly five", 95, 10, false, true, false},
		{"weekly exactly five", 10, 95, false, true, false},
		{"six remains", 94, 10, false, false, false},
		{"unknown week", 96, 0, true, false, true},
		{"invalid percentage", 101, 10, false, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := accounts.Snapshot{Account: "fixture", Authenticated: true, Single: accounts.Bucket{Primary: &accounts.Window{UsedPercent: tt.used5, Minutes: &minutes5}, Secondary: &accounts.Window{UsedPercent: tt.usedWeek, Minutes: &minutesWeek}}}
			if tt.absent {
				s.Single.Secondary = nil
			}
			low, err := usageLow(usageFromSnapshot(s, ""), 5)
			if (err != nil) != tt.unknown || low != tt.low {
				t.Fatalf("low=%v err=%v", low, err)
			}
		})
	}
}

func TestContinueIsExplicitAndNoSummaryIsInserted(t *testing.T) {
	l := Launch{Binary: "/codex", Thread: fixtureID}
	if len(ResumeArgs(l)) != 5 {
		t.Fatal("plain resume submits a prompt")
	}
	l.Continue = true
	a := ResumeArgs(l)
	if len(a) != 6 || a[4] != fixtureID || a[5] != ContinuePrompt {
		t.Fatal(a)
	}
}

func TestHandoffRefusesOldReceiptsBeforeSignalling(t *testing.T) {
	c := fixture(t)
	if e := writeReceipt(c, Launch{Thread: fixtureID, Ready: true, Account: "default"}); e != nil {
		t.Fatal(e)
	}
	_, e := RequestHandoff(context.Background(), c, "/unused", fixtureID, "beta", -1)
	if e == nil || !strings.Contains(e.Error(), "ownership receipt") {
		t.Fatal(e)
	}
}

func fixtureLogin(t *testing.T, home, name string) {
	t.Helper()
	os.MkdirAll(home, 0700)
	b, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]string{"id_token": "fixture", "access_token": "synthetic-access", "refresh_token": "synthetic-refresh", "account_id": name}})
	if e := os.WriteFile(filepath.Join(home, "auth.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
}

// Run the real CLI request and independent helper inside tmux, with a synthetic
// native executable implementing writer locks, quota RPC and an in-turn request.
// No actual account, native history or GUI is touched.
func TestRequestedAccountReplacementThroughTmux(t *testing.T) {
	t.Run("replacement", func(t *testing.T) { testAccountReplacement(t, false) })
	t.Run("unreleased-writer", func(t *testing.T) { testAccountReplacement(t, true) })
}
func testAccountReplacement(t *testing.T, ignoreTerm bool) {
	binary := os.Getenv("CODEXM_HANDOFF_TEST_BINARY")
	if binary == "" {
		t.Skip("set built codexm for isolated handoff integration")
	}
	realHome := os.Getenv("HOME")
	c := fixture(t)
	h := os.Getenv("HOME")
	fixtureLogin(t, c.Runtime.SharedCodexHome, "first-account")
	fixtureLogin(t, filepath.Join(c.Runtime.AccountsDir, "beta"), "second-account")
	fixtureLogin(t, filepath.Join(c.Runtime.AccountsDir, "alias"), "first-account")
	// The fixture is a native executable, so PID/executable/lock checks are real.
	source, _ := filepath.Abs("testdata/switch_fixture.go")
	if e := os.Remove(c.Runtime.CodexBinary); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("go", "build", "-o", c.Runtime.CodexBinary, source)
	cmd.Env = append(os.Environ(), "HOME="+realHome)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("build fixture: %v %s", e, b)
	}
	if e := c.Save(); e != nil {
		t.Fatal(e)
	}
	t.Setenv("FIXTURE_CODEXM", binary)
	t.Setenv("FIXTURE_CONFIG", c.Path)
	t.Setenv("FIXTURE_EVENTS", filepath.Join(h, "events.jsonl"))
	t.Setenv("FIXTURE_INSIDE_TRIGGER", filepath.Join(h, "switch-now"))
	// Open passes a deliberately small environment to tmux; fixture coordination
	// is also placed in this isolated HOME, never a production environment bypass.
	params, _ := json.Marshal(map[string]string{"binary": binary, "config": c.Path})
	os.WriteFile(filepath.Join(h, "fixture-params.json"), params, 0600)
	l, e := Open(context.Background(), c, binary, fixtureID, "default")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		for _, s := range Sessions(c) {
			_, _ = tmux(context.Background(), s.TmuxServer, "kill-session", "-t", "="+s.Session)
		}
	})
	await := func(what string, ok func() bool) {
		t.Helper()
		until := time.Now().Add(35 * time.Second)
		for time.Now().Before(until) {
			if ok() {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		screen, _ := Screen(context.Background(), c, l.Session)
		t.Fatalf("%s timed out: %s", what, screen.Text)
	}
	var first Launch
	await("first writer", func() bool {
		first, e = readLast(c, fixtureID)
		return e == nil && verifyOwnedCLI(context.Background(), c, first) == nil
	})
	// A stale PID incarnation must never be accepted, even with the right writer.
	stale := first
	stale.OwnerKey = "stale"
	if verifyOwnedCLI(context.Background(), c, stale) == nil {
		t.Fatal("stale process accepted")
	}
	// Same real login under another nickname is not a usable replacement.
	_, e = RequestHandoff(context.Background(), c, binary, fixtureID, "alias", -1)
	if e == nil || !strings.Contains(e.Error(), "same login") {
		t.Fatal(e)
	}
	if e = verifyOwnedCLI(context.Background(), c, first); e != nil {
		t.Fatal("rejection stopped source", e)
	}
	os.WriteFile(filepath.Join(h, "low-target"), []byte("fixture"), 0600)
	_, e = RequestHandoff(context.Background(), c, binary, fixtureID, "beta", -1)
	if e == nil || !strings.Contains(e.Error(), "target account has 5%") {
		t.Fatal("low target accepted", e)
	}
	os.Remove(filepath.Join(h, "low-target"))
	_, e = RequestHandoff(context.Background(), c, binary, fixtureID, "beta", 1)
	if e == nil || !strings.Contains(e.Error(), "above the requested threshold") {
		t.Fatal("above-threshold source switched", e)
	}
	if e = verifyOwnedCLI(context.Background(), c, first); e != nil {
		t.Fatal("preflight changed source", e)
	}
	if ignoreTerm {
		os.WriteFile(filepath.Join(h, "ignore-term"), []byte("fixture"), 0600)
	}
	// Ask from inside the old CLI: the caller returns before its parent is stopped.
	os.WriteFile(filepath.Join(h, "switch-now"), []byte("go"), 0600)
	if ignoreTerm {
		await("bounded failure", func() bool { s, e := HandoffStatus(c, fixtureID); return e == nil && s.Phase == "failed" })
		if e = verifyOwnedCLI(context.Background(), c, first); e != nil {
			t.Fatal("source was force-killed", e)
		}
		if _, e = tmux(context.Background(), CodexTmuxServer, "has-session", "-t", "="+terminalName(c, fixtureID, "beta")); e == nil {
			t.Fatal("replacement started while old writer survived")
		}
		return
	}
	await("replacement", func() bool {
		s, e := HandoffStatus(c, fixtureID)
		if e == nil && s.Phase == "failed" {
			t.Fatalf("handoff failed: %s", s.Error)
		}
		return e == nil && s.Phase == "started"
	})
	last, e := readLast(c, fixtureID)
	if e != nil || last.Account != "beta" || !last.Continue || last.Owner.PID == first.Owner.PID {
		t.Fatal("not replaced", last, e)
	}
	if verifyOwnedCLI(context.Background(), c, first) == nil {
		t.Fatal("old CLI survived handoff")
	}
	if e = verifyOwnedCLI(context.Background(), c, last); e != nil {
		t.Fatal(e)
	}
	events, _ := os.ReadFile(filepath.Join(h, "events.jsonl"))
	lines := strings.Split(strings.TrimSpace(string(events)), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], `"event":"start"`) || !strings.Contains(lines[1], `"event":"stop"`) || !strings.Contains(lines[2], `"event":"start"`) {
		t.Fatalf("unexpected execution order: %s", events)
	}
	var event struct {
		Prompt string `json:"prompt"`
		Thread string `json:"thread"`
	}
	json.Unmarshal([]byte(lines[2]), &event)
	if event.Thread != fixtureID || event.Prompt != ContinuePrompt {
		t.Fatal("thread changed or continuation missing", event)
	}
	// The old dead session is removed; this chat has only its replacement alive.
	if _, e = tmux(context.Background(), CodexTmuxServer, "has-session", "-t", "="+l.Session); e == nil {
		t.Fatal("old dead terminal was left behind")
	}
}
