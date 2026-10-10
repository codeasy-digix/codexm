package supervisor

import (
	"context"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/policy"
)

func TestOpenFinishedOrPausedGoalQueuesOnlyMetadataLoad(t *testing.T) {
	for _, state := range []string{"complete", "paused"} {
		t.Run(state, func(t *testing.T) {
			s := fixture(t)
			budget := int64(40000)
			j := jobs.Job{ID: jobs.ID(), State: state, Kind: "goal", Managed: true, Paused: state == "paused", Goal: &jobs.Goal{Status: state, TokensUsed: 1234, TokenBudget: &budget}}
			s.Store.PutJob(j)
			for _, client := range []string{"first", "second"} {
				if _, e := s.Handle(context.Background(), Call{Command: "view", Args: mustJSON(JobArgs{ID: j.ID, ClientID: client})}); e != nil {
					t.Fatal(e)
				}
			}
			got, _ := s.Store.Job(j.ID)
			as, _ := s.Store.Actions("")
			calls, _, _ := s.Store.DecisionCounts("")
			if got.State != state || got.Paused != j.Paused || got.Goal.Status != state || got.Goal.TokensUsed != 1234 || *got.Goal.TokenBudget != budget || len(as) != 1 || as[0].Kind != "load_view" || calls != 0 {
				t.Fatal("opening a window resumed or mutated work", got.State, as, calls)
			}
		})
	}
}

func TestCompletedWorkerCleanupWaitsForTerminalAndExpiresDeadClient(t *testing.T) {
	s := fixture(t)
	j := jobs.Job{ID: jobs.ID(), State: "complete", Managed: true, Kind: "goal"}
	s.Store.PutJob(j)
	s.viewers["terminal"] = viewer{At: time.Now(), JobID: j.ID}
	if !s.hasJobViewer(j.ID) {
		t.Fatal("viewer missing")
	}
	a := policy.Action("cleanup", j, nil)
	s.Store.PutAction(a)
	s.execute(a)
	as, _ := s.Store.Actions("done")
	if len(as) != 1 {
		t.Fatal("cleanup should defer runtime parking while a viewer is attached")
	}
	s.viewers["terminal"] = viewer{At: time.Now().Add(-16 * time.Second), JobID: j.ID}
	if s.hasJobViewer(j.ID) || len(s.viewers) != 0 {
		t.Fatal("dead terminal kept a completed worker alive")
	}
}
