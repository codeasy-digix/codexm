package historysynccli

import (
	"fmt"
	"os"
	"path/filepath"
)

// Existing data stores are not moved. Prefer the product-owned XDG config, but
// preserve old installations without silently falling back from a broken link.
func defaultConfigPath(home string) (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(base) {
		base = filepath.Join(home, ".config")
	}
	canonical := filepath.Join(base, "codexm", "history-sync.json")
	for _, p := range []string{canonical, filepath.Join(base, "codex-history-sync", "config.json"), filepath.Join(home, ".codex-history-sync", "v2", "config.json")} {
		if _, err := os.Lstat(p); err == nil {
			return p, nil
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect history-sync configuration: %w", err)
		}
	}
	return canonical, nil
}
