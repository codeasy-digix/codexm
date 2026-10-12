package supervisor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"golang.org/x/sys/unix"
)

func foreignWriter(t *testing.T, home, thread string) func() {
	t.Helper()
	p := filepath.Join(home, "thread-writer-locks", thread+".lock")
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		t.Fatal(e)
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }
}

func TestUnregisterRefusesDispatchBeforeChangingWorkerAndCancelsStaleAction(t *testing.T) {
	s := fixture(t)
	j := jobs.Job{ID: jobs.ID(), Managed: true, Kind: "chat", State: "idle", Session: "preserved"}
	s.Store.PutJob(j)
	s.dispatching[j.ID] = true
	if _, e := s.unregisterThread(s.ctx, j.ID); e == nil {
		t.Fatal("unregister accepted an in-flight dispatch")
	}
	got, _ := s.Store.Job(j.ID)
	if got.Session != j.Session || !got.Managed || got.State != j.State {
		t.Fatal("rejected unregister changed the worker")
	}
	delete(s.dispatching, j.ID)
	j.Session = ""
	s.Store.PutJob(j)
	a := jobs.Action{ID: jobs.ID(), JobID: j.ID, Kind: "start", State: "pending"}
	s.Store.PutAction(a)
	if _, e := s.unregisterThread(s.ctx, j.ID); e != nil {
		t.Fatal(e)
	}
	s.execute(a) // Simulate the action loop retaining an old pending snapshot.
	got, _ = s.Store.Job(j.ID)
	if got.Managed || got.Session != "" || got.State != "observed" {
		t.Fatal("stale dispatch restarted an unregistered chat")
	}
}

func TestOfficialCurrentChatRegistrationAndPausedAccountChangePreserveGoal(t *testing.T) {
	if os.Getenv("CODEXM_NATIVE_TEST") != "1" {
		t.Skip("opt-in native registration test")
	}
	s := fixture(t)
	s.Self = os.Getenv("CODEXM_BINARY")
	s.Config.Runtime.CodexBinary = os.Getenv("CODEXM_NATIVE_RUNTIME")
	if e := s.Config.Save(); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	r, e := cr.Resolve(ctx, s.Config)
	if e != nil {
		t.Fatal(e)
	}
	s.Resolver = r
	ordinary := true
	s.Reader = fakeReader{"account-work": {Authenticated: true, Ordinary: &ordinary}, "account-secondary": {Authenticated: true, Ordinary: &ordinary}}
	p, e := r.Probe(ctx, "account-work")
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	var original cr.ResumeResponse
	if e = p.Call(ctx, "thread/start", map[string]any{"cwd": t.TempDir(), "model": "gpt-6.1-sol", "sandbox": "read-only", "approvalPolicy": "never"}, &original); e != nil {
		t.Fatal(e)
	}
	budget := 30000
	if e = p.Call(ctx, "thread/goal/set", map[string]any{"threadId": original.Thread.ID, "objective": "isolated native registration", "status": "paused", "tokenBudget": budget, "origin": "user"}, nil); e != nil {
		t.Fatal(e)
	}
	j, e := s.registerThread(ctx, JobArgs{ThreadID: original.Thread.ID, Account: "account-work", OriginAccount: "account-work"})
	if e != nil || j.Goal == nil || !j.Paused || j.Model != original.Model || *j.Goal.TokenBudget != int64(budget) {
		t.Fatal("native registration did not preserve metadata", e)
	}
	again, e := s.registerThread(ctx, JobArgs{ThreadID: original.Thread.ID, Account: "account-secondary"})
	if e != nil || again.ID != j.ID || again.Account != j.Account {
		t.Fatal("registration was not idempotent", e)
	}
	results := make(chan jobs.Job, 4)
	errors := make(chan error, 4)
	for range 4 {
		go func() {
			job, err := s.registerThread(ctx, JobArgs{ThreadID: original.Thread.ID, Account: "account-work"})
			results <- job
			errors <- err
		}()
	}
	for range 4 {
		if result, err := <-results, <-errors; err != nil || result.ID != j.ID {
			t.Fatal("concurrent registration duplicated a chat", err)
		}
	}
	a, e := s.requestAccountChange(JobArgs{ID: j.ID, Account: "account-secondary", ExpectedAccount: "account-work", OperationID: jobs.ID()})
	if e != nil {
		t.Fatal(e)
	}
	s.execute(a)
	got, _ := s.Store.Job(j.ID)
	if got.Account != "account-work" || got.TargetAccount != "account-secondary" || got.Session != "" || !cr.WriterBusy(j.Store, j.ThreadID) {
		t.Fatal("handoff disturbed the original writer")
	}
	p.Close()
	if e = cr.WaitWriterRelease(ctx, j.Store, j.ThreadID); e != nil {
		t.Fatal(e)
	}
	s.execute(a)
	got, _ = s.Store.Job(j.ID)
	defer s.park(context.Background(), j.ID, false)
	if got.Account != "account-secondary" || got.ThreadID != original.Thread.ID || !got.Paused || got.Goal == nil || got.Goal.Status != "paused" || *got.Goal.TokenBudget != int64(budget) || got.Goal.TokensUsed != 0 {
		t.Fatal("handoff did not preserve same-chat intent and budget")
	}
	probe, e := r.Probe(ctx, "account-backup")
	if e != nil {
		t.Fatal(e)
	}
	defer probe.Close()
	if recent, _, e := probe.Recent(ctx, j.ThreadID, 1, ""); e != nil || len(recent.Turns) != 0 {
		t.Fatal("registration generated a model turn", e)
	}
}

func TestRegistrationObservesQuotaAndWaitsForForeignWriterWithoutChangingGoal(t *testing.T) {
	s := fixture(t)
	budget := int64(30000)
	j := jobs.Job{ID: jobs.ID(), ThreadID: jobs.ID(), Store: s.Config.Runtime.SharedCodexHome, Account: "account-work", InitialAccount: "account-work", State: "observed", Kind: "goal", Goal: &jobs.Goal{Objective: "private existing goal", Status: "active", TokenBudget: &budget, TokensUsed: 7654}, Model: "existing-model", Effort: "high"}
	if e := s.Store.PutJob(j); e != nil {
		t.Fatal(e)
	}
	release := foreignWriter(t, j.Store, j.ThreadID)
	defer release()
	got, e := s.registerObserved(j, "account-work")
	if e != nil {
		t.Fatal(e)
	}
	if !got.Managed || got.State != "external_writer" || got.ThreadID != j.ThreadID || got.Model != j.Model || got.Effort != j.Effort || got.Goal.TokensUsed != 7654 || *got.Goal.TokenBudget != budget {
		t.Fatal("registration changed original intent")
	}
	if as, _ := s.Store.Actions(""); len(as) != 0 {
		t.Fatal("registration started a turn")
	}
	limited := *j.Goal
	limited.Status = "usageLimited"
	thread := cr.Thread{ID: j.ThreadID, Turns: []cr.Turn{{ID: "limited-turn", Status: "failed", Error: &cr.TurnError{CodexErrorInfo: json.RawMessage(`"usageLimitExceeded"`)}}}}
	s.applyRegisteredObservation(j.ID, thread, &limited, true)
	got, _ = s.Store.Job(j.ID)
	if !got.Registration.HandoffDue || got.State != "external_writer" || !cr.WriterBusy(j.Store, j.ThreadID) {
		t.Fatal("foreign writer was not preserved")
	}
	if as, _ := s.Store.Actions(""); len(as) != 0 {
		t.Fatal("foreign active writer received takeover action")
	}
	release()
	s.applyRegisteredObservation(j.ID, thread, &limited, false)
	got, _ = s.Store.Job(j.ID)
	as, _ := s.Store.Actions("pending")
	if got.State != "quota_wait" || len(as) != 1 || as[0].Kind != "switch_account" || got.Goal.TokensUsed != 7654 || *got.Goal.TokenBudget != budget {
		t.Fatal("same-chat handoff was not queued after release")
	}
	s.applyRegisteredObservation(j.ID, thread, &limited, false)
	if as, _ = s.Store.Actions("pending"); len(as) != 1 {
		t.Fatal("duplicate handoff")
	}
}

func TestRegistrationPauseBudgetCompletionAndManualHandoff(t *testing.T) {
	for _, state := range []string{"paused", "budgetLimited", "blocked", "complete"} {
		t.Run(state, func(t *testing.T) {
			s := fixture(t)
			j := jobs.Job{ID: jobs.ID(), ThreadID: jobs.ID(), Store: s.Config.Runtime.SharedCodexHome, Account: "account-work", Kind: "goal", State: "observed", Goal: &jobs.Goal{Status: state}}
			s.Store.PutJob(j)
			got, e := s.registerObserved(j, "")
			if e != nil {
				t.Fatal(e)
			}
			s.applyRegisteredObservation(j.ID, cr.Thread{}, j.Goal, false)
			got, _ = s.Store.Job(j.ID)
			if as, _ := s.Store.Actions(""); len(as) > 0 || got.State == "queued" {
				t.Fatal("registration bypassed terminal goal state")
			}
		})
	}
	s := fixture(t)
	yes := true
	s.Reader = fakeReader{"account-secondary": {Authenticated: true, Ordinary: &yes}}
	j := jobs.Job{ID: jobs.ID(), ThreadID: jobs.ID(), Store: s.Config.Runtime.SharedCodexHome, Account: "account-work", Kind: "chat", State: "observed"}
	s.Store.PutJob(j)
	release := foreignWriter(t, j.Store, j.ThreadID)
	defer release()
	j, e := s.registerObserved(j, "account-work")
	if e != nil {
		t.Fatal(e)
	}
	a, e := s.requestAccountChange(JobArgs{ID: j.ID, Account: "account-secondary", ExpectedAccount: "account-work", OperationID: jobs.ID()})
	if e != nil {
		t.Fatal(e)
	}
	s.execute(a)
	got, _ := s.Store.Job(j.ID)
	as, _ := s.Store.Actions("pending")
	if got.Account != "account-work" || got.TargetAccount != "account-secondary" || len(as) != 1 || !cr.WriterBusy(j.Store, j.ThreadID) {
		t.Fatal("manual switch did not wait safely")
	}
	if _, e = s.unregisterThread(s.ctx, j.ID); e != nil {
		t.Fatal(e)
	}
	got, _ = s.Store.Job(j.ID)
	as, _ = s.Store.Actions("pending")
	if got.Managed || got.TargetAccount != "" || len(as) != 0 || !cr.WriterBusy(j.Store, j.ThreadID) {
		t.Fatal("unregister affected original writer")
	}
}
