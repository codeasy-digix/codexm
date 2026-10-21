// Package webbundle validates and stages immutable local Meteor artifacts.
package webbundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/codeasy-digix/codexm/internal/config"
)

const manifestName = "codexm-bundle.json"

type Manifest struct {
	Version   string `json:"version"`
	Protocol  int    `json:"protocolVersion"`
	Platform  string `json:"platform"`
	NodeMajor int    `json:"nodeMajor"`
	Digest    string `json:"digest"`
}

func inventory(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." || rel == manifestName {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), info.Mode())
		switch {
		case d.IsDir():
		case info.Mode().IsRegular():
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(h, f)
			f.Close()
			if err != nil {
				return err
			}
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			inside, err := filepath.Rel(root, resolved)
			if err != nil || filepath.IsAbs(target) || inside == ".." || strings.HasPrefix(inside, ".."+string(os.PathSeparator)) {
				return fmt.Errorf("bundle link escapes its artifact: %s", rel)
			}
			fmt.Fprint(h, target)
		default:
			return fmt.Errorf("unsupported artifact file: %s", rel)
		}
		h.Write([]byte{0})
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)), err
}

func structure(root string) error {
	for _, path := range []string{"main.js", "star.json", "programs/server/boot.js", "programs/server/program.json", "programs/server/node_modules"} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			return fmt.Errorf("incomplete Meteor bundle: %w", err)
		}
	}
	for _, path := range []string{"star.json", "programs/server/program.json"} {
		b, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			return err
		}
		if !json.Valid(b) {
			return fmt.Errorf("invalid Meteor artifact %s", path)
		}
	}
	return nil
}

func Seal(root string) (Manifest, error) {
	m := Manifest{Version: config.Version, Protocol: 1, Platform: runtime.GOOS + "-" + runtime.GOARCH, NodeMajor: 24}
	root, err := filepath.Abs(root)
	if err != nil {
		return m, err
	}
	if err = structure(root); err != nil {
		return m, err
	}
	m.Digest, err = inventory(root)
	if err != nil {
		return m, err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return m, err
	}
	return m, config.AtomicWrite(filepath.Join(root, manifestName), b, 0644)
}

func Verify(root string, full bool) (Manifest, error) {
	var m Manifest
	root, err := filepath.Abs(root)
	if err != nil {
		return m, err
	}
	b, err := os.ReadFile(filepath.Join(root, manifestName))
	if err != nil {
		return m, fmt.Errorf("build the web bundle first: %w", err)
	}
	if err = json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	if m.Version != config.Version || m.Protocol != 1 || m.Platform != runtime.GOOS+"-"+runtime.GOARCH || m.NodeMajor != 24 || len(m.Digest) != 64 {
		return m, errors.New("web bundle version, platform or bridge contract differs; rebuild it with this codexm source")
	}
	if err = structure(root); err != nil {
		return m, err
	}
	if full {
		actual, err := inventory(root)
		if err != nil {
			return m, err
		}
		if actual != m.Digest {
			return m, errors.New("web bundle checksum mismatch; existing files were preserved")
		}
	}
	return m, nil
}

// Copy only writes a new destination. Failed validation/copy removes only the
// directory this invocation created; an existing path is never overwritten.
func Copy(source, destination string) (Manifest, error) {
	m, err := Verify(source, true)
	if err != nil {
		return m, err
	}
	if err = os.Mkdir(destination, 0755); err != nil {
		return m, err
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(destination)
		}
	}()
	err = filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil || rel == "." {
			return err
		}
		target := filepath.Join(destination, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			if err := os.Mkdir(target, info.Mode().Perm()); err != nil {
				return err
			}
			return os.Chmod(target, info.Mode().Perm())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		if !info.Mode().IsRegular() {
			return errors.New("artifact changed during copy")
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		modeErr := out.Chmod(info.Mode().Perm())
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if modeErr != nil {
			return modeErr
		}
		return closeErr
	})
	if err != nil {
		return m, err
	}
	if _, err = Verify(destination, true); err != nil {
		return m, err
	}
	complete = true
	return m, nil
}
