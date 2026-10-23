package accounts

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/store"
)

// DetachRemovedAssignments is shared by the online dispatch gate and the
// offline exclusive lease. It never launches work or touches native Codex data.
func DetachRemovedAssignments(c config.Config, st *store.Store, busy map[string]bool) (detached, pending int, err error) {
	if len(c.AccountRegistry.Removed) == 0 {
		return
	}
	current, err := st.Jobs()
	if err != nil {
		return 0, 0, err
	}
	actions, err := st.Actions("")
	if err != nil {
		return 0, 0, err
	}
	var updates []jobs.Job
	var changes []jobs.WorkChange
	var updatedActions []jobs.Action
	for _, j := range current {
		removed := j.Account != "" && slices.Contains(c.AccountRegistry.Removed, j.Account)
		targetRemoved := j.TargetAccount != "" && slices.Contains(c.AccountRegistry.Removed, j.TargetAccount)
		if !removed && !targetRemoved && j.AccountReassignment == nil {
			continue
		}
		// A finished run's account is history, not a future assignment.
		if removed && jobs.Terminal(j.State) && j.Session == "" && !busy[j.ID] && !targetRemoved && j.AccountReassignment == nil {
			continue
		}
		j.AutoAccount = true
		if removed || targetRemoved {
			j.TargetAccount = ""
		}
		if (removed || targetRemoved && busy[j.ID]) && j.AccountReassignment == nil {
			if j.Session != "" || busy[j.ID] {
				j.AccountReassignment = &jobs.AccountReassignment{From: j.Account, Continue: continuing(j), HadRuntime: j.Session != "", PriorState: j.State}
			} else {
				changes = append(changes, jobs.WorkChange{ID: jobs.ID(), JobID: j.ID, ThreadID: j.ThreadID, From: j.Account, Reason: "계정 등록 삭제: 다음 실행 때 우선순위로 다시 선택", At: time.Now()})
				j.Account = ""
				detached++
			}
		}
		handoffQueued := false
		for _, a := range actions {
			if a.JobID != j.ID {
				continue
			}
			if a.Kind == "change_account" && a.State == "executing" && j.AccountReassignment != nil {
				var intent struct {
					Continue   bool   `json:"continue"`
					HadRuntime bool   `json:"had_runtime"`
					PriorState string `json:"prior_state"`
				}
				if json.Unmarshal(a.Payload, &intent) == nil {
					j.AccountReassignment.Continue = j.AccountReassignment.Continue || intent.Continue
					j.AccountReassignment.HadRuntime = j.AccountReassignment.HadRuntime || intent.HadRuntime
					if intent.PriorState != "" {
						j.AccountReassignment.PriorState = intent.PriorState
					}
				}
			}
			if a.Kind == "reassign_account" && (a.State == "pending" || a.State == "executing" || a.State == "outcome_unknown") {
				handoffQueued = true
			}
			if a.Kind == "change_account" && (a.State == "pending" || a.State == "outcome_unknown") {
				a.State, a.Error = "cancelled", "계정 등록 삭제로 고정 선택을 해제했습니다. 다음 실행 때 우선순위를 확인합니다."
				updatedActions = append(updatedActions, a)
			}
		}
		if j.AccountReassignment != nil {
			pending++
			if !handoffQueued {
				updatedActions = append(updatedActions, jobs.Action{ID: jobs.ID(), JobID: j.ID, Kind: "reassign_account", State: "pending", Payload: json.RawMessage(`{}`), CreatedAt: time.Now()})
			}
		}
		j.UpdatedAt = time.Now()
		updates = append(updates, j)
	}
	if len(updates) == 0 {
		return detached, pending, nil
	}
	err = st.CommitAccountRemoval(updates, changes, updatedActions)
	return
}

func continuing(j jobs.Job) bool {
	if j.Paused || jobs.Terminal(j.State) || j.State == "blocked" || j.State == "budget_limited" {
		return false
	}
	if j.Kind == "goal" && j.Goal != nil {
		return j.Goal.Status == "active" || j.Goal.Status == "usageLimited"
	}
	return j.TurnID != "" || j.ContinuationDue || j.State == "quota_wait"
}
