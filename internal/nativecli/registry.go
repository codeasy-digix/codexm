package nativecli

import (
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/pelletier/go-toml/v2"
	"golang.org/x/sys/unix"
)

// RemoveAccount hides registration only. Unlike the retired manager it never
// changes job ownership, resumes work or removes authentication/history.
func RemoveAccount(c config.Config, name string) error {
	if !config.Nickname.MatchString(name) || name == "default" {
		return errors.New("invalid account")
	}
	names, e := accounts.SavedLogins(c)
	if e != nil {
		return e
	}
	if !slices.Contains(names, name) {
		return errors.New("choose a saved account")
	}
	if e = os.MkdirAll(filepath.Dir(c.Path), 0700); e != nil {
		return e
	}
	fd, e := unix.Open(c.Path+".lock", unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	defer unix.Close(fd)
	if e = unix.Flock(fd, unix.LOCK_EX); e != nil {
		return e
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	doc := map[string]any{}
	before, e := os.ReadFile(c.Path)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if len(before) > 0 {
		if e = toml.Unmarshal(before, &doc); e != nil {
			return e
		}
	}
	registry, _ := doc["account_registry"].(map[string]any)
	if registry == nil {
		registry = map[string]any{}
	}
	removed := []string{}
	if values, ok := registry["removed"].([]any); ok {
		for _, v := range values {
			if s, ok := v.(string); ok {
				removed = append(removed, s)
			}
		}
	}
	if !slices.Contains(removed, name) {
		removed = append(removed, name)
	}
	slices.Sort(removed)
	registry["removed"] = removed
	doc["account_registry"] = registry
	// Preserve old records without restoring a removed nickname to old policy.
	if workers, ok := doc["worker_accounts"].(map[string]any); ok {
		if values, ok := workers["priority"].([]any); ok {
			kept := []any{}
			for _, v := range values {
				if text, ok := v.(string); !ok || text != name {
					kept = append(kept, v)
				}
			}
			workers["priority"] = kept
		}
		if workers["fallback_account"] == name {
			workers["fallback_account"] = ""
		}
	}
	after, e := toml.Marshal(doc)
	if e != nil {
		return e
	}
	if len(before) > 0 {
		if e = config.AtomicWrite(c.Path+".previous", before, 0600); e != nil {
			return e
		}
	}
	return config.AtomicWrite(c.Path, after, 0600)
}
