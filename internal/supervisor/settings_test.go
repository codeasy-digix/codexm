package supervisor

import (
	"context"
	"github.com/codeasy-digix/codexm/internal/config"
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
	doc.Values.DefaultWorker = "account-secondary"
	doc.Values.MaxParallelJobs = 4
	doc.Values.PreferredRecheckSeconds = 600
	request := func(d SettingsDocument) []byte {
		return mustJSON(map[string]any{"revision": d.Revision, "values": d.Values})
	}
	next, e := s.saveSettings(context.Background(), request(doc))
	if e != nil {
		t.Fatal(e)
	}
	if next.Revision == doc.Revision || s.defaultWorker() != "account-secondary" || s.workerConfig().Execution.MaxParallelJobs != 4 {
		t.Fatal("settings did not apply")
	}
	original.Values.DefaultWorker = "account-main"
	if _, e = s.saveSettings(context.Background(), request(original)); e == nil {
		t.Fatal("stale settings overwrote another client's save")
	}
	c, _ := config.Load(s.Config.Path)
	if c.Runtime != s.Config.Runtime || c.Storage != s.Config.Storage || c.Manager.Account != "account-backup" || c.DefaultWorker() != "account-secondary" {
		t.Fatal("unowned settings changed")
	}
	if _, e = s.saveSettings(context.Background(), mustJSON(map[string]any{"revision": next.Revision, "values": next.Values, "runtime": map[string]string{"codex_binary": "/bad"}})); e == nil {
		t.Fatal("runtime path accepted by GUI settings API")
	}
}
