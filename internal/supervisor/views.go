package supervisor

import (
	"context"
	"errors"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/policy"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

// ViewState exposes connection metadata, never prompts, goal objectives or auth.
type ViewState struct {
	ID       string `json:"id"`
	Ready    bool   `json:"ready"`
	ThreadID string `json:"thread_id"`
	RunID    string `json:"run_id"`
	Account  string `json:"account"`
	Binary   string `json:"binary"`
	Socket   string `json:"socket"`
	Cwd      string `json:"cwd"`
	State    string `json:"state"`
	Paused   bool   `json:"paused"`
	Error    string `json:"error,omitempty"`
}

func (s *Server) viewState(j jobs.Job) ViewState {
	c := s.clients[j.ID] // caller holds s.mu
	ready := c != nil && j.ThreadID != "" && j.Socket != "" && j.State != "starting" && j.State != "switching" && j.State != "quota_wait"
	if c != nil {
		select {
		case <-c.Done:
			ready = false
		default:
		}
	}
	return ViewState{ID: j.ID, Ready: ready, ThreadID: j.ThreadID, RunID: j.RunID, Account: j.Account, Binary: j.EngineBinary, Socket: j.Socket, Cwd: j.Cwd, State: j.State, Paused: j.Paused, Error: j.LastError}
}

func (s *Server) openView(ctx context.Context, a JobArgs) (ViewState, error) {
	if !config.Nickname.MatchString(a.ClientID) {
		return ViewState{}, errors.New("a terminal client ID is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.Store.Job(a.ID)
	if e != nil {
		return ViewState{}, e
	}
	if j.Kind == "group" {
		return ViewState{}, errors.New("select a child conversation to open Codex")
	}
	if j.Session == "" && cr.WriterBusy(j.Store, j.ThreadID) {
		return ViewState{}, errForeignWriter
	}
	if !j.Managed {
		j.Managed = true
		j.State = "idle"
		if j.Goal != nil {
			j.Paused = j.Goal.Status != "complete"
			j.State = "paused"
			if !j.Paused {
				j.State = "complete"
			}
		}
	}
	as, e := s.Store.Actions("")
	if e != nil {
		return ViewState{}, e
	}
	v := s.viewState(j)
	if !v.Ready {
		pending := false
		for _, a := range as {
			if a.JobID == j.ID && (a.Kind == "load_view" || a.Kind == "start" || a.Kind == "switch_account") && (a.State == "pending" || a.State == "executing" || a.State == "outcome_unknown") {
				pending = true
			}
		}
		if !pending {
			j.LastError = ""
			if e = s.persist(j, policy.Action("load_view", j, nil)); e != nil {
				return ViewState{}, e
			}
		} else if e = s.persist(j); e != nil {
			return ViewState{}, e
		}
	} else if e = s.persist(j); e != nil {
		return ViewState{}, e
	}
	s.viewers[a.ClientID] = viewer{At: time.Now(), JobID: j.ID}
	return v, nil
}

func (s *Server) liveViewersLocked() map[string]int {
	counts := map[string]int{}
	for id, v := range s.viewers {
		if time.Since(v.At) > 15*time.Second {
			delete(s.viewers, id)
		} else if v.JobID != "" {
			counts[v.JobID]++
		}
	}
	return counts
}

func (s *Server) hasJobViewer(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.liveViewersLocked()[id] > 0
}

// Loading a terminal preserves stopped/paused/completed state. It neither
// reactivates a goal nor queues continuation of the last interrupted input.
func (s *Server) loadView(ctx context.Context, j jobs.Job) error {
	if e := s.ensureRuntime(ctx, j.ID, false); e != nil {
		return e
	}
	if j.Paused || jobs.Terminal(j.State) {
		return s.update(j.ID, func(cur *jobs.Job) {
			if cur.TurnID == "" {
				cur.State = j.State
				cur.Paused = j.Paused
				cur.ContinuationDue = j.ContinuationDue
			}
		})
	}
	return nil
}
