// package prepares local distribution files; it never uploads or publishes them.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/codeasy-digix/codexm/internal/config"
)

var platforms = []string{"darwin-arm64", "darwin-amd64", "linux-arm64", "linux-amd64"}

func main() {
	out := flag.String("output", "dist/prepared", "new output directory (never published)")
	flag.Parse()
	if e := prepare(*out); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}

func prepare(destination string) error {
	if _, e := os.Lstat(destination); e == nil {
		return fmt.Errorf("prepared output already exists; preserved: %s", destination)
	} else if !os.IsNotExist(e) {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(destination), 0755); e != nil {
		return e
	}
	stage, e := os.MkdirTemp(filepath.Dir(destination), ".codexm-package-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	npm := filepath.Join(stage, "npm", "codexm")
	if e = os.MkdirAll(filepath.Join(npm, "scripts"), 0755); e != nil {
		return e
	}
	entries := map[string][]byte{}
	for _, name := range []string{"LICENSE", "README.md"} {
		entries[name], e = os.ReadFile(name)
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(npm, name), entries[name], 0644); e != nil {
			return e
		}
	}
	notices, e := dependencyNotices()
	if e != nil {
		return e
	}
	entries["THIRD_PARTY_NOTICES.txt"] = notices
	if e = os.WriteFile(filepath.Join(npm, "THIRD_PARTY_NOTICES.txt"), notices, 0644); e != nil {
		return e
	}
	launcher, e := os.ReadFile("packaging/npm/codexm.cjs")
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(npm, "scripts", "codexm.cjs"), launcher, 0755); e != nil {
		return e
	}
	var sums strings.Builder
	hashes := map[string]string{}
	for _, platform := range platforms {
		parts := strings.Split(platform, "-")
		bin := filepath.Join(npm, "bin", platform, "codexm")
		if e = os.MkdirAll(filepath.Dir(bin), 0755); e != nil {
			return e
		}
		cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", "-s -w -buildid=", "-o", bin, "./cmd/codexm")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+parts[0], "GOARCH="+parts[1])
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if e = cmd.Run(); e != nil {
			return e
		}
		entries["codexm"], e = os.ReadFile(bin)
		if e != nil {
			return e
		}
		archive, e := archiveBytes(entries)
		if e != nil {
			return e
		}
		name := "codexm-" + platform + ".tar.gz"
		if e = os.WriteFile(filepath.Join(stage, name), archive, 0644); e != nil {
			return e
		}
		hashes[platform] = fmt.Sprintf("%x", sha256.Sum256(archive))
		fmt.Fprintf(&sums, "%s  %s\n", hashes[platform], name)
		fmt.Println("Prepared", name)
	}
	manifest := map[string]any{
		"name": "codexm", "version": config.Version, "private": true,
		"description": "Independent terminal manager for official Codex jobs",
		"license":     "MIT", "repository": "github:codeasy-digix/codexm",
		"bin":   map[string]string{"codexm": "scripts/codexm.cjs"},
		"files": []string{"bin/", "scripts/", "LICENSE", "README.md", "THIRD_PARTY_NOTICES.txt"},
		"os":    []string{"darwin", "linux"}, "cpu": []string{"arm64", "x64"},
		"engines": map[string]string{"node": ">=18"},
	}
	b, e := json.MarshalIndent(manifest, "", "  ")
	if e != nil {
		return e
	}
	for name, contents := range map[string][]byte{
		"SHA256SUMS": []byte(sums.String()), "codexm.rb": []byte(formula(hashes)),
		"npm/codexm/package.json": append(b, '\n'),
	} {
		if e = os.WriteFile(filepath.Join(stage, name), contents, 0644); e != nil {
			return e
		}
	}
	if e = os.Rename(stage, destination); e != nil {
		return e
	}
	fmt.Println("Local preparation only:", destination)
	return nil
}

func archiveBytes(entries map[string][]byte) ([]byte, error) {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"codexm", "LICENSE", "README.md", "THIRD_PARTY_NOTICES.txt"} {
		mode := int64(0644)
		if name == "codexm" {
			mode = 0755
		}
		contents := entries[name]
		if e := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(contents))}); e != nil {
			return nil, e
		}
		if _, e := tw.Write(contents); e != nil {
			return nil, e
		}
	}
	if e := tw.Close(); e != nil {
		return nil, e
	}
	if e := gz.Close(); e != nil {
		return nil, e
	}
	return b.Bytes(), nil
}

func dependencyNotices() ([]byte, error) {
	cmd := exec.Command("go", "list", "-m", "-json", "all")
	output, e := cmd.Output()
	if e != nil {
		return nil, e
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	var notices strings.Builder
	notices.WriteString("codexm Go dependency licenses\n\n")
	goRoot, e := exec.Command("go", "env", "GOROOT").Output()
	if e != nil {
		return nil, e
	}
	goLicense, e := os.ReadFile(filepath.Join(strings.TrimSpace(string(goRoot)), "LICENSE"))
	if os.IsNotExist(e) {
		// Homebrew keeps the license outside its libexec GOROOT.
		goLicense, e = os.ReadFile("third_party/GO_LICENSE")
	}
	if os.IsNotExist(e) {
		// Homebrew puts the distribution license beside libexec (GOROOT).
		goLicense, e = os.ReadFile(filepath.Join(strings.TrimSpace(string(goRoot)), "..", "LICENSE"))
	}
	if e != nil {
		return nil, e
	}
	fmt.Fprintf(&notices, "--- Go runtime ---\n%s\n\n", goLicense)
	for {
		var module struct {
			Path, Version, Dir string
			Main               bool
		}
		if e = decoder.Decode(&module); e == io.EOF {
			break
		} else if e != nil {
			return nil, e
		}
		if module.Main || module.Dir == "" {
			continue
		}
		for _, file := range []string{"LICENSE", "LICENSE.md", "LICENSE.txt", "COPYING"} {
			license, err := os.ReadFile(filepath.Join(module.Dir, file))
			if err == nil {
				fmt.Fprintf(&notices, "--- %s %s ---\n%s\n\n", module.Path, module.Version, license)
				break
			}
		}
	}
	return []byte(notices.String()), nil
}

func formula(hashes map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `# Prepared locally. Apply to a tap only after publishing the verified release.
class Codexm < Formula
  desc "Terminal manager for official Codex jobs, accounts and logs"
  homepage "https://github.com/codeasy-digix/codexm"
  version "%s"
  license "MIT"
  depends_on "tmux"

`, config.Version)
	for _, osName := range []string{"macos", "linux"} {
		goos := osName
		if osName == "macos" {
			goos = "darwin"
		}
		fmt.Fprintf(&b, "  on_%s do\n", osName)
		for _, arch := range []string{"arm64", "amd64"} {
			brewarch := "arm"
			if arch == "amd64" {
				brewarch = "intel"
			}
			platform := goos + "-" + arch
			fmt.Fprintf(&b, "    on_%s do\n      url \"https://github.com/codeasy-digix/codexm/releases/download/v%s/codexm-%s.tar.gz\"\n      sha256 \"%s\"\n    end\n", brewarch, config.Version, platform, hashes[platform])
		}
		b.WriteString("  end\n\n")
	}
	b.WriteString(`  def install
    bin.install "codexm"
    (pkgshare/"licenses").install "THIRD_PARTY_NOTICES.txt"
  end

  def caveats
    <<~EOS
      Install the official Codex CLI separately; its codex command stays unchanged.
      Select a worker: codexm account NAME
      Select a manager: codexm setup --manager NAME
      Start the manager: codexm
    EOS
  end

  test do
    assert_match "codexm #{version}", shell_output("#{bin}/codexm --version")
    assert_match "codexm account", shell_output("#{bin}/codexm account --help")
  end
end
`)
	return b.String()
}
