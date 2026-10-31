package historysynccli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceStatusJSONDoesNotNeedManagerOrRelayConfig(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(h, "state"))
	var out bytes.Buffer
	if err := run(context.Background(), []string{"service", "status", "--json"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %s", out.String())
	}
	if got["installed"] != false || got["running"] != false {
		t.Fatal("unconfigured service claimed active", got)
	}
}

func TestConfigPreferenceAndBrokenLinkNeverFallBack(t *testing.T) {
	h := t.TempDir()
	x := filepath.Join(h, "custom-config")
	t.Setenv("XDG_CONFIG_HOME", x)
	canonical := filepath.Join(x, "codexm", "history-sync.json")
	legacy := filepath.Join(h, ".codex-history-sync", "v2", "config.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("legacy"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := defaultConfigPath(h); err != nil || got != legacy {
		t.Fatalf("legacy: %s %v", got, err)
	}
	if err := os.MkdirAll(filepath.Dir(canonical), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(h, "missing"), canonical); err != nil {
		t.Fatal(err)
	}
	if got, err := defaultConfigPath(h); err != nil || got != canonical {
		t.Fatalf("broken canonical config fell back: %s %v", got, err)
	}
}

func TestRelayServiceCannotReplaceExistingSystemUnit(t *testing.T) {
	p := filepath.Join(t.TempDir(), "old.plist")
	if err := os.WriteFile(p, []byte("existing unit"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := rejectLegacyService(p); err == nil {
		t.Fatal("accepted duplicate service")
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "existing unit" {
		t.Fatal("legacy service changed")
	}
	if err := rejectLegacyService(p + "-absent"); err != nil {
		t.Fatal(err)
	}
}

func TestRelayUnitUsesProductCommandAndSeparateReceipt(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := relayService("/tmp/relay & settings.json", "/tmp/product/codexm")
	other := relayService("/tmp/other.json", "/tmp/product/codexm")
	if m.Label() == other.Label() || !strings.Contains(m.Label(), "history-sync") {
		t.Fatal("service identity is not isolated")
	}
	for _, platform := range []string{"darwin", "linux"} {
		b, err := m.Definition(platform)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if !strings.Contains(s, "history-sync") || !strings.Contains(s, "daemon") || strings.Contains(s, "__service") || strings.Contains(s, ".home") || strings.Contains(s, "codex-accounts") {
			t.Fatalf("wrong service definition: %s", s)
		}
	}
}

func TestServiceStartRequiresExplicitEnabledConfiguration(t *testing.T) {
	h := t.TempDir()
	p := filepath.Join(h, "sync.json")
	if err := os.WriteFile(p, []byte(`{"node":"test","home":"`+filepath.ToSlash(h)+`/native","store":"`+filepath.ToSlash(h)+`/store","enabled":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateServiceConfig(p, false); err != nil {
		t.Fatal(err)
	}
	if err := validateServiceConfig(p, true); err == nil {
		t.Fatal("disabled relay started")
	}
}
