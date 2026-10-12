package supervisor

import (
	"context"
	"testing"

	"github.com/codeasy-digix/codexm/internal/jobs"
)

func TestManualAccountChangePreservesStoppedAndGoalStates(t *testing.T) {
	for _, state := range []string{"idle", "paused", "stopped", "complete", "blocked", "budget_limited", "result_ready"} {
		t.Run(state, func(t *testing.T) {
			s := fixture(t)
			yes := true
			s.Reader = fakeReader{"account-secondary": {Authenticated: true, Ordinary: &yes}}
			budget := int64(9000)
			j := jobs.Job{ID: jobs.ID(), ThreadID: jobs.ID(), Kind: "goal", Account: "account-work", InitialAccount: "account-work", Managed: true, State: state, Paused: state == "paused" || state == "stopped", Goal: &jobs.Goal{Status: "paused", TokenBudget: &budget, TokensUsed: 345}}
			if e := s.Store.PutJob(j); e != nil {
				t.Fatal(e)
			}
			request := JobArgs{ID: j.ID, Account: "account-secondary", ExpectedAccount: "account-work", OperationID: jobs.ID()}
			a, e := s.requestAccountChange(request)
			if e != nil {
				t.Fatal(e)
			}
			s.execute(a)
			got, _ := s.Store.Job(j.ID)
			if got.Account != "account-secondary" || got.TargetAccount != "" || got.ThreadID != j.ThreadID || got.Goal.TokensUsed != 345 || *got.Goal.TokenBudget != budget || got.Goal.Status != "paused" || got.State != state || got.Paused != j.Paused || got.Session != "" || s.manager() != "account-backup" {
				t.Fatalf("account selection changed execution intent: state=%s account=%s target=%s", got.State, got.Account, got.TargetAccount)
			}
			// A retried response cannot add another change, engine, turn or manager call.
			old, e := s.requestAccountChange(request)
			if e != nil || old.ID != a.ID || old.State != "done" {
				t.Fatal(old.State, e)
			}
			logs, e := s.Store.WorkLogs(j.ID, 10)
			if e != nil || len(logs.Changes) != 1 || len(logs.Runs) != 0 {
				t.Fatal("unexpected run or duplicate account change", e)
			}
			actions, _ := s.Store.Actions("")
			calls, _, _ := s.Store.DecisionCounts("")
			if len(actions) != 1 || calls != 0 {
				t.Fatal("account change generated work")
			}
		})
	}
}

func TestManualAccountChangeChecksFallbackAndUnknownBeforeTouchingWorker(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		name, target string
		primary      *bool
		allowed      bool
	}{
		{"primary available", "account-secondary", &yes, true},
		{"primary exhausted", "account-secondary", &no, false},
		{"primary unknown", "account-secondary", nil, false},
		{"fallback while primary available", "account-backup", &yes, false},
		{"fallback while primary unknown", "account-backup", nil, false},
		{"fallback after all primary exhausted", "account-backup", &no, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t)
			s.Reader = fakeReader{"account-work": {Authenticated: true, Ordinary: tc.primary}, "account-secondary": {Authenticated: true, Ordinary: tc.primary}, "account-main": {Authenticated: true, Ordinary: tc.primary}, "account-backup": {Authenticated: true, Ordinary: &yes}}
			j := jobs.Job{ID: jobs.ID(), ThreadID: jobs.ID(), Account: "account-work", State: "paused", Paused: true, Managed: true, Kind: "chat"}
			s.Store.PutJob(j)
			a, e := s.requestAccountChange(JobArgs{ID: j.ID, Account: tc.target})
			if e != nil {
				t.Fatal(e)
			}
			s.execute(a)
			got, _ := s.Store.Job(j.ID)
			want := "account-work"
			if tc.allowed {
				want = tc.target
			}
			if got.Account != want || got.State != "paused" || got.TargetAccount != "" {
				t.Fatal(got.Account, got.State, got.TargetAccount)
			}
		})
	}
}

func TestAccountChangeRecoversAfterManagerCrashWithoutRepeatingLog(t *testing.T) {
	s := fixture(t)
	yes := true
	s.Reader = fakeReader{"account-secondary": {Authenticated: true, Ordinary: &yes}}
	j := jobs.Job{ID: jobs.ID(), Account: "account-work", State: "paused", Paused: true, Managed: true, Kind: "chat"}
	s.Store.PutJob(j)
	a, e := s.requestAccountChange(JobArgs{ID: j.ID, Account: "account-secondary"})
	if e != nil {
		t.Fatal(e)
	}
	a.Payload = mustJSON(accountChange{Account: "account-secondary", From: "account-work", Prepared: true, PriorState: "paused"})
	a.State = "executing"
	s.Store.PutAction(a)
	if e = s.changeAccount(j.ID, "account-secondary", "interrupted handoff fixture"); e != nil {
		t.Fatal(e)
	}
	if e = s.Store.RecoverActions(); e != nil {
		t.Fatal(e)
	}
	s.reconcileActions()
	list, _ := s.Store.Actions("pending")
	if len(list) != 1 {
		t.Fatal("handoff was not recovered")
	}
	s.execute(list[0])
	got, _ := s.Store.Job(j.ID)
	log, _ := s.Store.WorkLogs(j.ID, 10)
	if got.Account != "account-secondary" || got.TargetAccount != "" || !got.Paused || len(log.Changes) != 1 {
		t.Fatal("handoff replay changed intent")
	}
	if _, e := s.Handle(context.Background(), Call{Command: "switch-account", Args: mustJSON(JobArgs{ID: j.ID, Account: "account-work", ExpectedAccount: "account-main"})}); e == nil {
		t.Fatal("stale selection accepted")
	}
}
