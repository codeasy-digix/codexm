package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func TestAccountRemoveWorksWithoutAnEngineAndNativeSelectionKeepsItRemoved(t *testing.T) {
	c := accountFixture(t)
	before, _ := os.ReadFile(filepath.Join(c.Runtime.AccountsDir, "account-secondary", "auth.json"))
	if err := os.Rename(filepath.Join(c.StateDir(), "engine.json"), filepath.Join(c.StateDir(), "saved-engine.json")); err != nil {
		t.Fatal(err)
	}
	if err := Main([]string{"--config", c.Path, "account", "account-secondary", "--remove"}); err != nil {
		t.Fatal(err)
	}
	loaded, _ := config.Load(c.Path)
	if !slices.Contains(loaded.AccountRegistry.Removed, "account-secondary") {
		t.Fatal("CLI did not remove registration")
	}
	after, _ := os.ReadFile(filepath.Join(c.Runtime.AccountsDir, "account-secondary", "auth.json"))
	if string(after) != string(before) {
		t.Fatal("CLI changed saved auth")
	}
	if err := os.Rename(filepath.Join(c.StateDir(), "saved-engine.json"), filepath.Join(c.StateDir(), "engine.json")); err != nil {
		t.Fatal(err)
	}
	prepareShellResult(t)
	if err := Main([]string{"--config", c.Path, "account", "account-secondary"}); err != nil {
		t.Fatal(err)
	}
	loaded, _ = config.Load(c.Path)
	if !slices.Contains(loaded.AccountRegistry.Removed, "account-secondary") || slices.Contains(loaded.WorkerAccounts.Priority, "account-secondary") {
		t.Fatal("native selection restored codexm worker registration")
	}
}

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

	for _, name := range []string{"account-secondary", "account-main"} {
		if e := cr.EnsureAccountHome(c, name); e != nil {
			t.Fatal(e)
		}
		auth, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]string{"id_token": "synthetic-id", "access_token": "synthetic-access", "refresh_token": "synthetic-refresh", "account_id": "fixture-" + name}})
		if e := os.WriteFile(filepath.Join(c.Runtime.AccountsDir, name, "auth.json"), auth, 0600); e != nil {
			t.Fatal(e)
		}
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

func TestAccountSelectionPublishesShellResultWithoutChangingPolicyOrGlobalLogin(t *testing.T) {
	c := accountFixture(t)
	t.Setenv("CODEX_ACCOUNT", "parent")
	t.Setenv("CODEX_HOME", "/parent/native-home")
	result := prepareShellResult(t)
	configBefore, _ := os.ReadFile(c.Path)
	if e := Main([]string{"--config", c.Path, "account", "account-secondary"}); e != nil {
		t.Fatal(e)
	}
	b, err := os.ReadFile(result)
	if err != nil || string(b) != "codexm-native-account-v1\x00account-secondary\x00"+filepath.Join(c.Runtime.AccountsDir, "account-secondary")+"\x00"+c.Runtime.SharedCodexHome+"\x00" {
		t.Fatal("incorrect shell selection result", err)
	}
	configAfter, _ := os.ReadFile(c.Path)
	if !bytes.Equal(configBefore, configAfter) {
		t.Fatal("native selection changed worker settings")
	}
	loaded, e := config.Load(c.Path)
	if e != nil || loaded.FirstPriority() != "account-work" || loaded.Manager.Account != "" {
		t.Fatal("saved selection or manager changed", e)
	}
	prepareShellResult(t)
	if e := Main([]string{"--config", c.Path, "account", "cancelled", "--login", "--device"}); e == nil {
		t.Fatal("cancelled login accepted")
	}
	loaded, _ = config.Load(c.Path)
	if loaded.FirstPriority() != "account-work" {
		t.Fatal("failed login replaced default")
	}
	if got, _ := os.ReadFile(filepath.Join(c.Runtime.SharedCodexHome, "auth.json")); string(got) != "native default login must remain here" {
		t.Fatal("native login changed")
	}
	if os.Getenv("CODEX_ACCOUNT") != "parent" || os.Getenv("CODEX_HOME") != "/parent/native-home" {
		t.Fatal("parent shell changed")
	}
}

func TestExplicitAccountDefaultFormsChangeNativeOnlyAcrossReloads(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{"positional", []string{"default"}},
		{"flag", []string{"--set-default"}},
		{"singleDashFlag", []string{"-set-default"}},
		{"positionalWithDeviceLogin", []string{"default", "--login", "--device"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := accountFixture(t)
			configBefore, _ := os.ReadFile(c.Path)
			before, _ := os.ReadFile(filepath.Join(c.Runtime.SharedCodexHome, "auth.json"))
			command := append([]string{"--config", c.Path, "account", "account-main"}, test.args...)
			if e := Main(command); e != nil {
				t.Fatal(e)
			}
			loaded, e := config.Load(c.Path)
			if e != nil || loaded.FirstPriority() != "account-work" {
				t.Fatal("native selection changed codexm worker defaults", e)
			}
			configAfter, _ := os.ReadFile(c.Path)
			if !bytes.Equal(configBefore, configAfter) {
				t.Fatal("native default changed worker policy")
			}
			target, e := os.Readlink(filepath.Join(c.Runtime.SharedCodexHome, "auth.json"))
			if e != nil || target != filepath.Join(c.Runtime.AccountsDir, "account-main", "auth.json") {
				t.Fatal("native default was not selected", e)
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
			if bytes.Equal(before, after) || !bytes.Contains(after, []byte("fixture-account-main")) {
				t.Fatal("native login did not change to the named account")
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
			command := append([]string{"--config", c.Path, "account", "not-registered"}, test.args...)
			if e := Main(command); e == nil || !strings.Contains(e.Error(), test.want) {
				t.Fatal("incorrect account argument error", e)
			}
			after, _ := os.ReadFile(c.Path)
			if !bytes.Equal(before, after) {
				t.Fatal("invalid arguments changed saved settings")
			}
			if _, e := os.Stat(filepath.Join(c.Runtime.AccountsDir, "not-registered")); !os.IsNotExist(e) {
				t.Fatal("invalid arguments reached named account login", e)
			}
		})
	}
}

func fmtHash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestNativeDefaultFailureDoesNotChangeWorkerPolicyOrNativeAuth(t *testing.T) {
	c := accountFixture(t)
	if e := os.WriteFile(filepath.Join(c.Runtime.SharedCodexHome, "config.toml"), []byte("cli_auth_credentials_store='keyring'"), 0600); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(c.Path)
	if e := Main([]string{"--config", c.Path, "account", "account-main", "default"}); e == nil || !strings.Contains(e.Error(), "requires cli_auth_credentials_store") {
		t.Fatal("unsupported auth store accepted", e)
	}
	after, _ := os.ReadFile(c.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed native switch changed worker configuration")
	}
	if b, _ := os.ReadFile(filepath.Join(c.Runtime.SharedCodexHome, "auth.json")); string(b) != "native default login must remain here" {
		t.Fatal("failed switch changed native auth")
	}
}

func prepareShellResult(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "result")
	t.Setenv("CODEXM_SHELL_RESULT", p)
	return p
}

func TestNativeSelectionWithoutShellAdapterFailsBeforeLogin(t *testing.T) {
	c := accountFixture(t)
	t.Setenv("CODEXM_SHELL_RESULT", "")
	if err := Main([]string{"--config", c.Path, "account", "missing"}); err == nil || !strings.Contains(err.Error(), "shell-init zsh") {
		t.Fatal("missing parent-shell adapter was not explained", err)
	}
	if _, err := os.Stat(filepath.Join(c.Runtime.AccountsDir, "missing")); !os.IsNotExist(err) {
		t.Fatal("missing adapter reached login")
	}
}
