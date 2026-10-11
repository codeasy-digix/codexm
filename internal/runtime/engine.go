package runtime

// Engine receipts pin both the CLI and its code-mode host. Account changes and
// upstream installers must never silently change the executable of a job.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	"golang.org/x/sys/unix"
)

type Engine struct {
	Binary          string            `json:"binary"`
	Version         string            `json:"version"`
	Source          string            `json:"source"`
	Files           map[string]string `json:"sha256"`
	ProtocolChecked bool              `json:"protocol_checked"`
	CreatedAt       time.Time         `json:"created_at"`
}

func enginePointer(c config.Config) string { return filepath.Join(c.StateDir(), "engine.json") }
func digest(p string) (string, error) {
	f, e := os.Open(p)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func ReadEngine(c config.Config) (Engine, error) {
	var r Engine
	b, e := os.ReadFile(enginePointer(c))
	if e != nil {
		return r, e
	}
	if e = json.Unmarshal(b, &r); e != nil {
		return r, e
	}
	return r, VerifyEngine(r)
}
func VerifyEngine(r Engine) error {
	if !r.ProtocolChecked || !filepath.IsAbs(r.Binary) || len(r.Files) == 0 {
		return errors.New("invalid engine receipt; run codexm runtime refresh")
	}
	for name, want := range r.Files {
		if filepath.Base(name) != name {
			return errors.New("invalid engine file name")
		}
		p := filepath.Join(filepath.Dir(r.Binary), name)
		info, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&022 != 0 {
			return fmt.Errorf("unsafe engine file: %s", p)
		}
		got, e := digest(p)
		if e != nil {
			return e
		}
		if got != want {
			return fmt.Errorf("pinned engine changed: %s; restore it or explicitly refresh", name)
		}
	}
	return nil
}
func PinEngine(ctx context.Context, c config.Config, source string) (Engine, error) {
	var r Engine
	source, e := filepath.EvalSymlinks(source)
	if e != nil {
		return r, e
	}
	root := config.Expand(c.Runtime.EnginesDir)
	if e = os.MkdirAll(root, 0700); e != nil {
		return r, e
	}
	lock, e := os.OpenFile(filepath.Join(root, "install.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return r, e
	}
	defer lock.Close()
	if e = unix.Flock(int(lock.Fd()), unix.LOCK_EX); e != nil {
		return r, e
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	b, e := exec.CommandContext(ctx, source, "--version").Output()
	if e != nil {
		return r, fmt.Errorf("engine version: %w", e)
	}
	r = Engine{Source: source, Version: strings.TrimSpace(string(b)), Files: map[string]string{}, CreatedAt: time.Now()}
	inputs := map[string]string{"codex": source}
	for _, name := range []string{"codex-code-mode-host", "codex-exec-server"} {
		p := filepath.Join(filepath.Dir(source), name)
		if info, e := os.Stat(p); e == nil && info.Mode().IsRegular() {
			inputs[name] = p
		}
	}
	for name, p := range inputs {
		h, e := digest(p)
		if e != nil {
			return r, e
		}
		r.Files[name] = h
	}
	keyData, _ := json.Marshal(r.Files)
	key := sha256.Sum256(append([]byte(r.Version), keyData...))
	dest := filepath.Join(root, hex.EncodeToString(key[:16]))
	r.Binary = filepath.Join(dest, "codex")
	if old, e := os.ReadFile(filepath.Join(dest, "receipt.json")); e == nil {
		var cached Engine
		if json.Unmarshal(old, &cached) != nil {
			return r, errors.New("invalid cached engine receipt")
		}
		if e = VerifyEngine(cached); e != nil {
			return r, e
		}
		r = cached
	} else {
		stage, e := os.MkdirTemp(root, ".install-")
		if e != nil {
			return r, e
		}
		defer os.RemoveAll(stage)
		for name, p := range inputs {
			if e = copyExecutable(p, filepath.Join(stage, name)); e != nil {
				return r, e
			}
			h, e := digest(filepath.Join(stage, name))
			if e != nil {
				return r, e
			}
			after, e := digest(p)
			if e != nil {
				return r, e
			}
			if h != r.Files[name] || after != h {
				return r, errors.New("upstream engine changed while copying; retry refresh")
			}
		}
		if e = CheckProtocol(ctx, filepath.Join(stage, "codex")); e != nil {
			return r, e
		}
		r.ProtocolChecked = true
		b, _ := json.MarshalIndent(r, "", "  ")
		if e = config.AtomicWrite(filepath.Join(stage, "receipt.json"), b, 0600); e != nil {
			return r, e
		}
		if e = os.Rename(stage, dest); e != nil {
			return r, e
		}
	}
	b, _ = json.MarshalIndent(r, "", "  ")
	return r, config.AtomicWrite(enginePointer(c), b, 0600)
}
func copyExecutable(src, dst string) error {
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0500)
	if e != nil {
		return e
	}
	_, e = io.Copy(out, in)
	if e == nil {
		e = out.Sync()
	}
	ce := out.Close()
	if e == nil {
		e = ce
	}
	return e
}
func CheckProtocol(ctx context.Context, binary string) error {
	dir, e := os.MkdirTemp("", "codexm-protocol-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, binary, "app-server", "generate-json-schema", "--experimental", "--out", dir)
	if b, e := cmd.CombinedOutput(); e != nil {
		return fmt.Errorf("engine protocol unsupported: %s: %w", SafeText(string(b)), e)
	}
	b, e := os.ReadFile(filepath.Join(dir, "ClientRequest.json"))
	if e != nil {
		return e
	}
	var d struct {
		OneOf []struct {
			Properties struct {
				Method struct {
					Enum []string `json:"enum"`
				} `json:"method"`
			} `json:"properties"`
		} `json:"oneOf"`
	}
	if e = json.Unmarshal(b, &d); e != nil {
		return e
	}
	methods := map[string]bool{}
	for _, x := range d.OneOf {
		for _, m := range x.Properties.Method.Enum {
			methods[m] = true
		}
	}
	for _, m := range []string{"initialize", "thread/start", "thread/name/set", "thread/resume", "thread/read", "thread/list", "thread/turns/list", "thread/loaded/list", "turn/start", "turn/steer", "turn/interrupt", "thread/goal/get", "thread/goal/set", "account/read", "account/rateLimits/read"} {
		if !methods[m] {
			return fmt.Errorf("engine lacks required protocol method %s; previous pin is retained", m)
		}
	}
	return nil
}

// Probe a foreign writer without taking ownership. Codex performs the final
// lock acquisition itself, so a race after this check is still rejected there.
func WriterBusy(home, thread string) bool {
	if thread == "" || filepath.Base(thread) != thread {
		return thread != ""
	}
	p := filepath.Join(home, "thread-writer-locks", thread+".lock")
	fd, e := unix.Open(p, unix.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if errors.Is(e, unix.ENOENT) {
		return false
	}
	if e != nil {
		return true
	}
	defer unix.Close(fd)
	if e = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); e != nil {
		return true
	}
	_ = unix.Flock(fd, unix.LOCK_UN)
	return false
}

// Killing a tmux session can return before its Codex process has exited. Wait
// for the existing lock to become free without deleting it or killing a writer.
func WaitWriterRelease(ctx context.Context, home, thread string) error {
	for WriterBusy(home, thread) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return nil
}
