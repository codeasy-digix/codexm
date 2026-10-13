package config

import (
	"os"

	"github.com/pelletier/go-toml/v2"
)

// RemoveLegacyWorkerDefault only drops the obsolete worker default. Unknown
// extension settings are kept, and the original document remains in .previous.
// Reads and normal commands never run this migration implicitly.
func RemoveLegacyWorkerDefault(path string) (changed bool, err error) {
	if path == "" {
		path = DefaultPath()
	}
	path = Expand(path)
	if _, e := os.Stat(path); os.IsNotExist(e) {
		return false, nil
	} else if e != nil {
		return false, e
	}
	err = withConfigLock(path, func() error {
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		var document map[string]any
		if e = toml.Unmarshal(b, &document); e != nil {
			return e
		}
		workers, ok := document["worker_accounts"].(map[string]any)
		if !ok {
			return nil
		}
		if _, ok = workers["default_account"]; !ok {
			return nil
		}
		if _, e = Load(path); e != nil {
			return e
		}
		delete(workers, "default_account")
		updated, e := toml.Marshal(document)
		if e != nil {
			return e
		}
		if e = AtomicWrite(path+".previous", b, 0600); e != nil {
			return e
		}
		if e = AtomicWrite(path, updated, 0600); e != nil {
			return e
		}
		changed = true
		return nil
	})
	return
}
