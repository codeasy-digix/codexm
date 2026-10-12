package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

// The real official engine must discover and call the installed plugin; a valid
// JSON manifest and a simulated iframe do not prove host integration.
func TestOfficialPluginDiscoversSettingsEntrypointsAndRegistrationSkill(t *testing.T) {
	if os.Getenv("CODEXM_PLUGIN_TEST") != "1" {
		t.Skip("opt-in isolated official plugin integration")
	}
	i := fixture(t)
	i.Self = os.Getenv("CODEXM_BINARY")
	i.Config.Runtime.CodexBinary = os.Getenv("CODEXM_NATIVE_RUNTIME")
	if !filepath.IsAbs(i.Self) || i.Config.Runtime.CodexBinary == "" {
		t.Fatal("built binary and official runtime required")
	}
	if e := i.Config.Save(); e != nil {
		t.Fatal(e)
	}
	if e := config.AtomicWrite(filepath.Join(i.Config.Runtime.SharedCodexHome, "auth.json"), []byte("{}"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := i.Install(); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, i.Config.Runtime.CodexBinary, "--no-daemon", "plugin", "add", "codexm@local-personal", "--json")
	cmd.Dir, cmd.Env = i.Home, pluginEnvironment(i.Home, i.Config.Runtime.SharedCodexHome)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("isolated plugin install failed: %v %s", e, cr.SafeText(string(b)))
	}
	discovery, e := i.Discovery(ctx)
	if e != nil || discovery["mcp_server_discovered"] != true || discovery["registration_skills_discovered"] != 1 || discovery["host_cache_current"] != true {
		t.Fatal("official package discovery", discovery, e)
	}
	cmd = exec.CommandContext(ctx, i.Config.Runtime.CodexBinary, "--no-daemon", "app-server", "--listen", "stdio://")
	cmd.Dir, cmd.Env = i.Home, pluginEnvironment(i.Home, i.Config.Runtime.SharedCodexHome)
	c, e := cr.StartStdio(ctx, cmd)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	var thread cr.ResumeResponse
	if e = c.Call(ctx, "thread/start", map[string]any{"cwd": i.Home, "sandbox": "read-only", "approvalPolicy": "never"}, &thread); e != nil {
		t.Fatal(e)
	}
	var status struct {
		Data []struct {
			Name          string                     `json:"name"`
			RuntimeStatus string                     `json:"runtimeStatus"`
			Tools         map[string]json.RawMessage `json:"tools"`
			Capabilities  map[string]json.RawMessage `json:"serverCapabilities"`
		} `json:"data"`
	}
	server := ""
	deadline := time.Now().Add(25 * time.Second)
	for server == "" && time.Now().Before(deadline) {
		if e = c.Call(ctx, "mcpServerStatus/list", map[string]any{"threadId": thread.Thread.ID, "detail": "full"}, &status); e != nil {
			t.Fatal(e)
		}
		for _, entry := range status.Data {
			if tool, found := entry.Tools["codexm_open"]; found && entry.RuntimeStatus == "connected" {
				if !bytes.Contains(tool, []byte(`"openai/ui"`)) || !bytes.Contains(tool, []byte(`"global"`)) || !bytes.Contains(tool, []byte(`"thread"`)) || !bytes.Contains(tool, []byte(`"settings"`)) {
					t.Fatal("management entrypoints absent")
				}
				capabilities, _ := json.Marshal(entry.Capabilities)
				if !bytes.Contains(capabilities, []byte("openai/settings")) || entry.Tools["codexm_settings_read"] == nil || entry.Tools["codexm_settings_update"] == nil {
					t.Fatal("native settings extension absent")
				}
				server = entry.Name
			}
		}
		if server == "" {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if server == "" {
		t.Fatal("installed MCP plugin did not connect")
	}
	var result json.RawMessage
	if e = c.Call(ctx, "mcpServer/tool/call", map[string]any{"threadId": thread.Thread.ID, "server": server, "tool": "codexm_settings_read", "arguments": map[string]any{}}, &result); e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(result, []byte("codexm_open")) || !bytes.Contains(result, []byte("preferred_recheck_seconds")) {
		t.Fatal("settings did not expose management action and account policy")
	}
	// Exercise the real native resource transport as well as tool discovery.
	// This is still a synthetic client, not evidence of a displayed GUI.
	if e = c.Call(ctx, "mcpServer/tool/call", map[string]any{"threadId": thread.Thread.ID, "server": server, "tool": "codexm_open", "arguments": map[string]any{}}, &result); e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(result, []byte(ResourceURI)) {
		t.Fatal("open result omitted its resource fallback")
	}
	if e = c.Call(ctx, "mcpServer/resource/read", map[string]any{"threadId": thread.Thread.ID, "server": server, "uri": ResourceURI}, &result); e != nil {
		t.Fatal(e)
	}
	_, build, e := uiResource()
	if e != nil || !bytes.Contains(result, []byte(build)) || !bytes.Contains(result, []byte("availableDisplayModes")) {
		t.Fatal("native transport did not return the current UI and display modes", e)
	}
	if e = c.Call(ctx, "mcpServer/tool/call", map[string]any{"threadId": thread.Thread.ID, "server": server, "tool": "codexm_ui_ready", "arguments": map[string]any{"ui_build": build, "bridge": "mcp-app", "visible": false}}, &result); e != nil {
		t.Fatal(e)
	}
	if i.panelHealth()["panel_connection_matches_current_ui"] != true || i.Status()["host_ui_verified"] != false {
		t.Fatal("native diagnostics confused a synthetic connection with visible UI")
	}
	if _, e := os.Stat(filepath.Join(i.Config.StateDir(), "services")); !os.IsNotExist(e) {
		t.Fatal("plugin discovery started supervision")
	}
	if recent, _, e := c.Recent(ctx, thread.Thread.ID, 1, ""); e != nil || len(recent.Turns) != 0 {
		t.Fatal("plugin discovery generated a model turn", e)
	}
}
