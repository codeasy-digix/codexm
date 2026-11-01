package accounts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/pelletier/go-toml/v2"
)

// NewNativeLoginBroker reuses isolated official authentication, without reading
// retired engine pins or reviving worker/account scheduling configuration.
func NewNativeLoginBroker(ctx context.Context, c config.Config) *LoginBroker {
	b := NewLoginBroker(ctx, c)
	b.start = func(ctx context.Context, c config.Config, home string) (loginTransport, error) {
		r, err := cr.Locate(c)
		if err != nil {
			return nil, err
		}
		return startResolvedLoginTransport(ctx, r, home)
	}
	b.publish = publishNativeLogin
	return b
}

func publishNativeLogin(c config.Config, name, target string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(c.Path), 0700); err != nil {
		return err
	}
	fd, err := syscall.Open(c.Path+".lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	if err = syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	before, err := os.ReadFile(c.Path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	doc := map[string]any{}
	if len(before) > 0 {
		if err = toml.Unmarshal(before, &doc); err != nil {
			return err
		}
	}
	changed := false
	if registry, ok := doc["account_registry"].(map[string]any); ok {
		if removed, ok := registry["removed"].([]any); ok {
			kept := []any{}
			for _, value := range removed {
				if value == name {
					changed = true
				} else {
					kept = append(kept, value)
				}
			}
			if changed {
				registry["removed"] = kept
			}
		}
	}
	var after []byte
	if changed {
		after, err = toml.Marshal(doc)
		if err != nil {
			return err
		}
		if err = config.AtomicWrite(c.Path+".previous", before, 0600); err != nil {
			return err
		}
	}
	previous, existed, err := readAuth(target)
	if err != nil {
		return err
	}
	if err = config.AtomicWrite(target, data, 0600); err != nil {
		return err
	}
	if changed {
		if err = config.AtomicWrite(c.Path, after, 0600); err != nil {
			if existed {
				return errors.Join(err, config.AtomicWrite(target, previous, 0600))
			}
			return errors.Join(err, os.Remove(target))
		}
	}
	return nil
}
