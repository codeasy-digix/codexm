package accounts

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestPinNativeStoragePreservesExistingFilesAndDefaultSwitchKeepsConfig(t *testing.T) {
	c := nativeDefaultFixture(t)
	home := c.Runtime.SharedCodexHome
	filename := filepath.Join(home, "config.toml")
	before, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(c.Runtime.AccountsDir, "chosen")
	for _, dir := range []string{home, private} {
		for _, name := range []string{"state_5.sqlite", "thread_history_1.sqlite", "queue_1.sqlite"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	backup, err := PinNativeStorage(c)
	if err != nil || backup == "" {
		t.Fatal("storage setup failed", err)
	}
	if saved, _ := os.ReadFile(backup); !bytes.Equal(saved, before) {
		t.Fatal("original config backup differs")
	}
	data, err := os.ReadFile(filename)
	if err != nil || !bytes.HasSuffix(data, before) {
		t.Fatal("storage setup rewrote existing settings", err)
	}
	var parsed nativeConfig
	if err := toml.Unmarshal(data, &parsed); err != nil || parsed.SQLiteHome == nil {
		t.Fatal("native sqlite_home is missing", err)
	}
	if pinned, err := NativeStoragePinned(c); err != nil || !pinned {
		t.Fatal("configured storage failed verification", err)
	}
	if original, _ := os.ReadFile(filepath.Join(home, "auth.json")); string(original) != "previous-login" {
		t.Fatal("storage setup changed the original login")
	}
	for _, dir := range []string{home, private} {
		for _, name := range []string{"state_5.sqlite", "thread_history_1.sqlite", "queue_1.sqlite"} {
			if raw, _ := os.ReadFile(filepath.Join(dir, name)); string(raw) != name {
				t.Fatal("storage setup changed a database", name)
			}
		}
	}
	if _, err = SetNativeDefault(c, "chosen"); err != nil {
		t.Fatal(err)
	}
	if current, _ := os.ReadFile(filename); !bytes.Equal(current, data) {
		t.Fatal("switching the login changed the pinned native configuration")
	}
	if backup, err := PinNativeStorage(c); err != nil || backup != "" {
		t.Fatal("already pinned storage was rewritten", err)
	}
}

func TestNativeAccountSelectionPreservesConflictingSQLitePath(t *testing.T) {
	for _, value := range []string{".", t.TempDir()} {
		c := nativeDefaultFixture(t)
		filename := filepath.Join(c.Runtime.SharedCodexHome, "config.toml")
		data, err := toml.Marshal(map[string]string{"sqlite_home": value})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := PinNativeStorage(c); err == nil {
			t.Fatal("storage setup accepted a conflicting path")
		}
		if _, err := NativeStoragePinned(c); err == nil {
			t.Fatal("native storage check accepted a split store")
		}
		if err := ValidateNativeAccount(c, "chosen"); err != nil {
			t.Fatal("a client that supplies its own storage path lost access to a valid login", err)
		}
		if _, err := SetNativeDefault(c, "chosen"); err == nil {
			t.Fatal("default account selection accepted a split store")
		}
		if current, _ := os.ReadFile(filename); !bytes.Equal(current, data) {
			t.Fatal("conflicting config was changed")
		}
		if original, _ := os.ReadFile(filepath.Join(c.Runtime.SharedCodexHome, "auth.json")); string(original) != "previous-login" {
			t.Fatal("failing storage validation changed the original login")
		}
	}
}
