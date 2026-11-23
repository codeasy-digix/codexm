package nativecli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func TestWebComputerUseMarketplaceRequiresExistingNativeInstallation(t *testing.T) {
	c := config.Defaults()
	c.Runtime.SharedCodexHome = t.TempDir()
	c.Runtime.AccountsDir = t.TempDir()
	active := filepath.Join(c.Runtime.AccountsDir, "example")
	marketplace := filepath.Join(".tmp", "bundled-marketplaces", "openai-bundled")
	cache := filepath.Join("plugins", "cache", "openai-bundled", "unified-computer-use")
	sharedMarketplace := filepath.Join(c.Runtime.SharedCodexHome, marketplace)
	writeConfig := func(source string, enabled bool) {
		t.Helper()
		settings := fmt.Sprintf(`[marketplaces.openai-bundled]
source_type = "local"
source = %q
[plugins."unified-computer-use@openai-bundled"]
enabled = %t
`, source, enabled)
		if err := os.WriteFile(filepath.Join(c.Runtime.SharedCodexHome, "config.toml"), []byte(settings), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig(sharedMarketplace, true)
	for _, path := range []string{marketplace, cache} {
		installed := filepath.Join(c.Runtime.SharedCodexHome, path)
		view := filepath.Join(active, path)
		for _, directory := range []string{installed, filepath.Dir(view)} {
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(installed, view); err != nil {
			t.Fatal(err)
		}
	}
	want := fmt.Sprintf("marketplaces.openai-bundled.source=%q", filepath.Join(active, marketplace))
	if value := webComputerUseMarketplace(c, "example"); value != want {
		t.Fatalf("got %q; want %q", value, want)
	}
	if value := webComputerUseMarketplace(c, "default"); value != "" {
		t.Fatal("shared native home should use its own marketplace config")
	}
	writeConfig(sharedMarketplace, false)
	if value := webComputerUseMarketplace(c, "example"); value != "" {
		t.Fatal("disabled plugin altered the selected account's marketplace")
	}
	writeConfig(t.TempDir(), true)
	if value := webComputerUseMarketplace(c, "example"); value != "" {
		t.Fatal("unexpected configured source was rebound")
	}
	writeConfig(sharedMarketplace, true)
	view := filepath.Join(active, marketplace)
	if err := os.Remove(view); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(view, 0700); err != nil {
		t.Fatal(err)
	}
	if value := webComputerUseMarketplace(c, "example"); value != "" {
		t.Fatal("a different marketplace was substituted for the installed source")
	}
}

func TestWebComputerUseApprovalOnlyPublishesCurrentAppConfirmation(t *testing.T) {
	connection := &webConnection{live: map[string]*webLiveEntry{
		"selected": {turn: webLiveTurn{ID: "current", Status: "inProgress"}},
	}}
	form := `{
		"threadId": "selected",
		"turnId": "current",
		"serverName": "cua_repl",
		"mode": "form",
		"message": "Allow Computer Use to use \"TextEdit\"?",
		"requestedSchema": {"type": "object", "properties": {}},
		"_meta": {
			"codex_approval_kind": "mcp_tool_call",
			"connector_id": "computer-use",
			"tool_name": "get_app_state",
			"persist": ["session", "always"],
			"x-codex-turn-metadata": "private-fixture"
		}
	}`
	params, ok := connection.computerUseApproval(json.RawMessage(form))
	if !ok {
		t.Fatal("active app permission was rejected")
	}
	var browser map[string]string
	if err := json.Unmarshal(params, &browser); err != nil ||
		len(browser) != 3 || browser["threadId"] != "selected" || browser["turnId"] != "current" ||
		browser["message"] != `Allow Computer Use to use "TextEdit"?` {
		t.Fatalf("unexpected browser params: %s", params)
	}
	if choices := webApprovalDecisions(cr.Message{Method: webComputerUseRequest}); !slices.Equal(choices, []string{"accept", "decline"}) {
		t.Fatalf("unexpected app permission decisions: %v", choices)
	}
	for _, tc := range []struct{ name, old, new string }{
		{"foreign thread", `"selected"`, `"other"`},
		{"stale turn", `"current"`, `"previous"`},
		{"other MCP", `"cua_repl"`, `"other_server"`},
		{"other connector", `"computer-use"`, `"browser-use"`},
		{"other tool", `"get_app_state"`, `"launch_app"`},
		{"link flow", `"mode": "form"`, `"mode": "url", "url": "https://example.com/"`},
		{"data form", `"properties": {}`, `"properties": {"data": {"type": "string"}}`},
		{"multiline app label", `TextEdit`, `TextEdit\nAlways allow`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := strings.Replace(form, tc.old, tc.new, 1)
			if raw, allowed := connection.computerUseApproval(json.RawMessage(input)); allowed || raw != nil {
				t.Fatalf("unexpectedly forwarded request: %s", raw)
			}
		})
	}
	connection.live["selected"].turn.Status = "completed"
	if _, allowed := connection.computerUseApproval(json.RawMessage(form)); allowed {
		t.Fatal("forwarded a request for a completed turn")
	}
}
