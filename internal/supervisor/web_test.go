package supervisor

import (
	"context"
	"encoding/json"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"strings"
	"sync"
	"testing"
)

func TestWebSendIsDurableAndNeverRepeated(t *testing.T) {
	s := fixture(t)
	j := jobs.Job{ID: "j", Kind: "chat", Managed: true, State: "idle"}
	if err := s.Store.PutJob(j); err != nil {
		t.Fatal(err)
	}
	request := WebOperation{ID: "same-operation-id", Command: "send", Args: mustJSON(JobArgs{ID: j.ID, Text: "test instruction"})}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			_, err := s.webOperation(context.Background(), mustJSON(request))
			if err != nil && !strings.Contains(err.Error(), "outcome_unknown") {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	actions, err := s.Store.Actions("")
	if err != nil || len(actions) != 1 {
		t.Fatal("duplicate send", len(actions), err)
	}
	result, err := s.webOperation(context.Background(), mustJSON(request))
	if err != nil {
		t.Fatal(err)
	}
	var action jobs.Action
	if err = json.Unmarshal(result.(json.RawMessage), &action); err != nil || action.ID != actions[0].ID {
		t.Fatal(err)
	}
	request.Args = mustJSON(JobArgs{ID: j.ID, Text: "different"})
	if _, err = s.webOperation(context.Background(), mustJSON(request)); err == nil || !strings.Contains(err.Error(), "operation_conflict") {
		t.Fatal(err)
	}
}
func TestWebStaleApprovalRequiresRunAndUncertainNeverReplays(t *testing.T) {
	s := fixture(t)
	request := WebOperation{ID: "approval-operation", Command: "reply", Args: mustJSON(JobArgs{ID: "j", RequestID: json.RawMessage(`1`), Result: json.RawMessage(`{}`)})}
	if _, err := s.webOperation(context.Background(), mustJSON(request)); err == nil {
		t.Fatal("missing expected run accepted")
	}
	request.Command = "shutdown"
	if _, err := s.webOperation(context.Background(), mustJSON(request)); err == nil {
		t.Fatal("arbitrary command exposed")
	}
}

func TestWebAccountChangeRejectsStaleRun(t *testing.T) {
	s := fixture(t)
	j := jobs.Job{ID: "j", Kind: "chat", Managed: true, Account: "account-work", RunID: "new-run", State: "paused"}
	if err := s.Store.PutJob(j); err != nil {
		t.Fatal(err)
	}
	request := WebOperation{ID: "stale-account-operation", Command: "switch-account", Args: mustJSON(JobArgs{ID: j.ID, RunID: "old-run", Account: "account-secondary"})}
	if _, err := s.webOperation(context.Background(), mustJSON(request)); err == nil || !strings.Contains(err.Error(), "execution changed") {
		t.Fatal("stale run accepted", err)
	}
	actions, err := s.Store.Actions("")
	if err != nil || len(actions) != 0 {
		t.Fatal("stale request created work", actions, err)
	}
	request.ID = "stale-empty-run-operation"
	request.Args = json.RawMessage(`{"id":"j","account":"account-secondary","run_id":""}`)
	if _, err := s.webOperation(context.Background(), mustJSON(request)); err == nil || !strings.Contains(err.Error(), "execution changed") {
		t.Fatal("empty expected run ignored", err)
	}
	j.RunID = ""
	if err := s.Store.PutJob(j); err != nil {
		t.Fatal(err)
	}
	request.ID = "registered-no-run-operation"
	if _, err := s.webOperation(context.Background(), mustJSON(request)); err != nil {
		t.Fatal("metadata-only registered account change refused", err)
	}
}

func TestDispatchSurvivesLostOperationResultWithoutReplay(t *testing.T) {
	s := fixture(t)
	j := jobs.Job{ID: "uncertain-job", Kind: "chat", Managed: true, State: "idle"}
	if err := s.Store.PutJob(j); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.DB.Exec(`CREATE TRIGGER fail_operation_result BEFORE UPDATE ON operations WHEN NEW.state IN ('done','failed') BEGIN SELECT RAISE(ABORT,'simulated disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	request := WebOperation{ID: "lost-result-operation", Command: "send", Args: mustJSON(JobArgs{ID: j.ID, Text: "one instruction"})}
	if _, err := s.webOperation(context.Background(), mustJSON(request)); err == nil || !strings.Contains(err.Error(), "outcome_unknown") {
		t.Fatal(err)
	}
	if _, err := s.Store.DB.Exec("DROP TRIGGER fail_operation_result"); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.RecoverOperations(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.webOperation(context.Background(), mustJSON(request)); err == nil || !strings.Contains(err.Error(), "outcome_unknown") {
		t.Fatal("uncertain operation replayed", err)
	}
	actions, err := s.Store.Actions("")
	if err != nil || len(actions) != 1 {
		t.Fatal("dispatch repeated after result loss", len(actions), err)
	}
}

func TestWebGoalEditPreservesLimitedStateUnlessExplicitlyChanged(t *testing.T) {
	s := fixture(t)
	budget := int64(30000)
	j := jobs.Job{ID: "goal-job", Managed: true, Kind: "goal", State: "goal_wait", Goal: &jobs.Goal{Objective: "existing", Status: "budgetLimited", TokenBudget: &budget, TokensUsed: 30001, TimeUsedSeconds: 70}}
	if err := s.Store.PutJob(j); err != nil {
		t.Fatal(err)
	}
	_, err := s.Handle(context.Background(), Call{Command: "goal", Args: mustJSON(JobArgs{ID: j.ID, Text: "edited objective"})})
	if err != nil {
		t.Fatal(err)
	}
	actions, _ := s.Store.Actions("")
	if len(actions) != 1 {
		t.Fatal(len(actions))
	}
	var p map[string]any
	if err = json.Unmarshal(actions[0].Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p["status"] != nil || p["tokenBudget"] != nil {
		t.Fatal("edit reset intent or budget", p)
	}
	got, _ := s.Store.Job(j.ID)
	if got.Goal.TokensUsed != 30001 || got.Goal.TimeUsedSeconds != 70 || *got.Goal.TokenBudget != budget {
		t.Fatal("goal accounting changed")
	}
	negative := int64(-1)
	if _, err = s.Handle(context.Background(), Call{Command: "goal", Args: mustJSON(JobArgs{ID: j.ID, Text: "objective", TokenBudget: &negative})}); err == nil {
		t.Fatal("negative goal budget accepted")
	}
}

func TestMissingNativeGoalCannotResetHistoricalBudget(t *testing.T) {
	budget := int64(10)
	j := jobs.Job{ThreadID: "existing", Goal: &jobs.Goal{Objective: "saved", Status: "budgetLimited", TokensUsed: 12, TokenBudget: &budget}}
	if _, err := initialGoalParams(j, true); err == nil {
		t.Fatal("missing native goal recreated with reset accounting")
	}
	j.Goal = &jobs.Goal{Objective: "explicit new goal", Status: "active", TokenBudget: &budget}
	p, err := initialGoalParams(j, false)
	if err != nil || p["tokenBudget"] != &budget || p["status"] != "active" {
		t.Fatal("new goal lost explicit budget", p, err)
	}
}
