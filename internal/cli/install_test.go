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
	script := filepath.Join(repo, "install.sh")
	write(script, source, 0700)
	write(filepath.Join(repo, "VERSION"), []byte("0.1.0\n"), 0600)
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
	checksum := filepath.Join(repo, "SHA256SUMS")
	write(checksum, []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(valid), asset)), 0600)
	bin := filepath.Join(repo, "test-bin")
	write(filepath.Join(bin, "codex"), []byte("#!/bin/sh\necho 'codex-cli 0.0-test'\n"), 0700)
	write(filepath.Join(bin, "gh"), []byte(`#!/bin/sh
set -eu
download_dest=
while [ "$#" -gt 0 ]; do
  if [ "$1" = --dir ]; then shift; download_dest=$1; fi
  shift
done
cp "$CODEXM_TEST_ARCHIVE" "$download_dest/$CODEXM_TEST_ASSET"
cp "$CODEXM_TEST_CHECKSUM" "$download_dest/SHA256SUMS"
`), 0700)
	installRoot := filepath.Join(repo, "data", "codexm")
	old := filepath.Join(installRoot, "versions", "previous")
	write(filepath.Join(old, "codexm"), []byte("#!/bin/sh\necho 'codexm 0.0.0'\n"), 0700)
	current := filepath.Join(installRoot, "bin", "codexm")
	os.MkdirAll(filepath.Dir(current), 0700)
	if e = os.Symlink(filepath.Join(old, "codexm"), current); e != nil {
		t.Fatal(e)
	}
	run := func() error {
		cmd := exec.Command("sh", script)
		cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "CODEXM_INSTALL_ROOT="+installRoot, "CODEXM_LINK_DIR="+filepath.Join(repo, "entrypoint"), "CODEXM_TEST_ARCHIVE="+archive, "CODEXM_TEST_ASSET="+asset, "CODEXM_TEST_CHECKSUM="+checksum)
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
	if target, _ := os.Readlink(current); target != filepath.Join(old, "codexm") {
		t.Fatal("failed download replaced current", target)
	}
	write(archive, valid, 0600)
	if e = run(); e != nil {
		t.Fatal(e)
	}
	if target, _ := os.Readlink(current); target != filepath.Join(installRoot, "versions", "v0.1.0", platform, "codexm") {
		t.Fatal("validated release not installed", target)
	}
	if e = run(); e != nil {
		t.Fatal("idempotent reinstall failed", e)
	}
	// Keep both the selected old install and a conflicting release untouched.
	os.Remove(current)
	os.Symlink(filepath.Join(old, "codexm"), current)
	conflict := filepath.Join(installRoot, "versions", "v0.1.0", platform, "codexm")
	write(conflict, []byte("different saved release"), 0700)
	if e = run(); e == nil {
		t.Fatal("conflicting release overwritten")
	}
	if target, _ := os.Readlink(current); target != filepath.Join(old, "codexm") {
		t.Fatal("conflict changed current", target)
	}
	if got, _ := os.ReadFile(conflict); string(got) != "different saved release" {
		t.Fatal("conflicting release changed")
	}
	// Local changes must not overwrite a release or a previous development build.
	write(filepath.Join(repo, "build.sh"), []byte("#!/bin/sh\ncp \"$CODEXM_TEST_LOCAL_BINARY\" \"$3\"\n"), 0700)
	local := filepath.Join(repo, "local-binary")
	write(local, binary, 0700)
	runSource := func(extra ...string) error {
		cmd := exec.Command("sh", append([]string{script, "--build"}, extra...)...)
		cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "CODEXM_INSTALL_ROOT="+installRoot, "CODEXM_LINK_DIR="+filepath.Join(repo, "entrypoint"), "CODEXM_TEST_LOCAL_BINARY="+local)
		if out, e := cmd.CombinedOutput(); e != nil {
			return fmt.Errorf("%w: %s", e, out)
		}
		return nil
	}
	if e = runSource(); e != nil {
		t.Fatal(e)
	}
	firstSource, _ := os.Readlink(current)
	write(local, append(binary, []byte("# changed local build\n")...), 0700)
	if e = runSource(); e != nil {
		t.Fatal("local development update failed", e)
	}
	secondSource, _ := os.Readlink(current)
	if firstSource == secondSource {
		t.Fatal("changed source replaced immutable files")
	}
	if b, _ := os.ReadFile(firstSource); string(b) != string(binary) {
		t.Fatal("previous source overwritten")
	}
	if b, _ := os.ReadFile(conflict); string(b) != "different saved release" {
		t.Fatal("local build overwrote release")
	}
	write(local, []byte("#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'codexm 0.1.0'; else exit 7; fi\n"), 0700)
	if e = runSource(); e == nil {
		t.Fatal("failed migration switched installation")
	}
	if target, _ := os.Readlink(current); target != secondSource {
		t.Fatal("failed child changed current", target)
	}
	write(local, binary, 0700)
	entry := filepath.Join(repo, "entrypoint", "codexm")
	os.Remove(entry)
	write(entry, []byte("user owned entrypoint"), 0700)
	if e = runSource(); e == nil {
		t.Fatal("non-symlink collision overwritten")
	}
	if b, _ := os.ReadFile(entry); string(b) != "user owned entrypoint" {
		t.Fatal("user file changed")
	}
	if target, _ := os.Readlink(current); target != secondSource {
		t.Fatal("collision changed current", target)
	}
	os.Remove(entry)
	os.Symlink(current, entry)
	realMV, err := exec.LookPath("mv")
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(bin, "mv"), []byte("#!/bin/sh\ncase \"$*\" in *entrypoint/.codexm-*) exit 7 ;; esac\nexec "+realMV+" \"$@\"\n"), 0700)
	if e = runSource(); e == nil {
		t.Fatal("failed public link replacement reported success")
	}
	if target, _ := os.Readlink(current); target != secondSource {
		t.Fatal("failed link replacement did not roll back", target)
	}
	if target, _ := os.Readlink(entry); target != current {
		t.Fatal("failed link replacement changed public entry", target)
	}
	// A removed option cannot recreate a legacy relay entrypoint.
	if err := os.Remove(filepath.Join(bin, "mv")); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Readlink(current)
	if err := runSource("--history-sync-compat"); err == nil {
		t.Fatal("retired install option accepted")
	}
	if target, _ := os.Readlink(current); target != before {
		t.Fatal("rejected option changed installed product")
	}
	if _, err := os.Lstat(filepath.Join(repo, "entrypoint", "codex-history-sync")); !os.IsNotExist(err) {
		t.Fatal("legacy entrypoint recreated")
	}
}
