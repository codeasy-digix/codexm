package supervisor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

// Interactive test fixture only: real native engine, blank isolated logins and
// a fake quota reader. Never creates model turns or uses production state.
func TestInteractivePluginPanelHost(t *testing.T) {
	if os.Getenv("CODEXM_PANEL_TEST") != "1" {
		t.Skip("opt-in interactive plugin panel fixture")
	}
	control := os.Getenv("CODEXM_PANEL_CONTROL")
	if !filepath.IsAbs(control) || os.Getenv("CODEXM_BINARY") == "" {
		t.Fatal("private control path and built binary required")
	}
	s := fixture(t)
	s.Self = os.Getenv("CODEXM_BINARY")
	s.Config.Runtime.CodexBinary = os.Getenv("CODEXM_NATIVE_RUNTIME")
	s.Config.Supervisor.ReconcileIntervalSeconds = 2
	s.Config.Supervisor.ExitWhenNoClientsAndNoPendingWork = false
	s.Config.Logging.UsageSampleSeconds = 3600
	if e := s.Config.Save(); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	r, e := cr.Resolve(ctx, s.Config)
	if e != nil {
		t.Fatal(e)
	}
	s.Resolver = r
	yes := true
	minutes := 300
	reset := time.Now().Add(4 * time.Hour).Unix()
	reader := fakeReader{}
	for i, name := range s.Config.WorkerAccounts.Priority {
		reader[name] = accounts.Snapshot{Authenticated: true, Ordinary: &yes, Single: accounts.Bucket{Primary: &accounts.Window{UsedPercent: 10 + i*20, Minutes: &minutes, ResetsAt: &reset}, Secondary: &accounts.Window{UsedPercent: 15 + i*10}}}
	}
	s.Reader = reader
	for name := range reader {
		if e = s.recordAccount(reader.Read(ctx, name)); e != nil {
			t.Fatal(e)
		}
	}
	first, e := s.newJob(NewArgs{Cwd: t.TempDir(), Title: "패널 시험 대화", Kind: "chat", Account: "account-work", ReadOnly: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ensureRuntime(ctx, first.ID, false); e != nil {
		t.Fatal(e)
	}
	second, e := s.newJob(NewArgs{Cwd: t.TempDir(), Title: "예산 보존 시험 목표", Kind: "chat", Account: "account-secondary", ReadOnly: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ensureRuntime(ctx, second.ID, false); e != nil {
		t.Fatal(e)
	}
	second, _ = s.Store.Job(second.ID)
	if e = s.client(second.ID).Call(ctx, "thread/goal/set", map[string]any{"threadId": second.ThreadID, "objective": "isolated panel fixture", "status": "paused", "origin": "user", "tokenBudget": 30000}, nil); e != nil {
		t.Fatal(e)
	}
	g, e := s.client(second.ID).Goal(ctx, second.ThreadID)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.update(second.ID, func(j *jobs.Job) { j.Goal = g; j.Kind = "goal"; j.Paused = true; j.State = "paused" }); e != nil {
		t.Fatal(e)
	}
	if e = s.park(ctx, second.ID, false); e != nil {
		t.Fatal(e)
	}
	for _, a := range mustActions(t, s) {
		if a.Kind == "start" {
			s.finishAction(a, "done", nil)
		}
	}
	p, e := r.Probe(ctx, "account-backup")
	if e != nil {
		t.Fatal(e)
	}
	var external cr.ResumeResponse
	if e = p.Call(ctx, "thread/start", map[string]any{"cwd": first.Cwd, "persistExtendedHistory": true, "approvalPolicy": "never", "sandbox": "read-only"}, &external); e != nil {
		t.Fatal(e)
	}
	if e = p.Call(ctx, "thread/name/set", map[string]any{"threadId": external.Thread.ID, "name": "등록할 기존 대화"}, nil); e != nil {
		t.Fatal(e)
	}
	p.Close()
	done := make(chan error, 1)
	go func() { done <- s.Run() }()
	t.Cleanup(func() {
		s.cancel()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Error("panel fixture cleanup timeout")
		}
	})
	if e = waitPanelReady(ctx, s.Config); e != nil {
		t.Fatal(e)
	}
	data, _ := json.Marshal(map[string]any{"config": s.Config.Path, "state": s.Config.StateDir(), "first_job": first.ID, "second_job": second.ID, "native_thread": external.Thread.ID, "pid": os.Getpid()})
	if e = config.AtomicWrite(filepath.Join(control, "ready.json"), data, 0600); e != nil {
		t.Fatal(e)
	}
	for {
		if _, e = os.Stat(filepath.Join(control, "stop")); e == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("interactive panel fixture timed out")
		case <-time.After(250 * time.Millisecond):
		}
	}
	counts, _, e := s.Store.DecisionCounts("")
	if e != nil || counts != 0 {
		t.Fatal("panel used manager model")
	}
}

func waitPanelReady(ctx context.Context, c config.Config) error {
	for {
		if Ping(ctx, c) == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
func mustActions(t *testing.T, s *Server) []jobs.Action {
	t.Helper()
	a, e := s.Store.Actions("")
	if e != nil {
		t.Fatal(e)
	}
	return a
}
