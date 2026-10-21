// Package shellinit provides only the parent-shell adapter for native accounts.
// It never intercepts the official codex executable or evaluates command output.
package shellinit

import (
	_ "embed"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

//go:embed codexm.zsh
var Zsh string

const ResultEnv = "CODEXM_SHELL_RESULT"

// OpenResult accepts a new file inside the shell's private scratch directory.
// Exclusive creation rejects symlinks and existing files without truncating them.
func OpenResult() (*os.File, error) {
	p := os.Getenv(ResultEnv)
	if p == "" {
		return nil, errors.New("current-terminal account selection requires zsh integration; run: source <(command codexm shell-init zsh), then retry codexm account NAME")
	}
	if !filepath.IsAbs(p) {
		return nil, errors.New("shell account result requires an absolute private path")
	}
	info, err := os.Lstat(filepath.Dir(p))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("shell account result requires a private real directory")
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || st.Uid != uint32(os.Getuid()) {
		return nil, errors.New("shell account result directory must belong to the current user")
	}
	return os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
}

// Select writes data, never shell source. Publish only after successful login
// and native policy validation. The parent checks the complete frame and status.
func Select(w io.Writer, name, home, shared string) error {
	_, err := io.WriteString(w, strings.Join([]string{"codexm-native-account-v1", name, home, shared, ""}, "\x00"))
	return err
}
