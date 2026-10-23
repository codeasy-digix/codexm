package supervisor

import (
	"context"
	"slices"
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
	if a, ok := m[name]; ok && quotaFresh(a) && !a.WasExhausted {
		if state := a.State(""); state == "available" || state == "candidate" {
			return a, nil
		}
	}
	a := s.Reader.Read(ctx, name)
	a.Account = name
	if ctx.Err() != nil {
		return a, ctx.Err()
	}
	if e = s.recordAccount(a); e != nil {
		return a, e
	}
	m, _, e = s.snapshots()
	return m[name], e
}

type executionAccountKey struct{}

// Every execution checks current availability in priority order. Only a viewer
// may reuse recent observations. Unknown never unlocks the reserve account.
func (s *Server) automaticAccount(ctx context.Context, j jobs.Job) (string, error) {
	return s.priorityAccount(ctx, j, true)
}

func (s *Server) priorityAccount(ctx context.Context, j jobs.Job, refresh bool) (string, error) {
	c := s.workerConfig()
	read := s.freshAccount
	if refresh {
		read = func(ctx context.Context, name string) (accounts.Snapshot, error) {
			// The alias may have been removed while another quota was read.
			if !slices.Contains(s.workerConfig().WorkerAccounts.Priority, name) {
				return accounts.Snapshot{}, later("계정 우선순위가 변경되었습니다. 다시 확인합니다.")
			}
			a := s.Reader.Read(ctx, name)
			a.Account = name
			if ctx.Err() != nil {
				return a, ctx.Err()
			}
			if e := s.recordAccount(a); e != nil {
				return a, e
			}
			m, _, e := s.snapshots()
			return m[name], e
		}
	}
	fresh := map[string]accounts.Snapshot{}
	blocked := map[string]bool{}
	allExhausted := true
	for _, name := range c.WorkerAccounts.Priority {
		if name == c.WorkerAccounts.FallbackAccount {
			continue
		}
		a, e := read(ctx, name)
		if e != nil {
			return "", e
		}
		fresh[name], blocked[name] = a, a.WasExhausted
		state := a.State(j.Model)
		allExhausted = allExhausted && state == "exhausted"
		if (state == "available" || state == "candidate") && !a.WasExhausted {
			return name, nil
		}
	}
	if f := c.WorkerAccounts.FallbackAccount; f != "" && (!c.WorkerAccounts.FallbackRequiresAllPrimaryExhausted || allExhausted) {
		a, e := read(ctx, f)
		if e != nil {
			return "", e
		}
		fresh[f], blocked[f] = a, a.WasExhausted
	}
	choice, reason := accounts.Choose(c, fresh, j.Model, blocked)
	if choice != "" {
		return choice, nil
	}
	if !refresh {
		return "", &postpone{reason, 30 * time.Second}
	}
	if e := s.update(j.ID, func(j *jobs.Job) {
		if !j.Paused && !jobs.Terminal(j.State) {
			j.State = "quota_wait"
		}
		j.LastError = reason
	}); e != nil {
		return "", e
	}
	return "", &postpone{reason, 30 * time.Second}
}
