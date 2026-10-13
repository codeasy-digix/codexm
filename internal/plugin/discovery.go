package plugin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

// Discovery asks the installed official engine to parse our package. File
// existence alone does not prove that a host can load its MCP server. This
// metadata probe never loads a conversation, starts supervision or calls a model.
func (i Installer) Discovery(ctx context.Context) (map[string]any, error) {
	binary, e := cr.Locate(i.Config)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary.Binary, "--no-daemon", "app-server", "--listen", "stdio://")
	cmd.Dir = i.home()
	cmd.Env = pluginEnvironment(i.home(), config.Expand(i.Config.Runtime.SharedCodexHome))
	c, e := cr.StartStdio(ctx, cmd)
	if e != nil {
		return nil, e
	}
	defer c.Close()
	var response struct {
		Plugin struct {
			Servers []string          `json:"mcpServers"`
			Skills  []json.RawMessage `json:"skills"`
			Summary struct {
				LocalVersion string `json:"localVersion"`
				Installed    bool   `json:"installed"`
				Enabled      bool   `json:"enabled"`
			} `json:"summary"`
		} `json:"plugin"`
	}
	if e = c.Call(ctx, "plugin/read", map[string]string{"pluginName": "codexm", "marketplacePath": i.catalog()}, &response); e != nil {
		return nil, e
	}
	found := false
	for _, name := range response.Plugin.Servers {
		found = found || name == "codexm"
	}
	names := []string{}
	registration, commands := 0, 0
	for _, raw := range response.Plugin.Skills {
		var skill struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &skill) != nil || skill.Name == "" {
			continue
		}
		name := skill.Name
		if index := strings.LastIndex(name, ":"); index >= 0 {
			name = name[index+1:]
		}
		names = append(names, name)
		if name == "codexm" {
			registration++
		} else if name == "codexm-manage" || name == "codexm-accounts" {
			commands++
		}
	}
	sort.Strings(names)
	return map[string]any{"mcp_server_discovered": found, "plugin_skills_discovered": len(response.Plugin.Skills), "registration_skills_discovered": registration, "command_skills_discovered": commands, "skill_names": names, "host_installed": response.Plugin.Summary.Installed, "host_enabled": response.Plugin.Summary.Enabled, "host_installed_version": response.Plugin.Summary.LocalVersion, "host_cache_current": response.Plugin.Summary.LocalVersion == config.Version}, nil
}

func pluginEnvironment(home, codexHome string) []string {
	env := []string{}
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "HOME=") && !strings.HasPrefix(value, "CODEX_HOME=") {
			env = append(env, value)
		}
	}
	return append(env, "HOME="+home, "CODEX_HOME="+codexHome)
}
