package accounts

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func removalFixture(t *testing.T) (config.Config, RemoveArgs) {
	t.Helper()
	r := t.TempDir()
	c := config.Defaults()
	c.Path = filepath.Join(r, "config.toml")
	c.Runtime.AccountsDir = filepath.Join(r, "accounts")
	c.Runtime.SharedCodexHome = filepath.Join(r, "shared")
	c.Storage.StateDir = filepath.Join(r, "state")
	c.Runtime.EnginesDir = filepath.Join(r, "engines")
	c.Manager.Account = "account-work"
	c.WorkerAccounts.Priority = []string{"account-work", "account-secondary"}
	c.WorkerAccounts.FallbackAccount = "account-secondary"
	c.Notifications.Accounts = []string{"account-secondary"}
	for _, n := range c.WorkerAccounts.Priority {
		if err := cr.EnsureAccountHome(c, n); err != nil {
			t.Fatal(err)
		}
		if err := config.AtomicWrite(filepath.Join(c.Runtime.AccountsDir, n, "auth.json"), []byte(`{"tokens":{"account_id":"fixture"}}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	_, rev, err := config.Current(c.Path)
	if err != nil {
		t.Fatal(err)
	}
	return c, RemoveArgs{Account: "account-secondary", Revision: rev}
}

func TestRemovalPreservesNativeLoginCredentialsHistoryAndCanBeRestored(t *testing.T) {
	c, p := removalFixture(t)
	auth := filepath.Join(c.Runtime.AccountsDir, "account-secondary", "auth.json")
	native := filepath.Join(c.Runtime.SharedCodexHome, "auth.json")
	if err := os.Symlink(auth, native); err != nil {
		t.Fatal(err)
	}
	history := filepath.Join(c.Runtime.SharedCodexHome, "history.jsonl")
	if err := os.WriteFile(history, []byte("conversation-canary"), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(auth)
	historical := jobs.Job{ID: "finished", Managed: true, State: "complete", Account: "account-secondary"}
	result, err := RemoveRegistration(c, p, []jobs.Job{historical})
	if err != nil || result.Revision == p.Revision {
		t.Fatal("remove", err)
	}
	loaded, err := config.Load(c.Path)
	if err != nil {
		t.Fatal(err)
	}
	names, _ := List(loaded)
	if slices.Contains(names, "account-secondary") || slices.Contains(loaded.WorkerAccounts.Priority, "account-secondary") || loaded.WorkerAccounts.FallbackAccount != "" {
		t.Fatal("removed account remained selectable")
	}
	if !slices.Equal(loaded.Notifications.Accounts, c.Notifications.Accounts) {
		t.Fatal("notification selection changed meaning")
	}
	after, _ := os.ReadFile(auth)
	link, _ := os.Readlink(native)
	h, _ := os.ReadFile(history)
	if string(after) != string(before) || link != auth || string(h) != "conversation-canary" || CurrentNickname(loaded, c.Runtime.SharedCodexHome, "") != "account-secondary" {
		t.Fatal("native login or history changed")
	}
	if _, _, err = config.Update(c.Path, "", func(c *config.Config) error { return c.RegisterAccount("account-secondary") }); err != nil {
		t.Fatal(err)
	}
	loaded, _ = config.Load(c.Path)
	names, _ = List(loaded)
	if !slices.Contains(names, "account-secondary") || !slices.Contains(loaded.WorkerAccounts.Priority, "account-secondary") {
		t.Fatal("explicit registration did not restore alias")
	}
}

func TestRemovalRejectsManagerJobsLoginLockAndStaleRevisionWithoutChanges(t *testing.T) {
	for _, which := range []string{"manager", "running", "idle", "paused", "queued", "target", "terminal-worker", "login", "stale"} {
		t.Run(which, func(t *testing.T) {
			c, p := removalFixture(t)
			var current []jobs.Job
			switch which {
			case "manager":
				p.Account = "account-work"
			case "running", "idle", "paused", "queued":
				current = []jobs.Job{{Managed: true, Account: "account-secondary", State: which}}
			case "target":
				current = []jobs.Job{{Managed: true, Account: "account-work", TargetAccount: "account-secondary", State: "switching"}}
			case "terminal-worker":
				current = []jobs.Job{{Managed: true, Account: "account-secondary", State: "complete", Session: "existing-worker"}}
			case "login":
				lock, err := cr.AccountLoginLock(c, "account-secondary")
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
			case "stale":
				if _, _, err := config.Update(c.Path, "", func(c *config.Config) error { c.Execution.MaxParallelJobs++; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.ReadFile(c.Path)
			if _, err := RemoveRegistration(c, p, current); err == nil {
				t.Fatal("unsafe removal accepted")
			}
			after, _ := os.ReadFile(c.Path)
			if string(after) != string(before) {
				t.Fatal("rejected removal changed settings")
			}
		})
	}
}

func TestLastRegistrationCanBeRemovedAndInvalidAliasCreatesNoHome(t *testing.T) {
	c, p := removalFixture(t)
	c.Manager.Account = ""
	c.WorkerAccounts.Priority = []string{"account-secondary"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	_, p.Revision, _ = config.Current(c.Path)
	if _, err := RemoveRegistration(c, p, nil); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(c.Path)
	if err != nil || len(loaded.WorkerAccounts.Priority) != 0 {
		t.Fatal("empty priority restored default accounts", err)
	}
	for _, name := range []string{"missing", "../outside", "default"} {
		p.Account = name
		if _, err := RemoveRegistration(loaded, p, nil); err == nil {
			t.Fatal("invalid alias accepted")
		}
	}
	if _, err := os.Stat(filepath.Join(c.Runtime.AccountsDir, "missing")); !os.IsNotExist(err) {
		t.Fatal("removing missing alias created a home")
	}
}

func TestVerifiedLoginRestoresRegistryAndRollsBackAuthWhenSettingsWriteFails(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "restore", true: "rollback"}[fail], func(t *testing.T) {
			c, p := removalFixture(t)
			if _, err := RemoveRegistration(c, p, nil); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(c.Runtime.AccountsDir, "account-secondary", "auth.json")
			before, _ := os.ReadFile(target)
			if fail {
				if err := os.Remove(c.Path + ".previous"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(c.Path+".previous", 0700); err != nil {
					t.Fatal(err)
				}
			}
			err := publishLogin(c, "account-secondary", target, []byte("verified-synthetic-credentials"))
			loaded, _ := config.Load(c.Path)
			names, _ := List(loaded)
			after, _ := os.ReadFile(target)
			if fail {
				if err == nil || slices.Contains(names, "account-secondary") || string(before) != string(after) {
					t.Fatal("failed registry write changed credentials or registration")
				}
			} else if err != nil || !slices.Contains(names, "account-secondary") || !strings.Contains(string(after), "verified-synthetic") {
				t.Fatal("verified login did not restore account", err)
			}
		})
	}
}
