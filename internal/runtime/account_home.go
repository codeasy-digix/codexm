package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/codeasy-digix/codexm/internal/config"
)

// Named homes keep credentials separate and share the existing local history.
// Existing files and the default login are never replaced.
func EnsureAccountHome(c config.Config, name string) error {
	if !config.Nickname.MatchString(name) || name == "default" {
		return fmt.Errorf("invalid account nickname")
	}
	root := config.Expand(c.Runtime.AccountsDir)
	home := filepath.Join(root, name)
	shared := config.Expand(c.Runtime.SharedCodexHome)
	contains := func(parent, child string) bool {
		rel, e := filepath.Rel(parent, child)
		return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
	}
	if contains(shared, root) || contains(root, shared) {
		return fmt.Errorf("credentials and shared conversations require separate directories")
	}
	for _, p := range []string{shared, root, home} {
		if info, e := os.Lstat(p); e == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("account home must be a real directory: %s", p)
		} else if e != nil && !os.IsNotExist(e) {
			return e
		}
		if e := os.MkdirAll(p, 0700); e != nil {
			return e
		}
		if p != shared {
			if e := os.Chmod(p, 0700); e != nil {
				return e
			}
		}
	}
	resolvedRoot, e := filepath.EvalSymlinks(root)
	if e != nil {
		return e
	}
	resolvedShared, e := filepath.EvalSymlinks(shared)
	if e != nil {
		return e
	}
	if contains(resolvedShared, resolvedRoot) || contains(resolvedRoot, resolvedShared) {
		return fmt.Errorf("credentials resolve inside or around shared conversations")
	}
	auth := filepath.Join(home, "auth.json")
	if info, e := os.Lstat(auth); e == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("named account auth must be a regular file")
		}
		if e = os.Chmod(auth, 0600); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	directories := []string{"sessions", "archived_sessions", "memories", "attachments", "skills", "rules", "thread-writer-locks", "project-metadata-locks", "rollout-migrations"}
	names := append(append([]string{}, directories...), "history.jsonl", "session_index.jsonl", "config.toml", "AGENTS.md")
	// Silently accepting private history/lock directories would split the store.
	// Check all destinations before linking; incompatible files are preserved.
	for _, n := range names {
		target := filepath.Join(home, n)
		info, e := os.Lstat(target)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("existing account data was preserved; shared link required: %s", target)
		}
		link, e := os.Readlink(target)
		if e != nil {
			return e
		}
		if !filepath.IsAbs(link) {
			link = filepath.Join(home, link)
		}
		if filepath.Clean(link) != filepath.Join(shared, n) && filepath.Clean(link) != filepath.Join(resolvedShared, n) {
			return fmt.Errorf("existing account link points elsewhere and was preserved: %s", target)
		}
	}
	// A first named login must share the storage and writer locks even when the
	// default home has never launched a thread yet. Otherwise Codex creates
	// private directories in each account and cross-account resume cannot work.
	for _, name := range directories {
		if e := os.MkdirAll(filepath.Join(shared, name), 0700); e != nil {
			return e
		}
	}
	for _, name := range []string{"history.jsonl", "session_index.jsonl", "config.toml"} {
		f, e := os.OpenFile(filepath.Join(shared, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil && !os.IsExist(e) {
			return e
		}
		if f != nil {
			if e = f.Close(); e != nil {
				return e
			}
		}
	}
	for _, name := range names {
		source, target := filepath.Join(shared, name), filepath.Join(home, name)
		if _, e := os.Lstat(target); e == nil {
			continue
		} else if !os.IsNotExist(e) {
			return e
		}
		if e := os.Symlink(source, target); e != nil && !os.IsExist(e) {
			return e
		}
	}
	return nil
}

// Interactive sign-in must not replace the same account concurrently.
func AccountLoginLock(c config.Config, name string) (*os.File, error) {
	if e := EnsureAccountHome(c, name); e != nil {
		return nil, e
	}
	p := filepath.Join(config.Expand(c.Runtime.AccountsDir), name, ".account-login.lock")
	fd, e := syscall.Open(p, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), "account login lock")
	if e := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, fmt.Errorf("another codexm account command is using this login; try again when it finishes")
	}
	return f, nil
}
