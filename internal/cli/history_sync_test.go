package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetiredHistorySyncCannotReadConfigurationOrStartWork(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(h, "state"))
	t.Setenv("CODEXM_CONFIG", filepath.Join(h, "bad.toml"))
	if err := os.WriteFile(filepath.Join(h, "bad.toml"), []byte("invalid [ TOML"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"history-sync"}, {"history-sync", "daemon"}, {"history-sync", "sync"}, {"history-sync", "serve"}, {"history-sync", "service", "start"}, {"--config", "missing", "history-sync", "--help"}} {
		err := Main(args)
		if err == nil || !strings.Contains(err.Error(), "history-sync has been removed") {
			t.Fatalf("retired command did not fail closed: %v %v", args, err)
		}
	}
	for _, name := range []string{".codex", ".codex-history-sync", "state", "config"} {
		if _, err := os.Stat(filepath.Join(h, name)); !os.IsNotExist(err) {
			t.Fatalf("retired sync touched %s", name)
		}
	}
}
