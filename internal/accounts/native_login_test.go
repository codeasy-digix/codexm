package accounts

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/pelletier/go-toml/v2"
)

func TestNativeLoginRestoresOnlyRegistrationAndPreservesLegacyPolicy(t *testing.T) {
	c, b, transports := loginFixture(t)
	// Compact login must not invoke the legacy config validator or recreate policy.
	before := []byte("[manager]\naccount='obsolete invalid!'\ninvocation='retired'\n[worker_accounts]\npriority=['other']\n[account_registry]\nremoved=['account-secondary','keep-removed']\n")
	if err := os.WriteFile(c.Path, before, 0600); err != nil {
		t.Fatal(err)
	}
	b.publish = publishNativeLogin
	login, err := b.Start(context.Background(), "account-secondary", "device")
	if err != nil {
		t.Fatal(err)
	}
	f := (*transports)[0]
	credentials := fixtureAuth(f.accountID, "new")
	if err = config.AtomicWrite(filepath.Join(f.home, "auth.json"), credentials, 0600); err != nil {
		t.Fatal(err)
	}
	f.complete(true, f.serverID)
	waitLogin(t, b, login.LoginID, "succeeded")
	waitLoginCleanup(t, b, login.LoginID)
	after, err := os.ReadFile(c.Path)
	if err != nil {
		t.Fatal(err)
	}
	var oldDoc, newDoc map[string]any
	if toml.Unmarshal(before, &oldDoc) != nil || toml.Unmarshal(after, &newDoc) != nil {
		t.Fatal("invalid config")
	}
	oldDoc["account_registry"].(map[string]any)["removed"] = []any{"keep-removed"}
	if !reflect.DeepEqual(oldDoc, newDoc) {
		t.Fatal("native login rewrote unrelated policy")
	}
	assertSavedBytes(t, filepath.Join(c.Runtime.AccountsDir, "account-secondary", "auth.json"), credentials)
	assertSavedBytes(t, filepath.Join(c.Runtime.SharedCodexHome, "auth.json"), []byte("native-login-canary"))
	assertSavedBytes(t, c.Path+".previous", before)
}

func TestNativeLoginNewAccountDoesNotRewriteConfiguration(t *testing.T) {
	c, b, transports := loginFixture(t)
	b.publish = publishNativeLogin
	before, _ := os.ReadFile(c.Path)
	login, err := b.Start(context.Background(), "new-account", "browser")
	if err != nil {
		t.Fatal(err)
	}
	f := (*transports)[0]
	if err = config.AtomicWrite(filepath.Join(f.home, "auth.json"), fixtureAuth(f.accountID, "new"), 0600); err != nil {
		t.Fatal(err)
	}
	f.complete(true, f.serverID)
	waitLogin(t, b, login.LoginID, "succeeded")
	waitLoginCleanup(t, b, login.LoginID)
	assertSavedBytes(t, c.Path, before)
	assertSavedBytes(t, filepath.Join(c.Runtime.SharedCodexHome, "auth.json"), []byte("native-login-canary"))
}
