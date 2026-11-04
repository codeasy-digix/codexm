package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirstRunSaveReloadAndBackup(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	c, e := Load(p)
	if e != nil {
		t.Fatal(e)
	}
	if c.Manager.Account != "" {
		t.Fatal("first run bypassed account selection")
	}
	c.Manager.Account = "account-backup"
	if e = c.Save(); e != nil {
		t.Fatal(e)
	}
	c.Manager.Account = "account-work"
	if e = c.Save(); e != nil {
		t.Fatal(e)
	}
	d, e := Load(p)
	if e != nil || d.Manager.Account != "account-work" {
		t.Fatalf("reload: %+v %v", d, e)
	}
	old, e := Load(p + ".previous")
	if e != nil || old.Manager.Account != "account-backup" {
		t.Fatal("previous selection was not preserved")
	}
	s, _ := os.Stat(p)
	if s.Mode().Perm() != 0600 {
		t.Fatal("config permissions")
	}
}

func TestLegacyDefaultWorkerIsIgnoredAndRemovedOnSave(t *testing.T) {
	c := Defaults()
	c.WorkerAccounts.Priority = []string{"account-work", "account-secondary"}
	c.Path = filepath.Join(t.TempDir(), "config.toml")
	if e := c.Save(); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(c.Path)
	b = []byte(strings.Replace(string(b), "[worker_accounts]", "[worker_accounts]\ndefault_account = 'obsolete/invalid'", 1))
	if e := os.WriteFile(c.Path, b, 0600); e != nil {
		t.Fatal(e)
	}
	loaded, e := Load(c.Path)
	if e != nil || loaded.FirstPriority() != "account-work" {
		t.Fatal("obsolete default controlled selection", e)
	}
	if e = loaded.Save(); e != nil {
		t.Fatal(e)
	}
	current, _ := os.ReadFile(c.Path)
	previous, _ := os.ReadFile(c.Path + ".previous")
	if strings.Contains(string(current), "default_account") || string(previous) != string(b) {
		t.Fatal("legacy migration lost its backup or saved a default")
	}
}
func TestInvalidAccountPolicyFailsBeforeWrite(t *testing.T) {
	c := Defaults()
	c.Path = filepath.Join(t.TempDir(), "config")
	c.WorkerAccounts.Priority = []string{"account-work", "account-backup", "account-work"}
	if c.Save() == nil {
		t.Fatal("duplicate accounts accepted")
	}
	if _, e := os.Stat(c.Path); !os.IsNotExist(e) {
		t.Fatal("invalid config was written")
	}
}
