package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func TestAccountSelectionPersistsWithoutChangingNativeLoginOrShell(t *testing.T) {
	root := t.TempDir()
	c := config.Defaults()
	c.Path = filepath.Join(root, "config.toml")
	c.Runtime.SharedCodexHome = filepath.Join(root, "shared")
	c.Runtime.AccountsDir = filepath.Join(root, "accounts")
	c.Runtime.EnginesDir = filepath.Join(root, "engines")
	c.Storage.StateDir = filepath.Join(root, "state")
	if e := c.Save(); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(c.Runtime.SharedCodexHome, 0700); e != nil {
		t.Fatal(e)
	}
	original := []byte("native default login must remain here")
	auth := filepath.Join(c.Runtime.SharedCodexHome, "auth.json")
	if e := os.WriteFile(auth, original, 0600); e != nil {
		t.Fatal(e)
	}
	binary := filepath.Join(root, "official-fixture")
	body := []byte("#!/bin/sh\nif [ \"$CODEX_ACCOUNT\" = cancelled ]; then exit 1; fi\nexit 0\n")
	if e := os.WriteFile(binary, body, 0700); e != nil {
		t.Fatal(e)
	}
	engine := cr.Engine{Binary: binary, Version: "fixture", ProtocolChecked: true, Files: map[string]string{"official-fixture": fmtHash(body)}}
	b, _ := json.Marshal(engine)
	if e := config.AtomicWrite(filepath.Join(c.StateDir(), "engine.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("CODEX_ACCOUNT", "parent")
	t.Setenv("CODEX_HOME", "/parent/native-home")
	if e := Main([]string{"--config", c.Path, "account", "account-secondary"}); e != nil {
		t.Fatal(e)
	}
	loaded, e := config.Load(c.Path)
	if e != nil || loaded.DefaultWorker() != "account-secondary" || loaded.Manager.Account != "" {
		t.Fatal("saved selection or manager changed", e)
	}
	if e := Main([]string{"--config", c.Path, "account", "cancelled", "--login", "--device"}); e == nil {
		t.Fatal("cancelled login accepted")
	}
	loaded, _ = config.Load(c.Path)
	if loaded.DefaultWorker() != "account-secondary" {
		t.Fatal("failed login replaced default")
	}
	if got, _ := os.ReadFile(auth); string(got) != string(original) {
		t.Fatal("native login changed")
	}
	if os.Getenv("CODEX_ACCOUNT") != "parent" || os.Getenv("CODEX_HOME") != "/parent/native-home" {
		t.Fatal("parent shell changed")
	}
}

func fmtHash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
