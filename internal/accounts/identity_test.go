package accounts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codeasy-digix/codexm/internal/config"
)

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
