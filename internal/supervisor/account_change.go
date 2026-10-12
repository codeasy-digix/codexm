package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/policy"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

type accountChange struct {
	Account    string `json:"account"`
	From       string `json:"from"`
	Prepared   bool   `json:"prepared"`
	Continue   bool   `json:"continue"`
	HadRuntime bool   `json:"had_runtime"`
	PriorState string `json:"prior_state"`
}

// Selection is a durable, idempotent action for one job. Defaults, other jobs and
// manager credentials are unaffected. Availability is checked before interruption.
func (s *Server) requestAccountChange(a JobArgs) (jobs.Action, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !config.Nickname.MatchString(a.Account) || !slices.Contains(s.workerConfig().WorkerAccounts.Priority, a.Account) {
		return jobs.Action{}, errors.New("select an account from the configured worker priority list")
	}
	if a.OperationID != "" && !config.Nickname.MatchString(a.OperationID) {
		return jobs.Action{}, errors.New("invalid operation ID")
	}
	as, e := s.Store.Actions("")
	if e != nil {
		return jobs.Action{}, e
	}
	for _, old := range as {
		if a.OperationID != "" && old.ID == a.OperationID {
			var p accountChange
			if json.Unmarshal(old.Payload, &p) != nil || old.Kind != "change_account" || old.JobID != a.ID || p.Account != a.Account {
				return jobs.Action{}, errors.New("operation ID already used by another command")
			}
			return old, nil
		}
	}
	j, e := s.Store.Job(a.ID)
	if e != nil {
		return jobs.Action{}, e
	}
	if !j.Managed || j.Kind == "group" {
		return jobs.Action{}, errors.New("select a managed conversation; use manage first")
	}
	if a.ExpectedAccount != "" && a.ExpectedAccount != j.Account {
		return jobs.Action{}, errors.New("the session account changed; refresh before selecting again")
	}
	if j.State == "external_writer" && j.Registration == nil {
		return jobs.Action{}, errForeignWriter
	}
	if j.TargetAccount != "" {
		for _, old := range as {
			if old.JobID == j.ID && old.Kind == "change_account" && (old.State == "pending" || old.State == "executing" || old.State == "outcome_unknown") && j.TargetAccount == a.Account {
				return old, nil
			}
		}
		return jobs.Action{}, errors.New("an account change is already in progress")
	}
	action := policy.Action("change_account", j, accountChange{Account: a.Account, From: j.Account})
	if a.OperationID != "" {
		action.ID = a.OperationID
	}
	if j.Account == a.Account {
		action.State = "done"
	} else {
		j.TargetAccount = a.Account
	}
	return action, s.persist(j, action)
}

func (s *Server) checkSelectedAccount(ctx context.Context, j jobs.Job, name string) error {
	c := s.workerConfig()
	if !slices.Contains(c.WorkerAccounts.Priority, name) {
		return errors.New("selected account is no longer in the worker priority list")
	}
	if name == c.WorkerAccounts.FallbackAccount {
		if _, e := s.refreshAccounts(ctx); e != nil {
			return e
		}
	} else {
		if e := s.recordAccount(s.Reader.Read(ctx, name)); e != nil {
			return e
		}
	}
	snapshots, blocked, e := s.snapshots()
	if e != nil {
		return e
	}
	snap := snapshots[name]
	state := snap.State(j.Model)
	if state != "available" && (state != "candidate" || blocked[name]) {
		return fmt.Errorf("account %s is %s; the current account was preserved until verification", name, state)
	}
	if name == c.WorkerAccounts.FallbackAccount && c.WorkerAccounts.FallbackRequiresAllPrimaryExhausted {
		choice, _ := accounts.Choose(c, snapshots, j.Model, blocked)
		if choice != name {
			return errors.New("fallback account requires every primary account to be confirmed exhausted")
		}
	}
	return nil
}

func mayContinue(j jobs.Job) bool {
	if j.Paused || jobs.Terminal(j.State) || j.State == "blocked" || j.State == "budget_limited" {
		return false
	}
	if j.Kind == "goal" && j.Goal != nil {
		return j.Goal.Status == "active" || j.Goal.Status == "usageLimited"
	}
	return j.TurnID != "" || j.ContinuationDue || j.State == "quota_wait"
}

func (s *Server) manualAccountChange(ctx context.Context, action *jobs.Action) error {
	var p accountChange
	if e := json.Unmarshal(action.Payload, &p); e != nil {
		return e
	}
	j, e := s.Store.Job(action.JobID)
	if e != nil {
		return e
	}
	if j.TargetAccount == "" && j.Account == p.Account {
		return nil
	}
	if j.TargetAccount != p.Account || j.Account != p.From && j.Account != p.Account {
		return errors.New("session account changed before this request could run")
	}
	if !p.Prepared {
		if action.RunID != j.RunID {
			return errors.New("session execution changed; refresh before selecting an account")
		}
		if cr.WriterBusy(j.Store, j.ThreadID) && !cr.HasSession(ctx, j.Session) {
			if j.Registration != nil {
				return later("registered conversation is still owned by the original Codex; waiting for writer release")
			}
			return errForeignWriter
		}
		if e = s.checkSelectedAccount(ctx, j, p.Account); e != nil {
			return e
		}
		j, e = s.Store.Job(j.ID)
		if e != nil {
			return e
		}
		if action.RunID != j.RunID || j.Account != p.From {
			return errors.New("session execution changed during account verification; refresh before retrying")
		}
		// Persist intent before interrupting: after a crash, an interrupted turn
		// cannot tell us by itself whether the user requested continuation.
		p.Prepared, p.Continue, p.HadRuntime, p.PriorState = true, mayContinue(j), j.Session != "", j.State
		action.Payload = mustJSON(p)
		if e = s.Store.PutAction(*action); e != nil {
			return e
		}
	}
	if j.Account == p.From {
		if e = s.update(j.ID, func(cur *jobs.Job) {
			if !cur.Paused && !jobs.Terminal(cur.State) && cur.State != "blocked" && cur.State != "budget_limited" {
				cur.State = "switching"
			}
		}); e != nil {
			return e
		}
		if e = s.park(ctx, j.ID, false); e != nil {
			return e
		}
		if e = s.changeAccount(j.ID, p.Account, "사용자가 선택한 세션 계정 변경"); e != nil {
			return e
		}
	}
	if e = s.update(j.ID, func(cur *jobs.Job) {
		cur.LastAccountCheck = time.Now()
		cur.LastError = ""
		if cur.State == "switching" {
			cur.State = "idle"
			if p.PriorState == "result_ready" {
				cur.State = p.PriorState
			}
		}
		cur.ContinuationDue = p.Continue && mayContinueAfterChange(*cur)
		cur.ResumeLimitedGoal = cur.ContinuationDue && cur.Goal != nil && cur.Goal.Status == "usageLimited"
	}); e != nil {
		return e
	}
	j, e = s.Store.Job(j.ID)
	if e != nil {
		return e
	}
	if mayContinueAfterChange(j) && (p.HadRuntime || p.Continue) {
		if e = s.ensureRuntime(ctx, j.ID, false); e != nil {
			return e
		}
		j, _ = s.Store.Job(j.ID)
		if p.Continue && mayContinueAfterChange(j) {
			if j.Kind == "goal" && j.Goal != nil && j.Goal.Status == "active" {
				s.enqueueOnce(j, "goal_continue", nil)
			} else if j.Kind != "goal" {
				if e = s.queueContinuation(j); e != nil {
					return e
				}
			}
		}
	}
	return s.update(j.ID, func(cur *jobs.Job) { cur.TargetAccount = "" })
}

func mayContinueAfterChange(j jobs.Job) bool {
	if j.Paused || jobs.Terminal(j.State) || j.State == "blocked" || j.State == "budget_limited" {
		return false
	}
	return j.Kind != "goal" || j.Goal == nil || j.Goal.Status == "active" || j.Goal.Status == "usageLimited"
}
