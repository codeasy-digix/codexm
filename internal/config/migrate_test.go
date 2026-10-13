package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestWorkerDefaultMigrationPreservesUnknownSettingsAndOriginalBackup(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	original := "[worker_accounts]\npriority=['account-work','account-secondary','account-main','account-backup']\ndefault_account='account-secondary'\n[custom]\nvalue='preserve me'\n"
	if e := os.WriteFile(p, []byte(original), 0600); e != nil {
		t.Fatal(e)
	}
	changed, e := RemoveLegacyWorkerDefault(p)
	if e != nil || !changed {
		t.Fatal("legacy default was not removed", e)
	}
	b, _ := os.ReadFile(p)
	previous, _ := os.ReadFile(p + ".previous")
	var values map[string]any
	if toml.Unmarshal(b, &values) != nil || strings.Contains(string(b), "default_account") || values["custom"].(map[string]any)["value"] != "preserve me" || string(previous) != original {
		t.Fatal("unrelated settings or backup were lost")
	}
	changed, e = RemoveLegacyWorkerDefault(p)
	after, _ := os.ReadFile(p)
	savedBackup, _ := os.ReadFile(p + ".previous")
	if e != nil || changed || string(after) != string(b) || string(savedBackup) != original {
		t.Fatal("migration was not idempotent", e)
	}
}

func TestWorkerDefaultMigrationDoesNotCreateFirstRunConfiguration(t *testing.T) {
	p := filepath.Join(t.TempDir(), "new", "config.toml")
	if changed, e := RemoveLegacyWorkerDefault(p); e != nil || changed {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Dir(p)); !os.IsNotExist(e) {
		t.Fatal("migration created a first-run configuration")
	}
}
