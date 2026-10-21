package runtime

import (
	"context"
	"github.com/codeasy-digix/codexm/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNamedRuntimeDoesNotDependOnAccountControllerOrSharedDaemon(t *testing.T) {
	dir := t.TempDir()
	c := config.Defaults()
	c.Runtime.SharedCodexHome = filepath.Join(dir, "shared")
	c.Runtime.AccountsDir = filepath.Join(dir, "accounts")
	c.Runtime.EnginesDir = filepath.Join(dir, "engines")
	c.Storage.StateDir = filepath.Join(dir, "state")
	c.Runtime.Controller = "missing-account-extension"
	if e := os.MkdirAll(c.Runtime.SharedCodexHome, 0700); e != nil {
		t.Fatal(e)
	}
	original := []byte("default-login-must-be-preserved")
	if e := os.WriteFile(filepath.Join(c.Runtime.SharedCodexHome, "auth.json"), original, 0600); e != nil {
		t.Fatal(e)
	}
	source := fakeEngine(t, filepath.Join(dir, "official"), "0.162.1", true)
	if _, e := PinEngine(context.Background(), c, source); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", dir)
	t.Setenv("CODEX_ACCOUNT", "unrelated")
	t.Setenv("CODEX_HOME", "/unrelated")
	t.Setenv("CODEXM_SHELL_RESULT", filepath.Join(dir, "parent-shell-receipt"))
	r, e := Resolve(context.Background(), c)
	if e != nil {
		t.Fatal("pinned runtime required an external account controller", e)
	}
	cmd := r.Command(context.Background(), "account-secondary", "--version")
	if cmd.Err != nil || cmd.Path != r.Binary || strings.Contains(strings.Join(cmd.Args, " "), "__native") {
		t.Fatal("wrapper was used", cmd.Args, cmd.Err)
	}
	for _, value := range cmd.Env {
		if strings.HasPrefix(value, "CODEXM_SHELL_RESULT=") {
			t.Fatal("native engine inherited the parent-shell receipt path")
		}
	}
	if _, e := cmd.Output(); e != nil {
		t.Fatal("direct official runtime failed", e)
	}
	if got, e := os.ReadFile(filepath.Join(c.Runtime.SharedCodexHome, "auth.json")); e != nil || string(got) != string(original) {
		t.Fatal("default native login changed")
	}
	if os.Getenv("CODEX_ACCOUNT") != "unrelated" || os.Getenv("CODEX_HOME") != "/unrelated" {
		t.Fatal("parent account environment changed")
	}
	for _, name := range []string{"sessions", "thread-writer-locks"} {
		p := filepath.Join(c.Runtime.AccountsDir, "account-secondary", name)
		if got, e := os.Readlink(p); e != nil || got != filepath.Join(c.Runtime.SharedCodexHome, name) {
			t.Fatal("first account did not share its storage and writer locks", name, got, e)
		}
	}
}

func TestAccountHomePreservesConflictsAndRejectsNestedCredentials(t *testing.T) {
	root := t.TempDir()
	c := config.Defaults()
	c.Runtime.SharedCodexHome = filepath.Join(root, "shared")
	c.Runtime.AccountsDir = filepath.Join(root, "accounts")
	home := filepath.Join(c.Runtime.AccountsDir, "account-secondary")
	os.MkdirAll(home, 0700)
	conflict := filepath.Join(home, "sessions")
	os.WriteFile(conflict, []byte("existing private conversation data"), 0600)
	if e := EnsureAccountHome(c, "account-secondary"); e == nil {
		t.Fatal("private history conflict silently accepted")
	}
	if b, e := os.ReadFile(conflict); e != nil || string(b) != "existing private conversation data" {
		t.Fatal("existing data was overwritten")
	}
	c.Runtime.AccountsDir = filepath.Join(c.Runtime.SharedCodexHome, "accounts")
	if e := EnsureAccountHome(c, "account-work"); e == nil {
		t.Fatal("credentials placed in shared conversations")
	}
	if _, e := os.Stat(c.Runtime.AccountsDir); !os.IsNotExist(e) {
		t.Fatal("nested credentials created")
	}
}

func TestConcurrentAccountLoginPreservesCredentialFile(t *testing.T) {
	root := t.TempDir()
	c := config.Defaults()
	c.Runtime.SharedCodexHome = filepath.Join(root, "shared")
	c.Runtime.AccountsDir = filepath.Join(root, "accounts")
	lock, e := AccountLoginLock(c, "account-secondary")
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Close()
	if second, e := AccountLoginLock(c, "account-secondary"); e == nil {
		second.Close()
		t.Fatal("two concurrent logins were allowed")
	}
}
