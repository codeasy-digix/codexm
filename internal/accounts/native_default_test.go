package accounts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func syntheticChatGPT(account string) []byte {
	claims, _ := json.Marshal(map[string]any{"email": "fixture@example.invalid", "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": account, "chatgpt_plan_type": "plus"}})
	token := "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".fixture" // gitleaks:allow -- unsigned, synthetic test JWT
	b, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]string{"id_token": token, "access_token": "synthetic-access", "refresh_token": "synthetic-refresh", "account_id": account}})
	return b
}

func nativeDefaultFixture(t *testing.T) config.Config {
	t.Helper()
	root := t.TempDir()
	c := config.Defaults()
	c.Runtime.SharedCodexHome = filepath.Join(root, "shared")
	c.Runtime.AccountsDir = filepath.Join(root, "accounts")
	c.Runtime.EnginesDir = filepath.Join(root, "engines")
	c.Storage.StateDir = filepath.Join(root, "state")
	if e := cr.EnsureAccountHome(c, "chosen"); e != nil {
		t.Fatal(e)
	}
	for p, b := range map[string][]byte{
		filepath.Join(c.Runtime.AccountsDir, "chosen", "auth.json"): syntheticChatGPT("chosen-id"),
		filepath.Join(c.Runtime.SharedCodexHome, "auth.json"):       []byte("previous-login"),
		filepath.Join(c.Runtime.SharedCodexHome, "config.toml"):     []byte("# retain comment\nmodel = 'gpt-6.1-sol'\ncli_auth_credentials_store = 'file'\n"),
		filepath.Join(c.Runtime.SharedCodexHome, "history.jsonl"):   []byte("retain history")} {
		if e := os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	return c
}

func TestNativeDefaultSharesOneCredentialAndPreservesConfigHistory(t *testing.T) {
	c := nativeDefaultFixture(t)
	backup, e := SetNativeDefault(c, "chosen")
	if e != nil {
		t.Fatal(e)
	}
	if b, _ := os.ReadFile(filepath.Join(backup, "auth.json")); string(b) != "previous-login" {
		t.Fatal("previous login backup was lost")
	}
	if info, e := os.Stat(filepath.Join(backup, "auth.json")); e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("backup is not private")
	}
	if info, e := os.Stat(backup); e != nil || info.Mode().Perm() != 0700 {
		t.Fatal("backup directory is not private")
	}
	dest := filepath.Join(c.Runtime.SharedCodexHome, "auth.json")
	source := filepath.Join(c.Runtime.AccountsDir, "chosen", "auth.json")
	if link, e := os.Readlink(dest); e != nil || link != source {
		t.Fatal("default did not select the named credential", e)
	}
	if info, e := os.Lstat(source); e != nil || !info.Mode().IsRegular() {
		t.Fatal("named credentials were replaced by a link")
	}
	if b, e := SetNativeDefault(c, "chosen"); e != nil || b != "" {
		t.Fatal("same default was not idempotent", e)
	}
	if e := os.WriteFile(dest, syntheticChatGPT("refreshed-id"), 0600); e != nil {
		t.Fatal(e)
	}
	if b, _ := os.ReadFile(source); !strings.Contains(string(b), "refreshed-id") {
		t.Fatal("default and named token files diverged")
	}
	for name, want := range map[string]string{"history.jsonl": "retain history", "config.toml": "# retain comment\nmodel = 'gpt-6.1-sol'\ncli_auth_credentials_store = 'file'\n"} {
		if b, _ := os.ReadFile(filepath.Join(c.Runtime.SharedCodexHome, name)); string(b) != want {
			t.Fatal("unrelated native data changed", name)
		}
	}
}

func TestNativeDefaultRejectsUnsafeStorageWithoutChangingLogin(t *testing.T) {
	for _, mode := range []string{"keyring", "auto", "ephemeral", "malformed", "named-link", "native-link", "invalid-auth", "forced-login", "forced-workspace"} {
		t.Run(mode, func(t *testing.T) {
			c := nativeDefaultFixture(t)
			dest := filepath.Join(c.Runtime.SharedCodexHome, "auth.json")
			source := filepath.Join(c.Runtime.AccountsDir, "chosen", "auth.json")
			switch mode {
			case "forced-login":
				os.WriteFile(filepath.Join(c.Runtime.SharedCodexHome, "config.toml"), []byte("forced_login_method='api'"), 0600)
			case "forced-workspace":
				os.WriteFile(filepath.Join(c.Runtime.SharedCodexHome, "config.toml"), []byte("forced_chatgpt_workspace_id='different-workspace'"), 0600)
			case "named-link":
				os.Remove(source)
				os.Symlink(dest, source)
			case "native-link":
				os.Remove(dest)
				os.Symlink(filepath.Join(t.TempDir(), "other"), dest)
			case "invalid-auth":
				os.WriteFile(source, []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"synthetic"}`), 0600)
			case "malformed":
				os.WriteFile(filepath.Join(c.Runtime.SharedCodexHome, "config.toml"), []byte("invalid = ["), 0600)
			default:
				os.WriteFile(filepath.Join(c.Runtime.SharedCodexHome, "config.toml"), []byte("cli_auth_credentials_store = '"+mode+"'"), 0600)
			}
			if _, e := SetNativeDefault(c, "chosen"); e == nil {
				t.Fatal("unsafe switch accepted")
			}
			if mode == "native-link" {
				if target, _ := os.Readlink(dest); !strings.HasSuffix(target, "/other") {
					t.Fatal("foreign link was replaced")
				}
			} else if b, _ := os.ReadFile(dest); string(b) != "previous-login" {
				t.Fatal("failed switch replaced the native login")
			}
		})
	}
}

func TestOfficialNativeDefaultLoginAndCredentialSave(t *testing.T) {
	if os.Getenv("CODEXM_NATIVE_TEST") != "1" {
		t.Skip("requires isolated official CLI integration")
	}
	binary := os.Getenv("CODEXM_NATIVE_RUNTIME")
	if binary == "" {
		t.Fatal("official runtime missing")
	}
	c := nativeDefaultFixture(t)
	if _, e := SetNativeDefault(c, "chosen"); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--no-daemon", "login", "status")
	cmd.Env = cr.Env(c, "", binary)
	if e := cmd.Run(); e != nil {
		t.Fatal("new official native launch did not accept selected ChatGPT login", e)
	}
	// A synthetic API-key save exercises the native file writer locally only.
	// It cannot send a model request and does not use any real credential.
	cmd = exec.CommandContext(ctx, binary, "--no-daemon", "login", "--with-api-key")
	cmd.Env = cr.Env(c, "", binary)
	cmd.Stdin = strings.NewReader("synthetic-after\n")
	if e := cmd.Run(); e != nil {
		t.Fatal("official local credential save failed", e)
	}
	if _, e := os.Readlink(filepath.Join(c.Runtime.SharedCodexHome, "auth.json")); e != nil {
		t.Fatal("native save replaced the managed link")
	}
	b, _ := os.ReadFile(filepath.Join(c.Runtime.AccountsDir, "chosen", "auth.json"))
	var a map[string]any
	if json.Unmarshal(b, &a) != nil || a["OPENAI_API_KEY"] != "synthetic-after" {
		t.Fatal("native writer did not update the selected named auth")
	}
}
