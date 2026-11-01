package cli

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeasy-digix/codexm/internal/config"
)

func TestChooseAccountUsesLastRegistrationOrNativeDefault(t *testing.T) {
	root := t.TempDir()
	c := config.Defaults()
	c.Runtime.AccountsDir = filepath.Join(root, "accounts")
	c.Runtime.SharedCodexHome = filepath.Join(root, "shared")
	home := filepath.Join(c.Runtime.AccountsDir, "alpha")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"tokens":{"account_id":"fixture"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		previous, input, want string
		removed               bool
	}{{"alpha", "\n", "alpha", false}, {"", "\n", "default", false}, {"gone", "\n", "default", false}, {"alpha", "\n", "default", true}, {"alpha", "0\n", "default", false}} {
		c.AccountRegistry.Removed = nil
		if tc.removed {
			c.AccountRegistry.Removed = []string{"alpha"}
		}
		got, err := chooseAccount(c, bufio.NewScanner(strings.NewReader(tc.input)), tc.previous)
		if err != nil || got != tc.want {
			t.Fatalf("previous %q removed %v: got %q want %q error %v", tc.previous, tc.removed, got, tc.want, err)
		}
	}
}
