package runtime

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/codeasy-digix/codexm/internal/config"
)

func TestOfficialNpmLauncherResolvesNativeBeforePinning(t *testing.T) {
	triples := map[string]string{"darwin/arm64": "aarch64-apple-darwin", "darwin/amd64": "x86_64-apple-darwin", "linux/arm64": "aarch64-unknown-linux-musl", "linux/amd64": "x86_64-unknown-linux-musl"}
	triple := triples[goruntime.GOOS+"/"+goruntime.GOARCH]
	if triple == "" {
		t.Skip("Unix native package")
	}
	arch := goruntime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	for _, layout := range []string{"nested", "hoisted", "vendored"} {
		t.Run(layout, func(t *testing.T) {
			root := t.TempDir()
			packageRoot := filepath.Join(root, "lib", "node_modules", "@openai", "codex")
			os.MkdirAll(filepath.Join(packageRoot, "bin"), 0700)
			os.WriteFile(filepath.Join(packageRoot, "package.json"), []byte(`{"name":"@openai/codex"}`), 0600)
			launcher := filepath.Join(packageRoot, "bin", "codex.js")
			os.WriteFile(launcher, []byte("#!/usr/bin/env node\nthrow new Error('launcher must not be pinned');\n"), 0700)
			vendor := filepath.Join(packageRoot, "vendor")
			if layout == "nested" {
				vendor = filepath.Join(packageRoot, "node_modules", "@openai", "codex-"+goruntime.GOOS+"-"+arch, "vendor")
			}
			if layout == "hoisted" {
				vendor = filepath.Join(root, "lib", "node_modules", "@openai", "codex-"+goruntime.GOOS+"-"+arch, "vendor")
			}
			native := fakeEngine(t, filepath.Join(vendor, triple, "bin"), "0.162.1", true)
			canonical, e := filepath.EvalSymlinks(native)
			if e != nil {
				t.Fatal(e)
			}
			c := config.Defaults()
			c.Runtime.CodexBinary = launcher
			got, e := Locate(c)
			if e != nil || got.Binary != canonical {
				t.Fatal("npm launcher was retained", got.Binary, e)
			}
			os.Remove(native)
			if _, e := Locate(c); e == nil {
				t.Fatal("missing optional native package silently accepted")
			}
		})
	}
}
