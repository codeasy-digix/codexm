package accounts

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codeasy-digix/codexm/internal/config"
)

func identityFixtureAuth(user, workspace, token string) []byte {
	claims, _ := json.Marshal(map[string]any{"sub": user, "email": user + "@example.invalid", "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": workspace}})
	data, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]string{"account_id": workspace, "id_token": "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(claims) + ".fixture", "access_token": token, "refresh_token": "fixture-refresh"}}) // gitleaks:allow -- unsigned, synthetic test JWT
	return data
}

func TestCopiedDefaultIdentitySeparatesMembersAndSurvivesTokenRefresh(t *testing.T) {
	c := config.Defaults()
	root := t.TempDir()
	c.Runtime.AccountsDir = filepath.Join(root, "accounts")
	c.Runtime.SharedCodexHome = filepath.Join(root, "shared")
	write := func(path string, data []byte) {
		t.Helper()
		if err := config.AtomicWrite(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(c.Runtime.AccountsDir, "alpha", "auth.json"), identityFixtureAuth("alice", "team", "old-token"))
	write(filepath.Join(c.Runtime.AccountsDir, "beta", "auth.json"), identityFixtureAuth("bob", "team", "other-token"))
	p := filepath.Join(c.Runtime.SharedCodexHome, "auth.json")
	write(p, identityFixtureAuth("alice", "team", "refreshed-token"))
	if got := CurrentNickname(c, "", ""); got != "alpha" {
		t.Fatalf("wrong member: %q", got)
	}
	before := InspectIdentity(c, "default")
	write(p, identityFixtureAuth("alice", "team", "another-token"))
	if got := InspectIdentity(c, "default"); got.Revision != before.Revision {
		t.Fatal("token refresh invalidated identity")
	}
	write(filepath.Join(c.Runtime.AccountsDir, "alias", "auth.json"), identityFixtureAuth("alice", "team", "alias-token"))
	if got := CurrentNickname(c, "", ""); got != "" {
		t.Fatalf("guessed ambiguous alias: %q", got)
	}
	write(p, identityFixtureAuth("nobody", "team", "new-token"))
	if got := CurrentNickname(c, "", ""); got != "" {
		t.Fatal("workspace alone matched a different user")
	}
}

func TestCurrentAccountIdentityMatchesExistingCredentialWithoutChanges(t *testing.T) {
	c := config.Defaults()
	root := t.TempDir()
	c.Runtime.AccountsDir = filepath.Join(root, "accounts")
	c.Runtime.SharedCodexHome = filepath.Join(root, "shared")
	for name, id := range map[string]string{"account-work": "account-a", "account-secondary": "account-b"} {
		if e := config.AtomicWrite(filepath.Join(c.Runtime.AccountsDir, name, "auth.json"), []byte(`{"tokens":{"account_id":"`+id+`","refresh_token":"private-token"}}`), 0600); e != nil {
			t.Fatal(e)
		}
	}
	p := filepath.Join(c.Runtime.SharedCodexHome, "auth.json")
	b := []byte(`{"tokens":{"account_id":"account-b","refresh_token":"private-token"}}`)
	config.AtomicWrite(p, b, 0600)
	if got := CurrentNickname(c, "", ""); got != "account-secondary" {
		t.Fatal(got)
	}
	if got := CurrentNickname(c, filepath.Join(c.Runtime.AccountsDir, "account-work"), "account-work"); got != "account-work" {
		t.Fatal(got)
	}
	if got := CurrentNickname(c, filepath.Join(root, "missing"), "account-secondary"); got != "" {
		t.Fatal("guessed unknown identity", got)
	}
	if after, _ := os.ReadFile(p); string(after) != string(b) {
		t.Fatal("credential file changed")
	}
}
