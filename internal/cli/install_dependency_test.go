package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerRequiresCodexBeforeAnyInstallationChanges(t *testing.T) {
	repo := t.TempDir()
	source, err := os.ReadFile(filepath.Join("..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(repo, "install.sh")
	os.WriteFile(script, source, 0700)
	os.WriteFile(filepath.Join(repo, "VERSION"), []byte("0.2.0\n"), 0600)
	bin := filepath.Join(repo, "only-system-tools")
	os.Mkdir(bin, 0700)
	for _, name := range []string{"dirname", "cat", "uname", "tr"} {
		p, e := exec.LookPath(name)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.Symlink(p, filepath.Join(bin, name)); e != nil {
			t.Fatal(e)
		}
	}
	installRoot := filepath.Join(repo, "install")
	linkDir := filepath.Join(repo, "entrypoints")
	for _, args := range [][]string{nil, {"--build"}} {
		cmd := exec.Command("/bin/sh", append([]string{script}, args...)...)
		cmd.Env = []string{"PATH=" + bin, "HOME=" + repo, "CODEXM_INSTALL_ROOT=" + installRoot, "CODEXM_LINK_DIR=" + linkDir}
		out, e := cmd.CombinedOutput()
		if e == nil || !strings.Contains(string(out), "Install the official Codex CLI first") {
			t.Fatalf("missing dependency not explained: %v %s", e, out)
		}
		for _, p := range []string{installRoot, linkDir} {
			if _, e = os.Lstat(p); !os.IsNotExist(e) {
				t.Fatal("installer created files without Codex", p, e)
			}
		}
	}
	// A broken installed codex must leave an existing manager untouched too.
	os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\nexit 7\n"), 0700)
	os.MkdirAll(filepath.Join(linkDir), 0700)
	current := filepath.Join(linkDir, "codexm")
	os.Symlink("previous-manager", current)
	cmd := exec.Command("/bin/sh", script, "--build")
	cmd.Env = []string{"PATH=" + bin, "HOME=" + repo, "CODEXM_INSTALL_ROOT=" + installRoot, "CODEXM_LINK_DIR=" + linkDir}
	if out, e := cmd.CombinedOutput(); e == nil || !strings.Contains(string(out), "Codex CLI is required") {
		t.Fatalf("broken dependency accepted: %v %s", e, out)
	}
	if target, _ := os.Readlink(current); target != "previous-manager" {
		t.Fatal("existing manager changed")
	}
}
