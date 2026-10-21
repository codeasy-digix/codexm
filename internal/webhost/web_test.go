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

func TestWebNodeIsIndependentOfShellDefault(t *testing.T) {
	dir := t.TempDir()
	for name, version := range map[string]string{"node": "26.0.0", "web-node": "24.15.0"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\necho v"+version+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	c := config.Defaults()
	if _, err := nodeProgram(context.Background(), c); err == nil {
		t.Fatal("unsupported shell Node accepted")
	}
	c.Runtime.WebNodeBinary = filepath.Join(dir, "web-node")
	program, err := nodeProgram(context.Background(), c)
	if err != nil || program != c.Runtime.WebNodeBinary {
		t.Fatal("configured Node was not used", program, err)
	}
	c.Runtime.WebNodeBinary = filepath.Join(dir, "missing")
	if _, err := nodeProgram(context.Background(), c); err == nil {
		t.Fatal("missing configured Node fell back to shell Node")
	}
	c.Runtime.WebNodeBinary = "relative-node"
	if _, err := nodeProgram(context.Background(), c); err == nil {
		t.Fatal("relative configured Node accepted")
	}
}
