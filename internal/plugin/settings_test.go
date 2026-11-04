package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeasy-digix/codexm/internal/config"
)

func configuredPlugin(t *testing.T) MCP {
	i := fixture(t)
	i.Config.WorkerAccounts.Priority = []string{"account-work", "account-secondary", "account-main", "account-backup"}
	i.Config.WorkerAccounts.FallbackAccount = "account-backup"
	i.Config.Manager.Account = "account-backup"
	for _, name := range i.Config.WorkerAccounts.Priority {
		if e := config.AtomicWrite(filepath.Join(i.Config.Runtime.AccountsDir, name, "auth.json"), []byte(`{}`), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := i.Config.Save(); e != nil {
		t.Fatal(e)
	}
	return MCP{Config: i.Config, Self: i.Self}
}

func TestNativePluginSettingsAdvertisePanelsAndMergeOnlyChangedValues(t *testing.T) {
	m := configuredPlugin(t)
	for _, method := range []string{"initialize", "server/discover"} {
		result, rpcErr := m.handle(context.Background(), rpcRequest{Method: method})
		b, _ := json.Marshal(result)
		if rpcErr != nil || !strings.Contains(string(b), `"openai/settings"`) || !strings.Contains(string(b), `"codexm_settings_read"`) {
			t.Fatal("missing native settings capability", method)
		}
	}
	result, e := m.readPluginSettings()
	if e != nil {
		t.Fatal(e)
	}
	d := result.(map[string]any)
	values := d["values"].(map[string]any)
	if _, ok := values["default_worker"]; ok {
		t.Fatal("independent worker default was advertised")
	}
	if _, ok := settingProperties(nil)["default_worker"]; ok {
		t.Fatal("independent default schema was advertised")
	}
	for key := range settingProperties(nil) {
		if _, ok := values[key]; !ok {
			t.Fatal("missing effective value", key)
		}
	}
	layout, _ := json.Marshal(d["layout"])
	if !strings.Contains(string(layout), `"tool":"codexm_open"`) {
		t.Fatal("settings management action absent")
	}
	before, _ := config.Load(m.Config.Path)
	if _, e = m.updatePluginSettings(context.Background(), json.RawMessage(`{"set":{"preferred_recheck_seconds":900,"five_hour_discord":true}}`)); e != nil {
		t.Fatal(e)
	}
	after, _ := config.Load(m.Config.Path)
	if after.WorkerAccounts.PreferredRecheckSeconds != 900 || !after.Notifications.FiveHourDiscord || after.Runtime != before.Runtime || after.Manager != before.Manager || strings.Join(after.WorkerAccounts.Priority, ",") != strings.Join(before.WorkerAccounts.Priority, ",") {
		t.Fatal("partial update changed unrelated state")
	}
	for _, path := range []string{"services", "state.sqlite"} {
		if _, e = os.Stat(filepath.Join(m.Config.StateDir(), path)); !os.IsNotExist(e) {
			t.Fatal("settings started management", path)
		}
	}
	for _, tool := range toolList() {
		entry := tool.(map[string]any)
		if entry["name"] == "codexm_open" {
			b, _ := json.Marshal(entry)
			if !strings.Contains(string(b), `"type":"global"`) || !strings.Contains(string(b), `"type":"thread"`) || !strings.Contains(string(b), `"type":"settings"`) || entry["title"] != "codexm 관리" {
				t.Fatal("missing click-to-open entrypoint")
			}
		}
	}
}

func TestNativeSettingsRejectMalformedUpdatesWithoutChangingFile(t *testing.T) {
	m := configuredPlugin(t)
	before, _ := os.ReadFile(m.Config.Path)
	for _, args := range []string{`{"set":{"default_worker":"account-secondary"}}`, `{"set":{}}`, `{"set":{"five_hour_discord":null}}`, `{"set":{"five_hour_discord":"true"}}`, `{"set":{"max_parallel_jobs":1.5}}`, `{"set":{"max_parallel_jobs":0}}`, `{"set":{"runtime":"arbitrary"}}`, `{"set":{"priority":"account-work, account-work"}}`, `{"set":{"manager_account":"missing"}}`, `{"set":{"priority":"account-work, account-backup, account-secondary"}}`, `{"set":{"max_parallel_jobs":2},"extra":true}`} {
		if _, e := m.updatePluginSettings(context.Background(), json.RawMessage(args)); e == nil {
			t.Fatal("invalid settings accepted", args)
		}
		after, _ := os.ReadFile(m.Config.Path)
		if string(before) != string(after) {
			t.Fatal("invalid update changed config", args)
		}
	}
}

func TestPreviousHostResourceReferenceRemainsReadableAfterPluginUpgrade(t *testing.T) {
	m := configuredPlugin(t)
	for _, uri := range []string{legacyResourceURI, olderResourceURI, previousResourceURI} {
		params, _ := json.Marshal(map[string]string{"uri": uri})
		result, rpcErr := m.handle(context.Background(), rpcRequest{Method: "resources/read", Params: params})
		b, _ := json.Marshal(result)
		if rpcErr != nil || !strings.Contains(string(b), uri) || !strings.Contains(string(b), "ui/initialize") {
			t.Fatal("cached host resource reference broke after upgrade", uri)
		}
	}
}
