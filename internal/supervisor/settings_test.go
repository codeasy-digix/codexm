package supervisor

import (
	"context"
	"encoding/json"
	"github.com/codeasy-digix/codexm/internal/config"
	"strings"
	"testing"
)

func TestEditableSettingsRejectStaleRevisionAndPreserveRuntimePaths(t *testing.T) {
	s := fixture(t)
	if e := s.Config.Save(); e != nil {
		t.Fatal(e)
	}
	doc, e := s.settingsDocument()
	if e != nil {
		t.Fatal(e)
	}
	original := doc
	doc.Values.Priority = []string{"account-secondary", "account-work", "account-main", "account-backup"}
	doc.Values.MaxParallelJobs = 4
	doc.Values.PreferredRecheckSeconds = 600
	request := func(d SettingsDocument) []byte {
		return mustJSON(map[string]any{"revision": d.Revision, "values": d.Values})
	}
	next, e := s.saveSettings(context.Background(), request(doc))
	if e != nil {
		t.Fatal(e)
	}
	if next.Revision == doc.Revision || s.workerConfig().FirstPriority() != "account-secondary" || s.workerConfig().Execution.MaxParallelJobs != 4 {
		t.Fatal("settings did not apply")
	}
	original.Values.Priority = []string{"account-main", "account-work", "account-secondary", "account-backup"}
	if _, e = s.saveSettings(context.Background(), request(original)); e == nil {
		t.Fatal("stale settings overwrote another client's save")
	}
	c, _ := config.Load(s.Config.Path)
	if c.Runtime != s.Config.Runtime || c.Storage != s.Config.Storage || c.Manager.Account != "account-backup" || c.FirstPriority() != "account-secondary" {
		t.Fatal("unowned settings changed")
	}
	if _, e = s.saveSettings(context.Background(), mustJSON(map[string]any{"revision": next.Revision, "values": next.Values, "runtime": map[string]string{"codex_binary": "/bad"}})); e == nil {
		t.Fatal("runtime path accepted by GUI settings API")
	}
}

func TestLegacyPanelDefaultIsIgnoredForFullSaveAndRejectedOnItsOwn(t *testing.T) {
	s := fixture(t)
	if e := s.Config.Save(); e != nil {
		t.Fatal(e)
	}
	doc, e := s.settingsDocument()
	if e != nil {
		t.Fatal(e)
	}
	values := map[string]any{}
	if e = json.Unmarshal(mustJSON(doc.Values), &values); e != nil {
		t.Fatal(e)
	}
	values["default_worker"] = "account-main"
	if _, e = s.saveSettings(context.Background(), mustJSON(map[string]any{"revision": doc.Revision, "values": values, "changed": []string{"default_worker"}})); e == nil {
		t.Fatal("obsolete setting was silently saved")
	}
	values["max_parallel_jobs"] = 4
	result, e := s.saveSettings(context.Background(), mustJSON(map[string]any{"revision": doc.Revision, "values": values}))
	if e != nil {
		t.Fatal(e)
	}
	if s.workerConfig().FirstPriority() != "account-work" || result.Values.MaxParallelJobs != 4 || !strings.Contains(result.Application, "적용하지 않았습니다") {
		t.Fatal("legacy panel changed automatic selection")
	}
}
