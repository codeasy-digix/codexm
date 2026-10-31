package cli

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
)

func TestHistoryRelayIsIndependentOfAccountAndManagerConfiguration(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, "config"))
	t.Setenv("CODEXM_CONFIG", filepath.Join(h, "invalid.toml"))
	if err := os.WriteFile(filepath.Join(h, "invalid.toml"), []byte("not [ valid TOML"), 0600); err != nil {
		t.Fatal(err)
	}
	oldLimit := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(oldLimit) })
	if err := Main([]string{"history-sync", "--help"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(h, ".codex")); !os.IsNotExist(err) {
		t.Fatal("relay help touched native data")
	}
	if err := Main([]string{"--config", "manager.toml", "history-sync", "--help"}); err == nil {
		t.Fatal("ambiguous config flag accepted")
	}
}
