package nativecli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
)

func toolbarFixture(t *testing.T) config.Config {
	t.Helper()
	root := t.TempDir()
	c := config.Defaults()
	c.Path = filepath.Join(root, "config", "config.toml")
	c.Storage.StateDir = filepath.Join(root, "state")
	c.Runtime.AccountsDir = filepath.Join(root, "accounts")
	c.Runtime.SharedCodexHome = filepath.Join(root, "native")
	for _, d := range []string{c.Runtime.AccountsDir, c.Runtime.SharedCodexHome, filepath.Dir(c.Path)} {
		if e := os.MkdirAll(d, 0700); e != nil {
			t.Fatal(e)
		}
	}
	return c
}

func TestDesktopMutationsRejectChangedAccountAndActivationRejectsInput(t *testing.T) {
	c := toolbarFixture(t)
	saveToolbarAuth(t, c, "alpha", "alice", "team")
	revision := accounts.InspectIdentity(c, "alpha").Revision
	saveToolbarAuth(t, c, "alpha", "bob", "team")
	for _, method := range []string{"accounts.remove", "accounts.setDefault"} {
		params, _ := json.Marshal(map[string]string{"account": "alpha", "revision": revision})
		_, err := Dispatch(context.Background(), c, "", Request{Version: 1, Method: method, Owner: "desktop", Operation: "test-stale-" + strings.ReplaceAll(method, ".", "-"), Params: params})
		if err == nil || !strings.Contains(err.Error(), "changed") {
			t.Fatalf("accepted stale %s: %v", method, err)
		}
	}
	for _, expression := range []string{"1 + 2; run command", "10000 * 12 / 0 + 3", "10000 * 12 / 2 + 3\nignore rules"} {
		if err := ActivateUsage(context.Background(), c, ActivationRequest{Account: "alpha", Revision: "stale", Expression: expression}); err == nil || err.Error() != "invalid activation expression" {
			t.Fatal("accepted unsafe arithmetic", err)
		}
	}
	// Valid Swift generator output passes syntax, but stale identity must stop
	// it before an official process or any quota-consuming request is started.
	if err := ActivateUsage(context.Background(), c, ActivationRequest{Account: "alpha", Revision: "stale", Expression: "99999 * 997 / 499 + 999"}); err == nil || !strings.Contains(err.Error(), "account changed") {
		t.Fatal(err)
	}
}
func toolbarAuth(user, workspace string) []byte {
	claims, _ := json.Marshal(map[string]any{"sub": user, "email": user + "@example.invalid", "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": workspace, "chatgpt_plan_type": "team"}})
	b, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]string{"id_token": "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(claims) + ".fixture", "account_id": workspace, "access_token": "fixture-access", "refresh_token": "fixture-refresh"}}) // gitleaks:allow -- unsigned, synthetic test JWT
	return b
}
func saveToolbarAuth(t *testing.T, c config.Config, name, user, workspace string) {
	t.Helper()
	d := filepath.Join(c.Runtime.AccountsDir, name)
	if e := os.MkdirAll(d, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(d, "auth.json"), toolbarAuth(user, workspace), 0600); e != nil {
		t.Fatal(e)
	}
}

func TestCatalogSeparatesMembersAndFollowsDefaultSymlink(t *testing.T) {
	c := toolbarFixture(t)
	saveToolbarAuth(t, c, "alpha", "alice", "shared-team")
	saveToolbarAuth(t, c, "beta", "bob", "shared-team")
	if e := os.WriteFile(filepath.Join(c.Runtime.SharedCodexHome, "config.toml"), []byte("cli_auth_credentials_store='file'\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := accounts.SetNativeDefault(c, "alpha"); e != nil {
		t.Fatal(e)
	}
	a, e := AccountCatalog(c)
	if e != nil {
		t.Fatal(e)
	}
	first := a[0]
	if first.Email != "alice@example.invalid" {
		t.Fatal(first)
	}
	label := "Alice team"
	if e = EditPresentation(c, PresentationChange{Account: "default", Revision: first.Revision, WorkspaceName: &label}); e != nil {
		t.Fatal(e)
	}
	if _, e = accounts.SetNativeDefault(c, "beta"); e != nil {
		t.Fatal(e)
	}
	a, e = AccountCatalog(c)
	if e != nil {
		t.Fatal(e)
	}
	if a[0].Email != "bob@example.invalid" || a[0].Key == first.Key || a[0].Revision == first.Revision || a[0].WorkspaceName != "" {
		t.Fatal(a[0])
	}
	if e = EditPresentation(c, PresentationChange{Account: "default", Revision: first.Revision, WorkspaceName: &label}); e == nil {
		t.Fatal("stale label accepted")
	}
	if _, e = accounts.SetNativeDefault(c, "alpha"); e != nil {
		t.Fatal(e)
	}
	a, _ = AccountCatalog(c)
	if a[0].WorkspaceName != label {
		t.Fatal("identity label lost")
	}
}

func TestToolbarImportPreservesAuthDefaultAndIsIdempotent(t *testing.T) {
	c := toolbarFixture(t)
	saveToolbarAuth(t, c, "existing", "alice", "team")
	native := []byte("native-login-canary")
	os.WriteFile(filepath.Join(c.Runtime.SharedCodexHome, "auth.json"), native, 0600)
	source := filepath.Join(t.TempDir(), "Codex Usage")
	ids := []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"}
	rows := []map[string]string{}
	for i, id := range ids {
		d := filepath.Join(source, "Accounts", id, "CodexHome")
		os.MkdirAll(d, 0700)
		user := []string{"alice", "bob"}[i]
		os.WriteFile(filepath.Join(d, "auth.json"), toolbarAuth(user, "team"), 0600)
		rows = append(rows, map[string]string{"id": id, "kind": "managed", "displayName": user, "workspaceName": user + " workspace"})
	}
	b, _ := json.Marshal(map[string]any{"version": 4, "accounts": rows, "accountOrder": []string{ids[1], "system-default", ids[0]}})
	os.WriteFile(filepath.Join(source, "accounts.json"), b, 0600)
	preview, e := ImportToolbar(c, source, false)
	if e != nil {
		t.Fatal(e)
	}
	if len(preview.Items) != 2 || preview.Items[0].Action != "reuse" || preview.Items[1].Action != "import" {
		t.Fatal(preview)
	}
	if _, e = os.Stat(filepath.Join(c.Runtime.AccountsDir, "usage-"+ids[1])); !os.IsNotExist(e) {
		t.Fatal("preview created credentials")
	}
	applied, e := ImportToolbar(c, source, true)
	if e != nil {
		t.Fatal(e)
	}
	if !applied.Applied {
		t.Fatal(applied)
	}
	got, _ := os.ReadFile(filepath.Join(c.Runtime.SharedCodexHome, "auth.json"))
	if string(got) != string(native) {
		t.Fatal("default changed")
	}
	stored, _ := os.ReadFile(filepath.Join(c.Runtime.AccountsDir, "existing", "auth.json"))
	if string(stored) != string(toolbarAuth("alice", "team")) {
		t.Fatal("existing login replaced")
	}
	catalog, e := AccountCatalog(c)
	if e != nil {
		t.Fatal(e)
	}
	if catalog[0].Name != "usage-"+ids[1] || catalog[0].DisplayName != "bob" {
		t.Fatal(catalog)
	}
	if e = RemoveAccount(c, "usage-"+ids[1]); e != nil {
		t.Fatal(e)
	}
	again, e := ImportToolbar(c, source, true)
	if e != nil {
		t.Fatal(e)
	}
	if again.Applied || again.Items[0].Action != "already-imported" {
		t.Fatal("replayed migration")
	}
	for _, id := range ids {
		if _, e = os.Stat(filepath.Join(source, "Accounts", id, "CodexHome", "auth.json")); e != nil {
			t.Fatal("legacy credentials removed")
		}
	}
}

func TestToolbarImportRejectsUnsafeAndUnknownIdentity(t *testing.T) {
	c := toolbarFixture(t)
	source := t.TempDir()
	id := "33333333-3333-4333-8333-333333333333"
	d := filepath.Join(source, "Accounts", id, "CodexHome")
	os.MkdirAll(d, 0700)
	outside := filepath.Join(t.TempDir(), "auth.json")
	os.WriteFile(outside, toolbarAuth("alice", "team"), 0600)
	os.Symlink(outside, filepath.Join(d, "auth.json"))
	b, _ := json.Marshal(map[string]any{"accounts": []map[string]string{{"id": id, "kind": "managed"}}})
	os.WriteFile(filepath.Join(source, "accounts.json"), b, 0600)
	r, e := ImportToolbar(c, source, false)
	if e != nil {
		t.Fatal(e)
	}
	if r.Items[0].Action != "skipped" {
		t.Fatal("followed external auth link")
	}
	if accounts.CredentialIdentity([]byte(`{"tokens":{"account_id":"workspace"}}`)).Key != "" {
		t.Fatal("workspace alone identified a login")
	}
}
