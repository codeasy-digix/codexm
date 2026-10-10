package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInstallerChecksArchiveBeforeReplacingCurrent(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Unix installer")
	}
	repo := t.TempDir()
	write := func(name string, data []byte, mode os.FileMode) {
		t.Helper()
		if e := os.MkdirAll(filepath.Dir(name), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(name, data, mode); e != nil {
			t.Fatal(e)
		}
	}
	source, e := os.ReadFile(filepath.Join("..", "..", "install.sh"))
	if e != nil {
		t.Fatal(e)
	}
	script := filepath.Join(repo, "tools", "codexm", "install.sh")
	write(script, source, 0700)
	write(filepath.Join(repo, "config", "home", "codexm-release.txt"), []byte("codexm-v0.1.0\n"), 0600)
	platform := runtime.GOOS + "-" + runtime.GOARCH
	asset := "codexm-" + platform + ".tar.gz"
	archive := filepath.Join(repo, asset)
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tarWriter := tar.NewWriter(gz)
	binary := []byte("#!/bin/sh\necho 'codexm 0.1.0'\n")
	if e = tarWriter.WriteHeader(&tar.Header{Name: "codexm", Mode: 0755, Size: int64(len(binary))}); e != nil {
		t.Fatal(e)
	}
	tarWriter.Write(binary)
	tarWriter.Close()
	gz.Close()
	valid := append([]byte(nil), b.Bytes()...)
	write(archive, valid, 0600)
	write(filepath.Join(repo, "manifests", "codexm-assets.sha256"), []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(valid), asset)), 0600)
	bin := filepath.Join(repo, "test-bin")
	write(filepath.Join(bin, "gh"), []byte(`#!/bin/sh
set -eu
download_dest=
while [ "$#" -gt 0 ]; do
  if [ "$1" = --dir ]; then shift; download_dest=$1; fi
  shift
done
cp "$CODEXM_TEST_ARCHIVE" "$download_dest/$CODEXM_TEST_ASSET"
`), 0700)
	platformRoot := filepath.Join(repo, "artifacts", platform)
	old := filepath.Join(platformRoot, "codexm", "previous")
	write(filepath.Join(old, "codexm"), []byte("#!/bin/sh\necho 'codexm 0.0.0'\n"), 0700)
	current := filepath.Join(platformRoot, "codexm-current")
	if e = os.Symlink("codexm/previous", current); e != nil {
		t.Fatal(e)
	}
	run := func() error {
		cmd := exec.Command("sh", script)
		cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "XDG_CACHE_HOME="+filepath.Join(repo, "cache"), "CODEXM_TEST_ARCHIVE="+archive, "CODEXM_TEST_ASSET="+asset)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, out)
		}
		return nil
	}
	write(archive, []byte("invalid download"), 0600)
	if e = run(); e == nil {
		t.Fatal("invalid checksum accepted")
	}
	if target, _ := os.Readlink(current); target != "codexm/previous" {
		t.Fatal("failed download replaced current", target)
	}
	write(archive, valid, 0600)
	if e = run(); e != nil {
		t.Fatal(e)
	}
	if target, _ := os.Readlink(current); target != "codexm/codexm-v0.1.0" {
		t.Fatal("validated release not installed", target)
	}
	if e = run(); e != nil {
		t.Fatal("idempotent reinstall failed", e)
	}
	// Keep both the selected old install and a conflicting release untouched.
	os.Remove(current)
	os.Symlink("codexm/previous", current)
	conflict := filepath.Join(platformRoot, "codexm", "codexm-v0.1.0", "codexm")
	write(conflict, []byte("different saved release"), 0700)
	if e = run(); e == nil {
		t.Fatal("conflicting release overwritten")
	}
	if target, _ := os.Readlink(current); target != "codexm/previous" {
		t.Fatal("conflict changed current", target)
	}
	if got, _ := os.ReadFile(conflict); string(got) != "different saved release" {
		t.Fatal("conflicting release changed")
	}
}
