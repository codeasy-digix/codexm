package accounts

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/pelletier/go-toml/v2"
)

func lockNativeAccountConfig(home string) (*os.File, error) {
	fd, err := syscall.Open(filepath.Join(home, ".account-default.lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, errors.New("cannot lock native account configuration")
	}
	lock := os.NewFile(uintptr(fd), "native account configuration lock")
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, errors.New("another terminal is changing the native default or storage configuration")
	}
	return lock, nil
}

func verifyNativeSQLiteHome(shared string, configured *string) (bool, error) {
	if configured == nil {
		return false, nil
	}
	if !filepath.IsAbs(*configured) {
		return false, errors.New("native sqlite_home must be an absolute path for shared Codex storage")
	}
	expected, err := filepath.EvalSymlinks(shared)
	if err != nil {
		return false, err
	}
	actual, err := filepath.EvalSymlinks(*configured)
	if err != nil || actual != expected {
		return false, fmt.Errorf("native sqlite_home differs from the shared Codex home %s", shared)
	}
	return true, nil
}

func configuredNativeSQLiteHome(shared string, data []byte) (bool, error) {
	native, err := parseNativeConfig(data)
	if err != nil {
		return false, err
	}
	return verifyNativeSQLiteHome(shared, native.SQLiteHome)
}

// NativeStoragePinned reads only the native user config.
func NativeStoragePinned(c config.Config) (bool, error) {
	shared := config.Expand(c.Runtime.SharedCodexHome)
	data, err := nativePrivateRead(filepath.Join(shared, "config.toml"), true)
	if err != nil {
		return false, err
	}
	return configuredNativeSQLiteHome(shared, data)
}

// PinNativeStorage is an explicit one-time local configuration change. Named
// homes share this config even if a native app does not inherit the shell's
// CODEX_SQLITE_HOME. No native database or account file is opened for writing.
func PinNativeStorage(c config.Config) (string, error) {
	shared := config.Expand(c.Runtime.SharedCodexHome)
	info, err := os.Lstat(shared)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("shared Codex home must be an existing directory")
	}
	shared, err = filepath.EvalSymlinks(shared)
	if err != nil {
		return "", err
	}
	lock, err := lockNativeAccountConfig(shared)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	filename := filepath.Join(shared, "config.toml")
	before, err := nativePrivateRead(filename, true)
	if err != nil {
		return "", err
	}
	if pinned, err := configuredNativeSQLiteHome(shared, before); err != nil || pinned {
		return "", err
	}
	header, err := toml.Marshal(map[string]string{"sqlite_home": shared})
	if err != nil {
		return "", err
	}
	// Prefix one top-level key without reformatting user settings or comments.
	bom := []byte{0xef, 0xbb, 0xbf}
	body, hasBOM := bytes.CutPrefix(before, bom)
	var after []byte
	if hasBOM {
		after = append(after, bom...)
	}
	after = append(after, header...)
	after = append(after, body...)
	if pinned, err := configuredNativeSQLiteHome(shared, after); err != nil || !pinned {
		return "", errors.New("shared SQLite configuration could not be validated")
	}
	backups := filepath.Join(c.StateDir(), "migrations")
	if err := os.MkdirAll(backups, 0700); err != nil {
		return "", err
	}
	backupDir, err := os.MkdirTemp(backups, "native-sqlite-home-")
	if err != nil {
		return "", err
	}
	backup := filepath.Join(backupDir, "config.toml")
	if err := config.AtomicWrite(backup, before, 0600); err != nil {
		return "", err
	}
	current, err := nativePrivateRead(filename, true)
	if err != nil || !bytes.Equal(current, before) {
		return "", errors.New("native configuration changed during the storage setup; retry")
	}
	if err := config.AtomicWrite(filename, after, 0600); err != nil {
		return "", err
	}
	return backup, nil
}
