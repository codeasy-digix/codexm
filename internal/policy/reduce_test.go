package policy

import (
	"encoding/json"
	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"testing"
	"time"
)

func event(j jobs.Job, method, payload string) (jobs.Job, []jobs.Action) {
	return Reduce(j, jobs.Event{Key: "e", RunID: "run", At: time.Now()}, cr.Message{Method: method, Params: json.RawMessage(payload)})
}
func TestOrdinaryChatDoesNotAutoContinue(t *testing.T) {
	j := jobs.Job{ID: "j", Kind: "chat", RunID: "run", ThreadID: "t", State: "running", TurnID: "turn"}
	j, as := event(j, "turn/completed", `{"threadId":"t","turn":{"id":"turn","status":"completed"}}`)
	if j.State != "idle" || len(as) != 0 {
		t.Fatalf("chat auto-continued: %+v %+v", j, as)
	}
}
func TestOldRunAndTurnCannotStopNewExecution(t *testing.T) {
	j := jobs.Job{Kind: "goal", RunID: "run", ThreadID: "t", TurnID: "current", State: "running"}
	x, as := Reduce(j, jobs.Event{RunID: "old"}, cr.Message{Method: "error", Params: json.RawMessage(`{"error":{"codexErrorInfo":"usageLimitExceeded"}}`)})
	if x.State != "running" || len(as) != 0 {
		t.Fatal("old run affected current")
	}
	x, as = event(j, "turn/completed", `{"threadId":"t","turn":{"id":"old","status":"interrupted"}}`)
	if x.Paused || x.TurnID != "current" || len(as) != 0 {
		t.Fatal("old turn interrupted current")
	}
}
func TestUsageLimitBudgetLimitAndRetryAreDistinct(t *testing.T) {
	for _, tt := range []struct{ kind, state, action string }{{"usageLimitExceeded", "quota_wait", "switch_account"}, {"sessionBudgetExceeded", "budget_limited", ""}, {"rateLimitExceeded", "retry_wait", "retry"}, {"unauthorized", "auth_required", ""}} {
		j, as := event(jobs.Job{ID: "j", RunID: "run", State: "running"}, "error", `{"willRetry":false,"error":{"message":"failed","codexErrorInfo":"`+tt.kind+`"}}`)
		if j.State != tt.state {
			t.Fatal(tt.kind, j.State)
		}
		if tt.action == "" && len(as) != 0 || tt.action != "" && (len(as) != 1 || as[0].Kind != tt.action) {
			t.Fatal(tt.kind, as)
		}
	}
}
func TestGoalCompletionAndHumanPause(t *testing.T) {
	j, as := event(jobs.Job{Kind: "goal", RunID: "run"}, "thread/goal/updated", `{"goal":{"status":"complete","tokensUsed":200}}`)
	if j.State != "complete" || len(as) != 1 || as[0].Kind != "cleanup" {
		t.Fatal("goal completion not handled")
	}
	j, as = event(jobs.Job{Kind: "goal", RunID: "run"}, "thread/goal/updated", `{"goal":{"status":"paused"}}`)
	if !j.Paused || len(as) != 0 {
		t.Fatal("human pause overridden")
	}
}
