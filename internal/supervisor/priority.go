package supervisor

import (
	"context"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/jobs"
)

func quotaFresh(a accounts.Snapshot) bool {
	age := time.Since(a.CheckedAt)
	return !a.CheckedAt.IsZero() && age >= 0 && age < time.Minute
}

func (s *Server) freshAccount(ctx context.Context, name string) (accounts.Snapshot, error) {
	m, _, e := s.snapshots()
	if e != nil {
		return accounts.Snapshot{}, e
	}
	if a, ok := m[name]; ok && quotaFresh(a) {
		return a, nil
	}
	a := s.Reader.Read(ctx, name)
	if ctx.Err() != nil {
		return a, ctx.Err()
	}
	if e = s.recordAccount(a); e != nil {
		return a, e
	}
	m, _, e = s.snapshots()
	return m[name], e
}

// Automatic jobs use the current panel order when their first worker starts.
// Known recent observations avoid extra authentication probes. Unknown is never
// counted as exhaustion when deciding whether the reserve may be used.
func (s *Server) automaticAccount(ctx context.Context, j jobs.Job) (string, error) {
	c := s.workerConfig()
	fresh := map[string]accounts.Snapshot{}
	blocked := map[string]bool{}
	allExhausted := true
	for _, name := range c.WorkerAccounts.Priority {
		if name == c.WorkerAccounts.FallbackAccount {
			continue
		}
		a, e := s.freshAccount(ctx, name)
		if e != nil {
			return "", e
		}
		fresh[name], blocked[name] = a, a.WasExhausted
		state := a.State(j.Model)
		allExhausted = allExhausted && state == "exhausted"
		if state == "available" || state == "candidate" && !a.WasExhausted {
			return name, nil
		}
	}
	if f := c.WorkerAccounts.FallbackAccount; f != "" && (!c.WorkerAccounts.FallbackRequiresAllPrimaryExhausted || allExhausted) {
		a, e := s.freshAccount(ctx, f)
		if e != nil {
			return "", e
		}
		fresh[f], blocked[f] = a, a.WasExhausted
	}
	choice, reason := accounts.Choose(c, fresh, j.Model, blocked)
	if choice != "" {
		return choice, nil
	}
	if e := s.update(j.ID, func(j *jobs.Job) { j.State = "quota_wait"; j.LastError = reason }); e != nil {
		return "", e
	}
	return "", &postpone{reason, 30 * time.Second}
}
