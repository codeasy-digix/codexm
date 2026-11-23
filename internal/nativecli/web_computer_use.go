package nativecli

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/pelletier/go-toml/v2"
)

// Named accounts share the machine's config. Its installed bundled marketplace
// is registered by absolute path, which Codex validates against the current
// account's home. Accept only a locally provisioned view of the same installed
// directories. Credentials and the native config stay in their original homes.
func webComputerUseMarketplace(c config.Config, account string) string {
	if account == "default" || !config.Nickname.MatchString(account) {
		return ""
	}
	shared := config.Expand(c.Runtime.SharedCodexHome)
	active := filepath.Join(config.Expand(c.Runtime.AccountsDir), account)
	marketplacePath := filepath.Join(".tmp", "bundled-marketplaces", "openai-bundled")
	pluginPath := filepath.Join("plugins", "cache", "openai-bundled", "unified-computer-use")
	if !sameInstalledDirectory(filepath.Join(shared, marketplacePath), filepath.Join(active, marketplacePath)) ||
		!sameInstalledDirectory(filepath.Join(shared, pluginPath), filepath.Join(active, pluginPath)) {
		return ""
	}
	var settings struct {
		Marketplaces map[string]struct {
			Type   string `toml:"source_type"`
			Source string `toml:"source"`
		} `toml:"marketplaces"`
		Plugins map[string]struct {
			Enabled bool `toml:"enabled"`
		} `toml:"plugins"`
	}
	data, err := os.ReadFile(filepath.Join(shared, "config.toml"))
	if err != nil || toml.Unmarshal(data, &settings) != nil {
		return ""
	}
	bundled := settings.Marketplaces["openai-bundled"]
	if bundled.Type != "local" || filepath.Clean(bundled.Source) != filepath.Join(shared, marketplacePath) ||
		!settings.Plugins["unified-computer-use@openai-bundled"].Enabled {
		return ""
	}
	value, _ := json.Marshal(filepath.Join(active, marketplacePath))
	return "marketplaces.openai-bundled.source=" + string(value)
}

func sameInstalledDirectory(installed, candidate string) bool {
	source, err := os.Lstat(installed)
	if err != nil || !source.IsDir() {
		return false
	}
	view, err := os.Stat(candidate)
	return err == nil && os.SameFile(source, view)
}
