package nativecli

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/codeasy-digix/codexm/internal/config"
)

const fixtureID = "11111111-2222-4333-8444-555555555555"

func fixture(t *testing.T) config.Config {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	c := config.Defaults()
	c.Path = filepath.Join(h, "settings.toml")
	c.Runtime.SharedCodexHome = filepath.Join(h, "shared")
	c.Runtime.AccountsDir = filepath.Join(h, "accounts")
	c.Storage.StateDir = filepath.Join(h, "state")
	if e := os.MkdirAll(c.Runtime.SharedCodexHome, 0700); e != nil {
		t.Fatal(e)
	}
	db, e := sql.Open("sqlite", filepath.Join(c.Runtime.SharedCodexHome, "state_5.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(`CREATE TABLE threads(id TEXT PRIMARY KEY,title TEXT,cwd TEXT,updated_at INTEGER,archived INTEGER); INSERT INTO threads VALUES(?,?,?,10,0)`, fixtureID, "fixture chat", h); e != nil {
		t.Fatal(e)
	}
	c.Runtime.CodexBinary = filepath.Join(h, "codex-fixture")
	if e = os.WriteFile(c.Runtime.CodexBinary, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$HOME/argv\"\nprintf '%s' \"$CODEX_HOME\" > \"$HOME/selected-home\"\nexit 0\n"), 0700); e != nil {
		t.Fatal(e)
	}
	return c
}
func TestNativeHandoffUsesOfficialArgumentsAndNoEngineOrManager(t *testing.T) {
	c := fixture(t)
	ctx := context.Background()
	l, e := Plan(ctx, c, fixtureID, "default")
	if e != nil {
		t.Fatal(e)
	}
	if e = Prepare(ctx, c, l); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), "argv"))
	if string(b) != "--no-daemon\nunarchive\n"+fixtureID+"\n" {
		t.Fatalf("wrong arguments: %q", b)
	}
	b, _ = os.ReadFile(filepath.Join(os.Getenv("HOME"), "selected-home"))
	if string(b) != c.Runtime.SharedCodexHome {
		t.Fatal("wrong native home")
	}
	if !reflect.DeepEqual(ResumeArgs(l), []string{l.Binary, "--no-daemon", "--no-alt-screen", "resume", fixtureID}) {
		t.Fatal("resume settings were overridden")
	}
	for _, name := range []string{"state.sqlite", "engine.json", "control.sock"} {
		if _, e = os.Stat(filepath.Join(c.StateDir(), name)); !os.IsNotExist(e) {
			t.Fatal("prepare created manager/engine state")
		}
	}
	for _, bad := range []string{"--all", "../other", fixtureID + ";echo bad"} {
		if _, e = Plan(ctx, c, bad, "default"); e == nil {
			t.Fatal("invalid thread accepted")
		}
	}
	if _, e = Plan(ctx, c, fixtureID, "--other"); e == nil {
		t.Fatal("invalid account accepted")
	}
}
func TestUnarchiveFailureDoesNotEraseLocksOrClaimLaunch(t *testing.T) {
	c := fixture(t)
	ctx := context.Background()
	l, e := Plan(ctx, c, fixtureID, "default")
	if e != nil {
		t.Fatal(e)
	}
	lock := filepath.Join(c.Runtime.SharedCodexHome, "writer.lock")
	os.WriteFile(lock, []byte("external owner"), 0600)
	os.WriteFile(l.Binary, []byte("#!/bin/sh\necho writer-busy >&2\nexit 9\n"), 0700)
	if e = Prepare(ctx, c, l); e == nil || !strings.Contains(e.Error(), "resume was not started") {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(lock)
	if string(b) != "external owner" {
		t.Fatal("external lock changed")
	}
	if _, e = os.Stat(receiptPath(c, l.Thread)); !os.IsNotExist(e) {
		t.Fatal("failed handoff recorded as launched")
	}
}
func TestCompactConfigIgnoresLegacyPolicyWithoutRewriting(t *testing.T) {
	c := fixture(t)
	b := []byte("[manager]\naccount='invalid policy!'\ninvocation='retired'\n[worker_accounts]\npriority=['unknown']\n[runtime]\nshared_codex_home=" + strconvQuote(c.Runtime.SharedCodexHome) + "\n")
	os.WriteFile(c.Path, b, 0600)
	loaded, e := Load(c.Path)
	if e != nil {
		t.Fatal(e)
	}
	if loaded.Runtime.SharedCodexHome != c.Runtime.SharedCodexHome || loaded.Manager.Account != "" {
		t.Fatal("legacy policy was loaded")
	}
	after, _ := os.ReadFile(c.Path)
	if string(after) != string(b) {
		t.Fatal("old config rewritten")
	}
}
func strconvQuote(s string) string { b, _ := json.Marshal(s); return string(b) }
func TestBridgeRejectsWorkerCommandsAndMutationReplay(t *testing.T) {
	c := fixture(t)
	ctx := context.Background()
	if _, e := Dispatch(ctx, c, "", Request{Version: 1, Method: "jobs.resume", Params: json.RawMessage(`{}`)}); e == nil {
		t.Fatal("retired command accepted")
	}
	r := Request{Version: 1, Method: "terminal.input", Owner: "browser", Operation: "operation-1234567890", Params: json.RawMessage(`{"session":"not-owned","text":"secret draft"}`)}
	_, e := Dispatch(ctx, c, "", r)
	if e == nil {
		t.Fatal("unowned terminal accepted")
	}
	r.Params = json.RawMessage(`{"session":"not-owned","text":"different draft"}`)
	if _, e = Dispatch(ctx, c, "", r); e == nil || !strings.Contains(e.Error(), "reused") {
		t.Fatal("operation ID reuse accepted", e)
	}
	files, _ := filepath.Glob(filepath.Join(root(c), "operations", "*"))
	for _, p := range files {
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), "draft") {
			t.Fatal("input leaked into operation receipts")
		}
	}
}
