package accounts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/pelletier/go-toml/v2"
)

// SetNativeDefault publishes a named file login for subsequent native launches.
// It neither contacts a daemon nor changes worker policy, the shell or a GUI.
// Callers serialize interactive login with runtime.AccountLoginLock first.
func SetNativeDefault(c config.Config, name string) (string, error) {
	if !config.Nickname.MatchString(name) || name == "default" {
		return "", errors.New("invalid native default account")
	}
	shared, root := config.Expand(c.Runtime.SharedCodexHome), config.Expand(c.Runtime.AccountsDir)
	for _, dir := range []string{shared, root, filepath.Join(root, name)} {
		info, e := os.Lstat(dir)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("native and named account homes must be real directories")
		}
	}
	resolvedShared, e := filepath.EvalSymlinks(shared)
	if e != nil {
		return "", e
	}
	resolvedRoot, e := filepath.EvalSymlinks(root)
	if e != nil {
		return "", e
	}
	inside := func(parent, child string) bool {
		rel, err := filepath.Rel(parent, child)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
	}
	if inside(resolvedRoot, resolvedShared) || inside(resolvedShared, resolvedRoot) {
		return "", errors.New("native and named credentials must use separate homes")
	}
	fd, e := syscall.Open(filepath.Join(shared, ".account-default.lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if e != nil {
		return "", errors.New("cannot lock native default selection")
	}
	lock := os.NewFile(uintptr(fd), "native default lock")
	defer lock.Close()
	if e = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return "", errors.New("another terminal is changing the native default; try again when it finishes")
	}
	conf := filepath.Join(shared, "config.toml")
	beforeConfig, e := nativePrivateRead(conf, true)
	if e != nil {
		return "", e
	}
	var native struct {
		Store       string `toml:"cli_auth_credentials_store"`
		ForcedLogin string `toml:"forced_login_method"`
		Workspace   string `toml:"forced_chatgpt_workspace_id"`
	}
	if toml.Unmarshal(beforeConfig, &native) != nil {
		return "", errors.New("native configuration could not be read safely")
	}
	if native.Store != "" && native.Store != "file" {
		return "", errors.New("native default switching requires cli_auth_credentials_store = \"file\"; the native login was preserved")
	}
	if native.ForcedLogin != "" && native.ForcedLogin != "chatgpt" {
		return "", errors.New("native login policy does not allow ChatGPT authentication; native login was preserved")
	}
	source := filepath.Join(root, name, "auth.json")
	chosen, e := nativePrivateRead(source, false)
	if e != nil {
		return "", e
	}
	var auth struct {
		Mode   string  `json:"auth_mode"`
		Key    *string `json:"OPENAI_API_KEY"`
		Tokens struct {
			ID      string `json:"id_token"`
			Access  string `json:"access_token"`
			Refresh string `json:"refresh_token"`
			Account string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal(chosen, &auth) != nil || auth.Key != nil && *auth.Key != "" || auth.Mode != "" && auth.Mode != "chatgpt" || auth.Tokens.ID == "" || auth.Tokens.Access == "" || auth.Tokens.Refresh == "" || auth.Tokens.Account == "" {
		return "", errors.New("a saved ChatGPT file login is required; native login was preserved")
	}
	if native.Workspace != "" && native.Workspace != auth.Tokens.Account {
		return "", errors.New("selected login does not match native workspace policy; native login was preserved")
	}
	dest := filepath.Join(shared, "auth.json")
	previousName, e := managedNativeLink(dest, root)
	if e != nil {
		return "", e
	}
	if previousName == name {
		return "", nil
	}
	previousPath := dest
	if previousName != "" {
		previousPath = filepath.Join(root, previousName, "auth.json")
	}
	previous, e := nativePrivateRead(previousPath, true)
	if e != nil {
		return "", e
	}
	backups := filepath.Join(root, ".default-backups")
	if e = os.Mkdir(backups, 0700); e != nil && !os.IsExist(e) {
		return "", e
	}
	if info, err := os.Lstat(backups); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("native login backup directory must be a real directory")
	}
	if e = os.Chmod(backups, 0700); e != nil {
		return "", e
	}
	backup, e := os.MkdirTemp(backups, time.Now().Format("20060102-150405")+"-")
	if e != nil {
		return "", e
	}
	for filename, data := range map[string][]byte{"auth.json": previous, "config.toml": beforeConfig, "previous-account.txt": []byte(previousName)} {
		if len(data) > 0 {
			if e = config.AtomicWrite(filepath.Join(backup, filename), data, 0600); e != nil {
				return "", e
			}
		}
	}
	currentConfig, e := nativePrivateRead(conf, true)
	if e != nil || !bytes.Equal(beforeConfig, currentConfig) {
		return "", errors.New("native configuration changed; reload and retry; native login was preserved")
	}
	current, e := nativePrivateRead(previousPath, true)
	currentName, linkErr := managedNativeLink(dest, root)
	if e != nil || linkErr != nil || currentName != previousName || !bytes.Equal(previous, current) {
		return "", errors.New("native login changed while preparing the switch; reload and retry")
	}
	tmp, e := os.CreateTemp(shared, ".codexm-default-auth-")
	if e != nil {
		return "", e
	}
	tempName := tmp.Name()
	defer os.Remove(tempName)
	if e = tmp.Close(); e != nil {
		return "", e
	}
	if e = os.Remove(tempName); e != nil {
		return "", e
	}
	if e = os.Symlink(source, tempName); e != nil {
		return "", e
	}
	if e = os.Rename(tempName, dest); e != nil {
		return "", fmt.Errorf("native default was not changed; backup: %s", backup)
	}
	if d, err := os.Open(shared); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return backup, nil
}

func nativePrivateRead(path string, optional bool) ([]byte, error) {
	fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if optional && os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, errors.New("cannot safely read native account or configuration file")
	}
	f := os.NewFile(uintptr(fd), "private native file")
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > 8*1024*1024 {
		return nil, errors.New("native account/configuration requires a regular file of at most 8 MiB")
	}
	b, e := io.ReadAll(io.LimitReader(f, 8*1024*1024+1))
	if e != nil || len(b) > 8*1024*1024 {
		return nil, errors.New("native account/configuration could not be read safely")
	}
	return b, nil
}

func managedNativeLink(dest, root string) (string, error) {
	info, e := os.Lstat(dest)
	if os.IsNotExist(e) {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	if info.Mode().IsRegular() {
		return "", nil
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "", errors.New("native auth must be a regular file or managed account link")
	}
	target, e := os.Readlink(dest)
	if e != nil {
		return "", e
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(dest), target)
	}
	target = filepath.Clean(target)
	name := filepath.Base(filepath.Dir(target))
	if !config.Nickname.MatchString(name) || name == "default" || target != filepath.Join(root, name, "auth.json") {
		return "", errors.New("native auth link points outside saved accounts; native login was preserved")
	}
	if info, e := os.Lstat(filepath.Dir(target)); e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("native auth link must select a real saved account directory")
	}
	return name, nil
}
