package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func withConfigLock(path string, fn func() error) error {
	if path == "" {
		path = DefaultPath()
	}
	path = Expand(path)
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	fd, e := syscall.Open(path+".lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	defer syscall.Close(fd)
	if e = syscall.Flock(fd, syscall.LOCK_EX); e != nil {
		return e
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	return fn()
}

func revision(path string) (string, error) {
	b, e := os.ReadFile(path)
	if e != nil && !os.IsNotExist(e) {
		return "", e
	}
	d := sha256.Sum256(b)
	return hex.EncodeToString(d[:]), nil
}

// Current returns a revision of the exact on-disk document, not a UI copy.
func Current(path string) (c Config, rev string, err error) {
	err = withConfigLock(path, func() error {
		var e error
		c, e = Load(path)
		if e != nil {
			return e
		}
		rev, e = revision(c.Path)
		return e
	})
	return
}

// Update merges owned fields into the latest configuration under a process-wide
// file lock. A stale GUI revision fails without touching either saved file.
func Update(path, expected string, mutate func(*Config) error) (c Config, rev string, err error) {
	err = withConfigLock(path, func() error {
		var e error
		c, e = Load(path)
		if e != nil {
			return e
		}
		rev, e = revision(c.Path)
		if e != nil {
			return e
		}
		if expected != "" && rev != expected {
			return errors.New("settings changed in another client; reload before saving")
		}
		if e = mutate(&c); e != nil {
			return e
		}
		if e = c.saveUnlocked(); e != nil {
			return e
		}
		rev, e = revision(c.Path)
		return e
	})
	return
}
