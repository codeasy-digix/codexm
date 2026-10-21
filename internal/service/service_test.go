package service

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeasy-digix/codexm/internal/config"
)

func TestDefinitionsEscapePathsAndKeepWorkersAcrossManagerRestart(t *testing.T) {
	c := config.Defaults()
	c.Path = "/home/test/a & b/manager.toml"
	c.Storage.StateDir = "/home/test/private % & state"
	m := Manager{Config: c, Self: "/home/test/a & b/codexm"}
	b, e := m.Definition("darwin")
	if e != nil {
		t.Fatal(e)
	}
	var parsed any
	if e = xml.Unmarshal(b, &parsed); e != nil {
		t.Fatal("invalid plist", e)
	}
	if !strings.Contains(string(b), "a &amp; b") || !strings.Contains(string(b), "AbandonProcessGroup") || !strings.Contains(string(b), "<string>Background</string>") {
		t.Fatal(string(b))
	}
	b, e = m.Definition("linux")
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(b), "%%") || !strings.Contains(string(b), "KillMode=process") || !strings.Contains(string(b), "--config") {
		t.Fatal(string(b))
	}
	if m.Label() == (Manager{Config: config.Defaults()}).Label() {
		t.Fatal("custom configuration collides with production service")
	}
}

func TestServiceKeepsRecordedDomainAcrossSessionChanges(t *testing.T) {
	c := config.Defaults()
	c.Storage.StateDir = t.TempDir()
	m := Manager{Config: c}
	for _, prefix := range []string{"gui", "user"} {
		domain := fmt.Sprintf("%s/%d", prefix, os.Getuid())
		if err := m.write(Receipt{Label: m.Label(), Definition: m.DefinitionPath(), Domain: domain}); err != nil {
			t.Fatal(err)
		}
		if m.domain() != domain {
			t.Fatal("lost recorded service domain", domain)
		}
	}
}

func TestStoppedServiceDoesNotAutostart(t *testing.T) {
	c := config.Defaults()
	c.Storage.StateDir = t.TempDir()
	c.Path = filepath.Join(c.StateDir(), "config.toml")
	m := Manager{Config: c, Self: "/safe/codexm"}
	if e := m.write(Receipt{Label: m.Label(), Definition: m.DefinitionPath(), Enabled: false}); e != nil {
		t.Fatal(e)
	}
	if e := m.Ensure(context.Background()); e == nil || !strings.Contains(e.Error(), "explicitly stopped") {
		t.Fatal(e)
	}
	if _, e := os.Stat(m.DefinitionPath()); !os.IsNotExist(e) {
		t.Fatal("test touched an OS service", e)
	}
}
