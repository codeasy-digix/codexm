package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/policy"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/store"
)

func (s *Server) reconcileLoop() {
	s.reconcile()
	t := time.NewTicker(s.Config.Interval())
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			s.reconcile()
		}
	}
}
func (s *Server) reconcile() {
	if s.isStopping() {
		return
	}
	js, e := s.Store.Jobs()
	if e != nil {
		s.fail(e)
		return
	}
	for _, j := range js {
		if !j.Managed || j.Kind == "group" {
			continue
		}
		s.mu.Lock()
		busy := s.dispatching[j.ID]
		if !busy {
			s.dispatching[j.ID] = true
		}
		s.mu.Unlock()
		if busy {
			continue
		}
		s.reconcileJob(j)
		s.mu.Lock()
		delete(s.dispatching, j.ID)
		s.mu.Unlock()
	}
	s.reconcileActions()
	s.aggregateGroups()
	if s.Config.Supervisor.ExitWhenNoClientsAndNoPendingWork && time.Since(s.started) > 30*time.Second && !s.Store.HasPending() {
		js, e = s.Store.Jobs()
		if e != nil {
			s.fail(e)
			return
		}
		work := false
		for _, j := range js {
			if !j.Managed {
				continue
			}
			switch j.State {
			case "idle", "complete", "stopped", "archived", "result_ready", "paused", "observed":
			default:
				work = true
			}
		}
		s.mu.Lock()
		s.liveViewersLocked()
		viewers := len(s.viewers)
		s.mu.Unlock()
		if !work && viewers == 0 {
			for _, j := range js {
				if j.Session != "" {
					ctx, c := context.WithTimeout(s.ctx, 20*time.Second)
					e = s.park(ctx, j.ID, false)
					c()
					if e != nil {
						return
					}
				}
			}
			s.cancel()
		}
	}
}
func (s *Server) reconcileJob(j jobs.Job) {
	ctx, cancel := context.WithTimeout(s.ctx, 40*time.Second)
	defer cancel()
	c := s.client(j.ID)
	if c != nil {
		select {
		case <-c.Done:
			c = nil
		default:
		}
	}
	if c == nil && j.Session != "" && cr.HasSession(ctx, j.Session) {
		if e := s.connect(ctx, j, false); e != nil {
			_ = s.update(j.ID, func(j *jobs.Job) { j.LastError = cr.SafeText(e.Error()) })
			return
		}
		c = s.client(j.ID)
	}
	if c != nil && j.ThreadID != "" {
		s.sampleUsage(ctx, j, c, false)
		t, _, e := c.Recent(ctx, j.ThreadID, 1, "")
		if e != nil {
			return
		}
		g, e := c.Goal(ctx, j.ThreadID)
		if e != nil {
			return
		}
		s.mu.Lock()
		cur, e := s.Store.Job(j.ID)
		if e != nil {
			s.mu.Unlock()
			return
		}
		if cur.RunID != j.RunID {
			s.mu.Unlock()
			return
		}
		cur.LastObserved = time.Now()
		cur.Goal = g
		as := []jobs.Action{}
		if t.Status.Type == "active" {
			if len(t.Turns) > 0 {
				last := t.Turns[len(t.Turns)-1]
				if last.Status == "inProgress" {
					cur.TurnID = last.ID
				}
			}
			if !cur.Paused && !jobs.Terminal(cur.State) && cur.State != "switching" {
				cur.State = "running"
				for _, f := range t.Status.ActiveFlags {
					if f == "waitingOnUserInput" {
						cur.State = "waiting_on_user"
					}
					if f == "waitingOnApproval" {
						cur.State = "waiting_on_approval"
					}
				}
			}
		} else if cur.TurnID != "" && len(t.Turns) > 0 {
			last := t.Turns[len(t.Turns)-1]
			if last.ID == cur.TurnID && last.Status != "inProgress" {
				b, _ := json.Marshal(map[string]any{"threadId": j.ThreadID, "turn": last})
				ev := jobs.Event{Key: "reconcile:" + j.RunID + ":" + last.ID + ":" + last.Status, JobID: j.ID, RunID: j.RunID, Method: "turn/completed", Payload: b, At: time.Now()}
				cur, as = policy.Reduce(cur, ev, cr.Message{Method: ev.Method, Params: b})
				_ = s.Store.Commit(cur, &ev, as)
				as = nil
			}
		}
		if g != nil && cur.Kind == "goal" {
			switch g.Status {
			case "complete":
				if cur.State != "complete" {
					cur.State = "complete"
					as = append(as, policy.Action("cleanup", cur, nil))
				}
			case "paused":
				if cur.State != "switching" {
					cur.Paused = true
					cur.State = "paused"
				}
			case "budgetLimited":
				cur.State = "budget_limited"
			case "blocked":
				cur.State = "blocked"
			case "usageLimited":
				if !cur.Paused && cur.State != "quota_wait" {
					cur.State = "quota_wait"
					as = append(as, policy.Action("switch_account", cur, nil))
				}
			case "active":
				if !cur.Paused && cur.TurnID == "" && cur.ContinuationDue {
					cur.State = "goal_wait"
					cur.ContinuationDue = false
					as = append(as, policy.Action("goal_continue", cur, nil))
				}
			}
		}
		if e = s.persist(cur, as...); e != nil {
			s.unhealthy = e.Error()
		}
		s.mu.Unlock()
		j = cur
	} else if j.Session != "" && !cr.HasSession(ctx, j.Session) {
		if j.RunID != "" {
			if e := s.Store.EndRun(j.RunID, "runtime_lost", time.Now()); e != nil && !store.NotFound(e) {
				s.fail(e)
				return
			}
		}
		// A lost runtime is safe to reload, but the last dispatched user input is
		// never replayed here. That action remains outcome_unknown until inspected.
		_ = s.update(j.ID, func(j *jobs.Job) {
			j.Session = ""
			j.Socket = ""
			j.TurnID = ""
			if !j.Paused && !jobs.Terminal(j.State) {
				j.State = "unknown"
			}
		})
		if !j.Paused && !jobs.Terminal(j.State) {
			s.enqueueOnce(j, "start", map[string]string{"origin": "automatic"})
		}
	}
	if j.Paused || jobs.Terminal(j.State) {
		if c != nil && j.TurnID == "" && !s.hasJobViewer(j.ID) {
			s.enqueueOnce(j, "cleanup", nil)
		}
		return
	}
	if j.Session == "" && (j.State == "queued" || j.State == "starting" || j.State == "switching" || j.State == "unknown") {
		s.enqueueOnce(j, "start", map[string]string{"origin": "automatic"})
	}
	if j.ContinuationDue && j.Kind != "goal" && j.TurnID == "" && c != nil {
		_ = s.queueContinuation(j)
	}
	if j.State == "quota_wait" {
		s.enqueueOnce(j, "switch_account", nil)
	}
	if j.Account == s.Config.WorkerAccounts.FallbackAccount && time.Since(j.LastAccountCheck) >= time.Duration(s.Config.WorkerAccounts.PreferredRecheckSeconds)*time.Second {
		if _, e := s.refreshAccounts(ctx); e != nil {
			return
		}
		m, blocked, e := s.snapshots()
		if e != nil {
			return
		}
		choice, _ := accounts.Choose(s.Config, m, j.Model, blocked)
		_ = s.update(j.ID, func(j *jobs.Job) { j.LastAccountCheck = time.Now() })
		if choice != "" && choice != s.Config.WorkerAccounts.FallbackAccount {
			s.enqueueOnce(j, "prefer_account", map[string]string{"account": choice})
		}
	}
}
func (s *Server) enqueueOnce(j jobs.Job, kind string, p any) {
	as, _ := s.Store.Actions("")
	for _, a := range as {
		if a.JobID == j.ID && a.Kind == kind && (a.State == "pending" || a.State == "executing" || a.State == "outcome_unknown") {
			return
		}
	}
	if e := s.Store.PutAction(policy.Action(kind, j, p)); e != nil {
		s.fail(e)
	}
}
func (s *Server) reconcileActions() {
	as, e := s.Store.Actions("outcome_unknown")
	if e != nil {
		s.fail(e)
		return
	}
	for _, a := range as {
		j, e := s.Store.Job(a.JobID)
		if e != nil {
			continue
		}
		switch a.Kind {
		case "cleanup", "pause_execution":
			if j.Session == "" || !cr.HasSession(s.ctx, j.Session) {
				s.finishAction(a, "done", nil)
			} else if j.Paused || jobs.Terminal(j.State) {
				a.State = "pending"
				_ = s.Store.PutAction(a)
			}
			continue
		case "switch_account", "prefer_account":
			if a.RunID != j.RunID || j.State == "queued" || j.State == "idle" || jobs.Busy(j.State) && j.State != "switching" {
				s.finishAction(a, "done", nil)
			} else if j.State == "switching" || j.State == "quota_wait" {
				a.State = "pending"
				_ = s.Store.PutAction(a)
			}
			continue
		case "set_goal":
			var p struct {
				Objective string `json:"objective"`
			}
			_ = json.Unmarshal(a.Payload, &p)
			if j.Goal != nil && j.Goal.Objective == p.Objective {
				s.finishAction(a, "done", nil)
			}
			continue
		}
		if (a.Kind == "start" || a.Kind == "load_view") && j.ThreadID != "" && s.client(j.ID) != nil {
			s.finishAction(a, "done", nil)
			continue
		}
		if a.Kind != "send_input" {
			continue
		}
		var in jobs.Input
		if json.Unmarshal(a.Payload, &in) != nil || in.MessageID == "" {
			continue
		}
		c := s.client(j.ID)
		if c == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		t, _, e := c.Recent(ctx, j.ThreadID, 20, "")
		cancel()
		if e != nil {
			continue
		}
		for _, turn := range t.Turns {
			for _, item := range turn.Items {
				if item.ID == in.MessageID && item.Type == "userMessage" {
					s.finishAction(a, "done", nil)
				}
			}
		}
	}
}
func (s *Server) aggregateGroups() {
	js, e := s.Store.Jobs()
	if e != nil {
		return
	}
	for _, p := range js {
		if p.Kind != "group" || p.Paused || jobs.Terminal(p.State) {
			continue
		}
		count, complete := 0, 0
		for _, j := range js {
			if j.ParentID == p.ID {
				count++
				if j.State == "complete" {
					complete++
				}
			}
		}
		if count > 0 && count == complete {
			_ = s.update(p.ID, func(j *jobs.Job) {
				j.State = "result_ready"
				j.LastOutput = fmt.Sprintf("%d child jobs completed", count)
			})
			if len(p.CompletionCommand) > 0 {
				s.enqueueOnce(p, "check_completion", nil)
			}
		}
	}
}
