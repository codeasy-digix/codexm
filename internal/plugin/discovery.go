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
	result := map[string]any{"mcp_server_discovered": found, "plugin_skills_discovered": len(response.Plugin.Skills), "registration_skills_discovered": registration, "command_skills_discovered": commands, "skill_names": names, "host_installed": response.Plugin.Summary.Installed, "host_enabled": response.Plugin.Summary.Enabled, "host_installed_version": response.Plugin.Summary.LocalVersion, "host_cache_current": response.Plugin.Summary.LocalVersion == config.Version}
	var hookList struct {
		Data []struct {
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
			Warnings []string `json:"warnings"`
			Hooks    []struct {
				PluginID string `json:"pluginId"`
				Event    string `json:"eventName"`
				Command  string `json:"command"`
				Enabled  bool   `json:"enabled"`
				Trust    string `json:"trustStatus"`
				Key      string `json:"key"`
				Hash     string `json:"currentHash"`
			} `json:"hooks"`
		} `json:"data"`
	}
	if err := c.Call(ctx, "hooks/list", map[string]any{"cwds": []string{i.home()}}, &hookList); err != nil {
		result["prompt_command_hook_discovery_problem"] = cr.SafeText(err.Error())
	} else {
		count := 0
		problems := []string{}
		for _, entry := range hookList.Data {
			for _, problem := range entry.Errors {
				problems = append(problems, cr.SafeText(problem.Message))
			}
			for _, warning := range entry.Warnings {
				problems = append(problems, cr.SafeText(warning))
			}
			for _, hook := range entry.Hooks {
				if strings.HasPrefix(hook.PluginID, "codexm@") && hook.Event == "userPromptSubmit" && strings.Contains(hook.Command, "__prompt-command") {
					count++
					result["prompt_command_hook_trust"] = hook.Trust
					result["prompt_command_hook_enabled"] = hook.Enabled
					result["prompt_command_hook_key"] = hook.Key
					result["prompt_command_hook_hash"] = hook.Hash
				}
			}
		}
		result["prompt_command_hooks_discovered"] = count
		if len(problems) > 0 {
			result["hook_discovery_problems"] = problems
		}
	}
	return result, nil
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
