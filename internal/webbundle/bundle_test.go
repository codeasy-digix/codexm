package webbundle

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyPreservesLinkModesWithPrivateUmask(t *testing.T) {
	previous := unix.Umask(0022)
	defer unix.Umask(previous)
	src := fixture(t)
	if err := os.Symlink("main.js", filepath.Join(src, "entry.js")); err != nil {
		t.Fatal(err)
	}
	if _, err := Seal(src); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(src, alias); err != nil {
		t.Fatal(err)
	}
	unix.Umask(0077)
	destination := filepath.Join(t.TempDir(), "bundle")
	if _, err := Copy(alias, destination); err != nil {
		t.Fatal("private installation changed the sealed artifact", err)
	}
	if _, err := Verify(destination, true); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "programs/server/node_modules"), 0755); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{"main.js": "console.log('fixture')", "star.json": "{}", "programs/server/boot.js": "// boot", "programs/server/program.json": "{}"} {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCopyValidatesArtifactAndPreservesCollisions(t *testing.T) {
	src := fixture(t)
	m, err := Seal(src)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "new")
	if got, err := Copy(src, dest); err != nil || got.Digest != m.Digest {
		t.Fatal(got, err)
	}
	if _, err := Copy(src, dest); err == nil {
		t.Fatal("overwrote existing artifact")
	}
	if _, err := Verify(dest, true); err != nil {
		t.Fatal("collision modified artifact", err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.js"), []byte("modified"), 0644); err != nil {
		t.Fatal(err)
	}
	absent := filepath.Join(t.TempDir(), "absent")
	if _, err := Copy(src, absent); err == nil {
		t.Fatal("accepted modified artifact")
	}
	if _, err := os.Stat(absent); !os.IsNotExist(err) {
		t.Fatal("failed validation published files", err)
	}
	if _, err := Verify(dest, true); err != nil {
		t.Fatal("failed copy damaged previous artifact", err)
	}
}

func TestBundleRejectsExternalLinksAndInvalidMetadata(t *testing.T) {
	src := fixture(t)
	if err := os.Symlink("/etc/passwd", filepath.Join(src, "outside")); err != nil {
		t.Fatal(err)
	}
	if _, err := Seal(src); err == nil {
		t.Fatal("external link accepted")
	}
	os.Remove(filepath.Join(src, "outside"))
	os.WriteFile(filepath.Join(src, "star.json"), []byte("invalid"), 0644)
	if _, err := Seal(src); err == nil {
		t.Fatal("invalid Meteor manifest accepted")
	}
}
