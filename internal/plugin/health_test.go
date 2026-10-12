package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/codeasy-digix/codexm/internal/config"
)

func TestPanelConnectionIsSeparateFromOpenAndResourceObservations(t *testing.T) {
	i := fixture(t)
	m := MCP{Config: i.Config, Self: i.Self}
	before, _ := os.ReadFile(i.Config.Path)
	_, current, e := uiResource()
	if e != nil {
		t.Fatal(e)
	}
	html, _ := HTML()
	if !strings.Contains(html, "const UI_BUILD='"+current+"'") || strings.Contains(html, "__CODEXM_UI_BUILD__") {
		t.Fatal("served UI cannot identify its immutable build")
	}
	for _, request := range []rpcRequest{
		{Method: "initialize", Params: json.RawMessage(`{"clientInfo":{"name":"CANARY-PRIVATE-TEXT"}}`)},
		{Method: "resources/read", Params: json.RawMessage(`{"uri":"` + ResourceURI + `"}`)},
		{Method: "tools/call", Params: json.RawMessage(`{"name":"codexm_open","arguments":{}}`)},
	} {
		result, rpcErr := m.handle(context.Background(), request)
		if rpcErr != nil {
			t.Fatal(rpcErr)
		}
		if request.Method == "tools/call" {
			meta := result.(map[string]any)["_meta"].(map[string]any)
			if meta["ui"].(map[string]any)["resourceUri"] != ResourceURI || meta["openai/outputTemplate"] != ResourceURI {
				t.Fatal("open result cannot be resolved without a tool catalog")
			}
		}
	}
	health := i.panelHealth()
	observations := health["observations"].(map[string]panelObservation)
	if len(observations) != 3 || health["panel_connection_matches_current_ui"] != false || health["proves_visible_ui"] != false {
		t.Fatal("an open request or resource response was mistaken for a displayed panel", health)
	}
	args, _ := json.Marshal(map[string]any{"ui_build": current, "bridge": "mcp-app", "visible": true})
	result, e := m.panelReady(args)
	if e != nil || result.(map[string]any)["recorded"] != true {
		t.Fatal(result, e)
	}
	health = i.panelHealth()
	if health["panel_connection_matches_current_ui"] != true || i.Status()["host_ui_verified"] != false || health["proves_visible_ui"] != false {
		t.Fatal("bridge observation changed the UI verification claim", health)
	}
	after, _ := os.ReadFile(i.Config.Path)
	if string(before) != string(after) {
		t.Fatal("panel diagnostics changed settings")
	}
	for _, stage := range panelStages {
		p := filepath.Join(i.Config.StateDir(), "plugin-health", stage+".json")
		data, e := os.ReadFile(p)
		st, statErr := os.Stat(p)
		if e != nil || statErr != nil || st.Mode().Perm() != 0600 || strings.Contains(string(data), "CANARY-PRIVATE-TEXT") {
			t.Fatal("diagnostic must contain only private, whitelisted observations", stage, e, statErr)
		}
	}
	if _, e := os.Stat(filepath.Join(i.Config.StateDir(), "services")); !os.IsNotExist(e) {
		t.Fatal("bridge report started supervision")
	}
	if _, e := os.Stat(filepath.Join(i.Config.Runtime.SharedCodexHome, "state_5.sqlite")); !os.IsNotExist(e) {
		t.Fatal("bridge report opened a conversation database")
	}
}

func TestPanelReportsDetectOldCacheAndRejectUnrelatedData(t *testing.T) {
	i := fixture(t)
	m := MCP{Config: i.Config}
	oldBuild := strings.Repeat("0", 64)
	if _, e := m.panelReady(json.RawMessage(`{"ui_build":"` + oldBuild + `","bridge":"legacy","visible":false}`)); e != nil {
		t.Fatal(e)
	}
	if i.panelHealth()["panel_connection_matches_current_ui"] != false {
		t.Fatal("a stale UI cache was accepted as current")
	}
	p := filepath.Join(i.Config.StateDir(), "plugin-health", "panel_connected.json")
	before, _ := os.ReadFile(p)
	for _, invalid := range []string{
		`{"ui_build":"` + oldBuild + `","bridge":"host-secret","visible":true}`,
		`{"ui_build":"` + oldBuild + `","bridge":"mcp-app","visible":null}`,
		`{"ui_build":"auth-canary","bridge":"mcp-app","visible":true}`,
		`{"ui_build":"` + oldBuild + `","bridge":"mcp-app","visible":true,"conversation":"CANARY"}`,
	} {
		if _, e := m.panelReady(json.RawMessage(invalid)); e == nil {
			t.Fatal("unrelated diagnostic data accepted")
		}
		after, _ := os.ReadFile(p)
		if string(before) != string(after) {
			t.Fatal("invalid report changed the last valid observation")
		}
	}
}

func TestPanelObservationFailureDoesNotBlockMCP(t *testing.T) {
	i := fixture(t)
	if e := config.AtomicWrite(i.Config.StateDir(), []byte("not-a-directory"), 0600); e != nil {
		t.Fatal(e)
	}
	m := MCP{Config: i.Config}
	if _, e := m.handle(context.Background(), rpcRequest{Method: "initialize"}); e != nil {
		t.Fatal("diagnostic failure blocked initialization", e)
	}
	if _, e := m.handle(context.Background(), rpcRequest{Method: "resources/read", Params: json.RawMessage(`{"uri":"` + ResourceURI + `"}`)}); e != nil {
		t.Fatal("diagnostic failure blocked resource read", e)
	}
	_, build, _ := uiResource()
	args, _ := json.Marshal(map[string]any{"ui_build": build, "bridge": "mcp-app", "visible": true})
	result, e := m.panelReady(args)
	if e != nil || result.(map[string]any)["recorded"] != false {
		t.Fatal("optional diagnostic failure blocked the panel", result, e)
	}
}

func TestConcurrentMCPObservationsRetainIndependentStages(t *testing.T) {
	i := fixture(t)
	var wg sync.WaitGroup
	for _, stage := range panelStages {
		wg.Go(func() {
			if e := (MCP{Config: i.Config}).observePanel(stage, nil); e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	if len(i.panelHealth()["observations"].(map[string]panelObservation)) != len(panelStages) {
		t.Fatal("a concurrent MCP process erased another stage")
	}
}
