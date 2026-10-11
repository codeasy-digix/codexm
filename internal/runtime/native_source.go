package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
)

// Official npm installs expose a JS launcher on PATH. Pin the actual native
// package instead so its immutable copy does not depend on Node or that launcher.
func officialEngineSource(source string) (string, error) {
	resolved, e := filepath.EvalSymlinks(source)
	if e != nil {
		return "", e
	}
	resolved, e = filepath.Abs(resolved)
	if e != nil {
		return "", e
	}
	root := filepath.Dir(filepath.Dir(resolved))
	b, e := os.ReadFile(filepath.Join(root, "package.json"))
	var pkg struct {
		Name string `json:"name"`
	}
	if e != nil || json.Unmarshal(b, &pkg) != nil || pkg.Name != "@openai/codex" {
		return resolved, nil
	}
	triples := map[string]string{
		"darwin/arm64": "aarch64-apple-darwin", "darwin/amd64": "x86_64-apple-darwin",
		"linux/arm64": "aarch64-unknown-linux-musl", "linux/amd64": "x86_64-unknown-linux-musl",
	}
	triple := triples[goruntime.GOOS+"/"+goruntime.GOARCH]
	if triple == "" {
		return "", fmt.Errorf("unsupported official npm Codex platform")
	}
	arch := goruntime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	platformPackage := "codex-" + goruntime.GOOS + "-" + arch
	vendors := []string{filepath.Join(root, "vendor")}
	// Cover nested and hoisted optional dependencies, including pnpm symlinks.
	for dir := root; ; dir = filepath.Dir(dir) {
		vendors = append(vendors, filepath.Join(dir, "node_modules", "@openai", platformPackage, "vendor"))
		if filepath.Dir(dir) == dir {
			break
		}
	}
	for _, vendor := range vendors {
		for _, binDir := range []string{"bin", "codex"} {
			candidate := filepath.Join(vendor, triple, binDir, "codex")
			if info, e := os.Stat(candidate); e == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
				return filepath.EvalSymlinks(candidate)
			}
		}
	}
	return "", fmt.Errorf("official npm Codex native package is missing; repair the official installation or set runtime.codex_binary")
}
