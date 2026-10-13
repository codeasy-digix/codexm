package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func accountFixture(t *testing.T) config.Config {
	t.Helper()
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
	return c
}

func TestAccountSelectionPersistsWithoutChangingNativeLoginOrShell(t *testing.T) {
	c := accountFixture(t)
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
	if got, _ := os.ReadFile(filepath.Join(c.Runtime.SharedCodexHome, "auth.json")); string(got) != "native default login must remain here" {
		t.Fatal("native login changed")
	}
	if os.Getenv("CODEX_ACCOUNT") != "parent" || os.Getenv("CODEX_HOME") != "/parent/native-home" {
		t.Fatal("parent shell changed")
	}
}

func TestExplicitAccountDefaultFormsPersistAcrossReloads(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{"positional", []string{"default"}},
		{"flag", []string{"--set-default"}},
		{"positionalWithDeviceLogin", []string{"default", "--login", "--device"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := accountFixture(t)
			if e := saveWorkerAccount(c, "account-secondary"); e != nil {
				t.Fatal(e)
			}
			before, _ := os.ReadFile(filepath.Join(c.Runtime.SharedCodexHome, "auth.json"))
			command := append([]string{"--config", c.Path, "account", "account-main"}, test.args...)
			if e := Main(command); e != nil {
				t.Fatal(e)
			}
			loaded, e := config.Load(c.Path)
			if e != nil || loaded.DefaultWorker() != "account-main" {
				t.Fatal("explicit default was not saved", e)
			}
			for n := 0; n < 2; n++ {
				if e := Main([]string{"--config", c.Path, "account", "--list"}); e != nil {
					t.Fatal(e)
				}
			}
			if loaded.Manager != c.Manager || loaded.Runtime != c.Runtime || loaded.Storage != c.Storage {
				t.Fatal("default selection changed unrelated settings")
			}
			after, _ := os.ReadFile(filepath.Join(c.Runtime.SharedCodexHome, "auth.json"))
			if !bytes.Equal(before, after) {
				t.Fatal("default selection changed native login")
			}
		})
	}
}

func TestInvalidAccountArgumentsFailBeforeLoginOrSettingsChange(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"unknownAction", []string{"other"}, "unexpected account argument"},
		{"extraAction", []string{"default", "other"}, "unexpected account argument"},
		{"loginBeforeUnknownAction", []string{"--login", "other"}, "unexpected account argument"},
		{"conflictingLoginMethods", []string{"default", "--browser", "--device"}, "choose one login method"},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := accountFixture(t)
			before, _ := os.ReadFile(c.Path)
			command := append([]string{"--config", c.Path, "account", "account-main"}, test.args...)
			if e := Main(command); e == nil || !strings.Contains(e.Error(), test.want) {
				t.Fatal("incorrect account argument error", e)
			}
			after, _ := os.ReadFile(c.Path)
			if !bytes.Equal(before, after) {
				t.Fatal("invalid arguments changed saved settings")
			}
			if _, e := os.Stat(filepath.Join(c.Runtime.AccountsDir, "account-main")); !os.IsNotExist(e) {
				t.Fatal("invalid arguments reached named account login", e)
			}
		})
	}
}

func fmtHash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
