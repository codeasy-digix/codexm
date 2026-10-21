package webhost

import (
	"context"
	"encoding/json"
	"github.com/codeasy-digix/codexm/internal/config"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingBundleDoesNotPublishLaunch(t *testing.T) {
	c := config.Defaults()
	c.Storage.StateDir = t.TempDir()
	c.Path = filepath.Join(c.StateDir(), "config.toml")
	err := Command(context.Background(), c, "/safe/codexm", []string{"--bundle", filepath.Join(c.StateDir(), "absent"), "--no-open", "--port", "43571"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(c.StateDir(), "web", "launch.json")); !os.IsNotExist(err) {
		t.Fatal("failed launch published state", err)
	}
}
func TestFailedChildDoesNotClaimReadiness(t *testing.T) {
	c := config.Defaults()
	c.Storage.StateDir = t.TempDir()
	c.Path = filepath.Join(c.StateDir(), "config.toml")
	state := filepath.Join(c.StateDir(), "web")
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	spec := Spec{State: state, Config: c.Path, Origin: "http://127.0.0.1:43571", Program: "/bin/sh", Directory: state, Args: []string{"-c", "exit 7"}}
	b, _ := json.Marshal(spec)
	path := filepath.Join(state, "launch.json")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), c, path); err == nil {
		t.Fatal("failed child reported success")
	}
	if _, err := os.Stat(filepath.Join(state, "ready.json")); !os.IsNotExist(err) {
		t.Fatal("failed child left readiness")
	}
}
