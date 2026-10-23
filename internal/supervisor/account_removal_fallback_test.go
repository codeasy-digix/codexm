package supervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/policy"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"golang.org/x/sys/unix"
)

func removeFixtureAccount(t *testing.T, s *Server, name string) accounts.Removal {
	t.Helper()
	if err := s.Config.Save(); err != nil {
		t.Fatal(err)
	}
	_, rev, err := config.Current(s.Config.Path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.removeAccount(accounts.RemoveArgs{Account: name, Revision: rev})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestRemovalDetachesInactiveAssignmentsAndPreservesExecutionIntent(t *testing.T) {
	for _, state := range []string{"idle", "paused", "auth_required", "quota_wait", "queued", "error", "external_writer", "complete", "stopped"} {
		t.Run(state, func(t *testing.T) {
			s := fixture(t)
			budget := int64(9000)
			j := jobs.Job{ID: jobs.ID(), ThreadID: jobs.ID(), Account: "account-work", InitialAccount: "account-work", Kind: "goal", Managed: true, State: state, Paused: state == "paused" || state == "stopped", Goal: &jobs.Goal{Objective: "synthetic objective", Status: "paused", TokenBudget: &budget, TokensUsed: 123}, LastInput: "synthetic input", Model: "fixture-model", Effort: "low", Registration: &jobs.Registration{OriginAccount: "account-work"}}
			if err := s.Store.PutJob(j); err != nil {
				t.Fatal(err)
			}
			result := removeFixtureAccount(t, s, "account-work")
			got, _ := s.Store.Job(j.ID)
			want := ""
			if jobs.Terminal(state) {
				want = "account-work"
			}
			if got.Account != want || got.InitialAccount != j.InitialAccount || got.ThreadID != j.ThreadID || got.State != state || got.Paused != j.Paused || got.Goal.TokensUsed != 123 || *got.Goal.TokenBudget != budget || got.LastInput != j.LastInput || got.Model != j.Model || got.Effort != j.Effort || got.Registration.OriginAccount != "account-work" {
				t.Fatal("removal changed execution intent")
			}
			if !jobs.Terminal(state) && (!got.AutoAccount || result.DetachedJobs != 1) {
				t.Fatal("inactive assignment was retained")
			}
			as, _ := s.Store.Actions("")
			calls, _, _ := s.Store.DecisionCounts("")
			if len(as) != 0 || calls != 0 {
				t.Fatal("inactive removal started work")
			}
			latest, _ := config.Load(s.Config.Path)
			if slices.Contains(latest.WorkerAccounts.Priority, "account-work") {
				t.Fatal("removed alias selectable")
			}
		})
	}
}

func TestRemovalCancelsSelectedTargetWithoutRunningStaleQueueEntry(t *testing.T) {
	for _, removed := range []string{"account-work", "account-secondary"} {
		t.Run(removed, func(t *testing.T) {
			s := fixture(t)
			j := jobs.Job{ID: jobs.ID(), Kind: "chat", Account: "account-work", State: "paused", Paused: true, Managed: true}
			s.Store.PutJob(j)
			a, err := s.requestAccountChange(JobArgs{ID: j.ID, Account: "account-secondary"})
			if err != nil {
				t.Fatal(err)
			}
			removeFixtureAccount(t, s, removed)
			s.execute(a) // actionLoop may still hold this pre-removal snapshot.
			got, _ := s.Store.Job(j.ID)
			saved, _ := s.Store.Action(a.ID)
			if got.TargetAccount != "" || saved.State != "cancelled" || !got.Paused {
				t.Fatal("stale selected target survived removal")
			}
		})
	}
}

func TestRemovalNeverAcquiresOrKillsAnExternalWriter(t *testing.T) {
	s := fixture(t)
	j := jobs.Job{ID: jobs.ID(), ThreadID: jobs.ID(), Account: "account-work", Kind: "chat", State: "external_writer", Managed: true, Store: s.Config.Runtime.SharedCodexHome}
	dir := filepath.Join(j.Store, "thread-writer-locks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, j.ThreadID+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	s.Store.PutJob(j)
	removeFixtureAccount(t, s, "account-work")
	got, _ := s.Store.Job(j.ID)
	if got.Account != "" || got.State != "external_writer" || !cr.WriterBusy(j.Store, j.ThreadID) || got.Session != "" {
		t.Fatal("foreign writer was changed")
	}
}

func TestRemovalHandoffWaitsForQuotaAndRecoversWithoutDuplicateAudit(t *testing.T) {
	s := fixture(t)
	no := false
	s.Reader = fakeReader{}
	for _, name := range s.Config.WorkerAccounts.Priority {
		s.Reader.(fakeReader)[name] = accounts.Snapshot{Authenticated: true, Ordinary: &no}
	}
	j := jobs.Job{ID: jobs.ID(), Account: "account-work", InitialAccount: "account-work", Kind: "chat", State: "running", Managed: true, TurnID: "interrupted-turn", Session: "process:" + filepath.Join(t.TempDir(), "ended.process.json")}
	s.Store.PutJob(j)
	result := removeFixtureAccount(t, s, "account-work")
	if result.PendingHandoffs != 1 {
		t.Fatal("owned worker handoff missing")
	}
	as, _ := s.Store.Actions("pending")
	if len(as) != 1 {
		t.Fatal("missing handoff")
	}
	s.execute(as[0])
	got, _ := s.Store.Job(j.ID)
	a, _ := s.Store.Action(as[0].ID)
	if got.Account != "" || got.AccountReassignment == nil || !got.ContinuationDue || got.State != "quota_wait" || a.State != "pending" {
		t.Fatal("unavailable replacement discarded work")
	}
	a.State = "executing"
	s.Store.PutAction(a)
	s.Store.RecoverActions()
	s.reconcileActions()
	recovered, _ := s.Store.Action(a.ID)
	if recovered.State != "pending" {
		t.Fatal("handoff intent not recovered")
	}
	s.execute(recovered)
	logs, _ := s.Store.WorkLogs(j.ID, 20)
	if len(logs.Changes) != 1 {
		t.Fatal("handoff recovery duplicated detach audit")
	}
}

func TestRemovalTombstoneRepairsCleanupAfterCrash(t *testing.T) {
	s := fixture(t)
	s.Config.Save()
	j := jobs.Job{ID: jobs.ID(), Account: "account-work", InitialAccount: "account-work", Kind: "chat", State: "paused", Paused: true, Managed: true}
	s.Store.PutJob(j)
	_, rev, _ := config.Current(s.Config.Path)
	if _, err := accounts.RemoveRegistration(s.Config, accounts.RemoveArgs{Account: "account-work", Revision: rev}); err != nil {
		t.Fatal(err)
	}
	s.reconcile()
	got, _ := s.Store.Job(j.ID)
	if got.Account != "" || !got.AutoAccount || !got.Paused {
		t.Fatal("saved removal was not recovered")
	}
	s.reconcile()
	logs, _ := s.Store.WorkLogs(j.ID, 20)
	if len(logs.Changes) != 1 {
		t.Fatal("recovery repeated audit")
	}
}

type executionReader struct {
	values fakeReader
	calls  []string
}

func (r *executionReader) Read(ctx context.Context, name string) accounts.Snapshot {
	r.calls = append(r.calls, name)
	return r.values.Read(ctx, name)
}

func TestEveryExecutionReadsFreshPriorityInsteadOfSavedAccountOrQuota(t *testing.T) {
	s := fixture(t)
	s.Config.Save()
	yes, no := true, false
	r := &executionReader{values: fakeReader{"account-work": {Authenticated: true, Ordinary: &yes}, "account-secondary": {Authenticated: true, Ordinary: &yes}, "account-main": {Authenticated: true, Ordinary: &yes}, "account-backup": {Authenticated: true, Ordinary: &yes}}}
	s.Reader = r
	j := jobs.Job{ID: jobs.ID(), Account: "account-secondary", AutoAccount: false, InitialAccount: "account-secondary", State: "paused", Paused: true, Kind: "chat", Managed: true}
	s.Store.PutJob(j)
	for _, want := range []string{"account-work", "account-secondary", "account-main", "account-backup"} {
		choice, err := s.initialAccount(context.Background(), j)
		if err != nil || choice != want {
			t.Fatal("priority not refreshed", choice, want, err)
		}
		r.values[want] = accounts.Snapshot{Authenticated: true, Ordinary: &no}
	}
	if len(r.calls) != 10 {
		t.Fatal("execution reused quota cache", r.calls)
	}
	r.values["account-work"] = accounts.Snapshot{Authenticated: true} // unknown must not unlock reserve.
	r.values["account-backup"] = accounts.Snapshot{Authenticated: true, Ordinary: &yes}
	choice, err := s.initialAccount(context.Background(), j)
	var wait *postpone
	if choice != "" || !errors.As(err, &wait) {
		t.Fatal("unknown primary unlocked reserve")
	}
	got, _ := s.Store.Job(j.ID)
	if !got.Paused || got.State != "paused" {
		t.Fatal("quota check resumed paused work")
	}
}

func TestOfficialAccountRemovalParksWorkerAndNextExecutionUsesPriority(t *testing.T) {
	if os.Getenv("CODEXM_NATIVE_TEST") != "1" {
		t.Skip("isolated official runtime required")
	}
	s := fixture(t)
	s.Self = os.Getenv("CODEXM_BINARY")
	s.Config.Runtime.CodexBinary = os.Getenv("CODEXM_NATIVE_RUNTIME")
	if s.Self == "" {
		t.Fatal("CODEXM_BINARY required")
	}
	s.Config.Save()
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	r, err := cr.Resolve(ctx, s.Config)
	if err != nil {
		t.Fatal(err)
	}
	s.Resolver = r
	yes := true
	s.Reader = fakeReader{"account-work": {Authenticated: true, Ordinary: &yes}, "account-secondary": {Authenticated: true, Ordinary: &yes}, "account-main": {Authenticated: true, Ordinary: &yes}}
	j, err := s.newJob(NewArgs{Cwd: t.TempDir(), Kind: "chat", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.park(context.Background(), j.ID, false); s.cancel(); s.wg.Wait() }()
	if err = s.ensureRuntime(ctx, j.ID, false); err != nil {
		t.Fatal(err)
	}
	first, _ := s.Store.Job(j.ID)
	pending, _ := s.Store.Actions("pending")
	for _, a := range pending {
		if a.Kind == "start" {
			s.finishAction(a, "done", nil)
		}
	}
	result := removeFixtureAccount(t, s, "account-work")
	if result.PendingHandoffs != 1 {
		t.Fatal("no owned worker retirement")
	}
	pending, _ = s.Store.Actions("pending")
	for _, a := range pending {
		if a.Kind == "reassign_account" {
			s.execute(a)
		}
	}
	detached, _ := s.Store.Job(j.ID)
	if detached.Account != "" || detached.AccountReassignment != nil || detached.Session != "" || detached.ThreadID != first.ThreadID || cr.HasSession(ctx, first.Session) || cr.WriterBusy(first.Store, first.ThreadID) {
		t.Fatal("owned writer was not released")
	}
	if err = s.ensureRuntime(ctx, j.ID, false); err != nil {
		t.Fatal(err)
	}
	second, _ := s.Store.Job(j.ID)
	if second.Account != "account-secondary" || second.ThreadID != first.ThreadID || second.RunID == first.RunID {
		t.Fatal("next start did not use remaining priority on same thread")
	}
	if _, _, err = config.Update(s.Config.Path, "", func(c *config.Config) error {
		c.WorkerAccounts.Priority = []string{"account-main", "account-secondary", "account-backup"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// User resume against an already connected idle worker also rechecks priority.
	if err = s.ensureRuntime(ctx, j.ID, true); err != nil {
		t.Fatal(err)
	}
	third, _ := s.Store.Job(j.ID)
	if third.Account != "account-main" || third.ThreadID != first.ThreadID || third.RunID == second.RunID {
		t.Fatal("connected worker pinned resume to its old account")
	}
	c := s.client(j.ID)
	thread, err := c.Read(ctx, third.ThreadID, true)
	if err != nil || len(thread.Turns) != 0 {
		t.Fatal("metadata verification started a model turn", err)
	}
	calls, _, _ := s.Store.DecisionCounts("")
	if calls != 0 {
		t.Fatal("removal invoked manager model")
	}
	// Pending input is independent of retirement and retains its exact message ID.
	in := policy.Action("send_input", third, jobs.Input{Text: "synthetic pending input", MessageID: "preserved-message"})
	s.Store.PutAction(in)
	saved, _ := s.Store.Action(in.ID)
	if string(saved.Payload) != string(in.Payload) {
		t.Fatal("queued input changed")
	}
}

func TestRemovalKeepsLegacyClientsCompatibleAndDeclaresHandoffCapability(t *testing.T) {
	s := fixture(t)
	v, err := s.Handle(context.Background(), Call{Command: "ping"})
	if err != nil {
		t.Fatal(err)
	}
	caps := v.(map[string]any)
	if caps["accounts_api"] != 1 || caps["account_reassignment_api"] != 1 {
		t.Fatal("existing panels must keep working while new clients can verify handoff support")
	}
}

func TestRemovedNativeLoginCanRegisterHistoryWithoutBecomingWorkerAssignment(t *testing.T) {
	s := fixture(t)
	j := jobs.Job{ID: jobs.ID(), ThreadID: jobs.ID(), Account: "account-work", InitialAccount: "account-work", Kind: "chat", State: "complete"}
	if err := s.Store.PutJob(j); err != nil {
		t.Fatal(err)
	}
	removeFixtureAccount(t, s, "account-work")
	registered, err := s.registerObserved(j, "account-work")
	if err != nil || registered.Account != "" || registered.InitialAccount != "account-work" || !registered.AutoAccount || registered.State != "idle" || registered.Registration.OriginAccount != "account-work" {
		t.Fatal("historical/native login pinned registration", err)
	}
}

func TestRemovingOnlyWorkerPreservesQueuedInputAndGoalUntilAnotherAccountIsAvailable(t *testing.T) {
	s := fixture(t)
	s.Config.WorkerAccounts.Priority = []string{"account-work"}
	s.Config.WorkerAccounts.FallbackAccount = ""
	budget := int64(3000)
	j := jobs.Job{ID: jobs.ID(), ThreadID: jobs.ID(), Account: "account-work", InitialAccount: "account-work", Kind: "goal", State: "paused", Paused: true, Managed: true, Goal: &jobs.Goal{Objective: "synthetic", Status: "paused", TokenBudget: &budget, TokensUsed: 123}}
	in := policy.Action("send_input", j, jobs.Input{Text: "preserve this request", MessageID: "original-message"})
	if err := s.persist(j, in); err != nil {
		t.Fatal(err)
	}
	removeFixtureAccount(t, s, "account-work")
	if _, err := s.Handle(context.Background(), Call{Command: "resume-job", Args: mustJSON(JobArgs{ID: j.ID})}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Store.Job(j.ID)
	_, err := s.initialAccount(context.Background(), got)
	var wait *postpone
	if !errors.As(err, &wait) {
		t.Fatal("empty account list did not wait")
	}
	got, _ = s.Store.Job(j.ID)
	saved, _ := s.Store.Action(in.ID)
	if got.Account != "" || got.State != "quota_wait" || got.ThreadID != j.ThreadID || got.Goal.TokensUsed != 123 || *got.Goal.TokenBudget != budget || string(saved.Payload) != string(in.Payload) || saved.State != "pending" {
		t.Fatal("removing final worker lost queued work")
	}
}

func TestRemovalFinishesWhenPriorWorkerAndConnectionAreAlreadyDead(t *testing.T) {
	s := fixture(t)
	j := jobs.Job{ID: jobs.ID(), Account: "account-work", Kind: "chat", State: "idle", Managed: true, Session: "process:" + filepath.Join(t.TempDir(), "dead.process.json")}
	if err := s.Store.PutJob(j); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	close(done)
	s.clients[j.ID] = &cr.Client{Done: done}
	removeFixtureAccount(t, s, "account-work")
	as, _ := s.Store.Actions("pending")
	if len(as) != 1 {
		t.Fatal("missing retirement action")
	}
	s.execute(as[0])
	got, _ := s.Store.Job(j.ID)
	a, _ := s.Store.Action(as[0].ID)
	if got.Account != "" || got.AccountReassignment != nil || got.Session != "" || s.client(j.ID) != nil || a.State != "done" {
		t.Fatal("dead RPC transport blocked account retirement")
	}
}
