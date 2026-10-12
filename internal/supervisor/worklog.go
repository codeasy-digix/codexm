package supervisor

import (
	"context"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/activity"
	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/store"
)

func (s *Server) changeAccount(id, to, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.Store.Job(id)
	if e != nil {
		return e
	}
	if j.Account == to {
		return nil
	}
	c := jobs.WorkChange{ID: jobs.ID(), JobID: id, ThreadID: j.ThreadID, From: j.Account, To: to, Reason: reason, At: time.Now()}
	j.Account, j.UpdatedAt = to, c.At
	return s.Store.ChangeAccount(j, c)
}

func (s *Server) sampleUsage(ctx context.Context, j jobs.Job, c *cr.Client, force bool) {
	if j.RunID == "" || c == nil {
		return
	}
	r, e := s.Store.Run(j.RunID)
	if e != nil {
		if !store.NotFound(e) {
			s.fail(e)
		}
		return
	}
	if r.EndedAt != nil || !force && r.UsageLatest != nil && time.Since(r.UsageLatest.At) < time.Duration(s.workerConfig().Logging.UsageSampleSeconds)*time.Second {
		return
	}
	snapshot := accounts.Snapshot{Account: j.Account, CheckedAt: time.Now(), Authenticated: true}
	callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if e = c.Call(callCtx, "account/rateLimits/read", map[string]any{}, &snapshot); e != nil {
		snapshot.Error = "account usage read failed"
	}
	// Sampling uses the existing app server and never starts a model turn.
	if e = s.recordAccount(snapshot); e != nil {
		s.fail(e)
		return
	}
	if e = s.Store.SampleRun(j.RunID, activity.Usage(snapshot, j.Model)); e != nil {
		s.fail(e)
	}
}
