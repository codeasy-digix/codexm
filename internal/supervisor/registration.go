package supervisor

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/policy"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

// Register attaches policy to an existing conversation without acquiring its
// writer or generating a turn. A foreign writer remains in control until it exits.
func (s *Server) registerThread(ctx context.Context, a JobArgs) (jobs.Job, error) {
	if !config.Nickname.MatchString(a.ThreadID) {
		return jobs.Job{}, errors.New("valid current thread ID required")
	}
	if e := s.validateOriginAccount(a.OriginAccount); e != nil {
		return jobs.Job{}, e
	}
	all, e := s.Store.Jobs()
	if e != nil {
		return jobs.Job{}, e
	}
	for _, j := range all {
		if j.ThreadID == a.ThreadID && j.Managed {
			return j, nil
		}
	}
	list, e := catalog.Read(ctx, config.Expand(s.Config.Runtime.SharedCodexHome))
	if e != nil {
		return jobs.Job{}, e
	}
	var metadata catalog.Entry
	for _, entry := range list.Sessions {
		if entry.ThreadID == a.ThreadID {
			metadata = entry
		}
		if entry.ThreadID == a.ThreadID && entry.Source == "subagent" {
			return jobs.Job{}, errors.New("register native subagents through their parent conversation")
		}
	}
	j, e := s.importThread(ctx, JobArgs{ThreadID: a.ThreadID, Account: a.Account})
	if e != nil {
		return j, e
	}
	j.Model, j.Effort = metadata.Model, metadata.Effort
	return s.registerObserved(j, a.OriginAccount)
}

func (s *Server) registerObserved(j jobs.Job, origin string) (jobs.Job, error) {
	if e := s.validateOriginAccount(origin); e != nil {
		return j, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, e := s.Store.Job(j.ID)
	if e != nil {
		return j, e
	}
	if cur.Managed {
		return cur, nil
	}
	if slices.Contains(s.workerConfig().AccountRegistry.Removed, cur.Account) {
		return cur, errors.New("이 계정은 등록 해제됐습니다. 등록된 계정을 선택하세요.")
	}
	cur.Managed = true
	if cur.Model == "" {
		cur.Model = j.Model
	}
	if cur.Effort == "" {
		cur.Effort = j.Effort
	}
	cur.Registration = &jobs.Registration{OriginAccount: origin}
	cur.State = "idle"
	if cur.Goal != nil {
		switch cur.Goal.Status {
		case "complete":
			cur.State = "complete"
		case "paused":
			cur.State, cur.Paused = "paused", true
		case "budgetLimited":
			cur.State = "budget_limited"
		case "blocked":
			cur.State = "blocked"
		}
	}
	if !cur.Paused && !jobs.Terminal(cur.State) && cr.WriterBusy(cur.Store, cur.ThreadID) {
		cur.State = "external_writer"
	}
	// Registration is metadata only; no start action, account change or goal write.
	e = s.persist(cur)
	return cur, e
}

// The GUI's original login may be a saved account outside worker priority.
// Recording it is metadata, not permission to select it as a managed worker.
func (s *Server) validateOriginAccount(origin string) error {
	if origin == "" {
		return nil
	}
	names, e := accounts.List(s.workerConfig())
	if e != nil {
		return e
	}
	if !slices.Contains(names, origin) {
		return errors.New("origin account is not a saved account")
	}
	return nil
}

func (s *Server) unregisterThread(ctx context.Context, id string) (jobs.Job, error) {
	// Hold the same dispatch gate as the action loop while stopping our worker
	// and cancelling queued actions. Never interrupt an in-flight account switch.
	s.mu.Lock()
	if s.dispatching[id] {
		s.mu.Unlock()
		return jobs.Job{}, errors.New("wait for the in-flight management action before unregistering")
	}
	s.dispatching[id] = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.dispatching, id); s.mu.Unlock() }()
	j, e := s.Store.Job(id)
	if e != nil {
		return j, e
	}
	as, e := s.Store.Actions("")
	if e != nil {
		return j, e
	}
	for _, a := range as {
		if a.JobID == id && a.State == "executing" {
			return j, errors.New("wait for the in-flight management action before unregistering")
		}
	}
	if j.Session != "" {
		if e = s.park(ctx, id, false); e != nil {
			return j, e
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e = s.Store.Job(id)
	if e != nil {
		return j, e
	}
	j.Managed, j.Registration, j.TargetAccount = false, nil, ""
	j.ContinuationDue = false
	j.State = "observed"
	as, e = s.Store.Actions("")
	if e != nil {
		return j, e
	}
	for _, a := range as {
		if a.JobID == id && a.State == "executing" {
			return j, errors.New("wait for the in-flight management action before unregistering")
		}
	}
	for _, a := range as {
		if a.JobID == id && (a.State == "pending" || a.State == "outcome_unknown") {
			a.State = "cancelled"
			if e = s.Store.PutAction(a); e != nil {
				return j, e
			}
		}
	}
	return j, s.persist(j)
}

func (s *Server) observeRegistered(ctx context.Context, j jobs.Job) {
	p, e := s.Resolver.Probe(ctx, s.manager())
	if e != nil {
		return
	}
	defer p.Close()
	t, _, e := p.Recent(ctx, j.ThreadID, 1, "")
	if e != nil {
		return
	}
	g, e := p.Goal(ctx, j.ThreadID)
	if e != nil {
		return
	}
	busy := cr.WriterBusy(j.Store, j.ThreadID)
	s.applyRegisteredObservation(j.ID, t, g, busy)
}

func (s *Server) applyRegisteredObservation(id string, t cr.Thread, g *jobs.Goal, busy bool) {
	s.mu.Lock()
	j, e := s.Store.Job(id)
	if e != nil || j.Registration == nil || !j.Managed || j.Session != "" {
		s.mu.Unlock()
		return
	}
	j.LastObserved = time.Now()
	j.Goal = g
	if g != nil {
		j.Kind = "goal"
	}
	registration := *j.Registration
	j.Registration = &registration
	registration.ExternalState = t.Status.Type
	limited := g != nil && g.Status == "usageLimited"
	if len(t.Turns) > 0 {
		last := t.Turns[len(t.Turns)-1]
		registration.LastTurnID = last.ID
		if last.Error != nil && policy.ErrorKind(last.Error.CodexErrorInfo) == "usageLimitExceeded" {
			limited = true
		}
	}
	if limited {
		registration.HandoffDue = true
		j.ContinuationDue = true
	}
	switch {
	case j.Paused: // Explicit user pause/stop always wins over the external observer.
	case g != nil && g.Status == "complete":
		j.State, j.ContinuationDue = "complete", false
	case g != nil && g.Status == "paused":
		j.State, j.Paused = "paused", true
	case g != nil && g.Status == "budgetLimited":
		j.State, j.ContinuationDue = "budget_limited", false
	case g != nil && g.Status == "blocked":
		j.State, j.ContinuationDue = "blocked", false
	case busy:
		j.State = "external_writer"
	case j.TargetAccount != "": // A manual account action owns the handoff after release.
	case registration.HandoffDue:
		j.State = "quota_wait"
	default:
		j.State = "idle"
	}
	if e = s.persist(j); e != nil {
		s.mu.Unlock()
		s.fail(e)
		return
	}
	s.mu.Unlock()
	if !busy && !j.Paused && !jobs.Terminal(j.State) && j.TargetAccount == "" {
		if registration.HandoffDue {
			s.enqueueOnce(j, "switch_account", nil)
		}
		if g != nil && g.Status == "active" {
			s.enqueueOnce(j, "start", map[string]string{"origin": "automatic"})
		}
	}
}
