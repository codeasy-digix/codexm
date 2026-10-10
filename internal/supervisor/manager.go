package supervisor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/manager"
	"github.com/codeasy-digix/codexm/internal/policy"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func (s *Server) decide(ctx context.Context, a jobs.Action) error {
	js, e := s.Store.Jobs()
	if e != nil {
		return e
	}
	var p struct {
		Text   string `json:"text"`
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(a.Payload, &p)
	incident := a.ID
	if a.Kind == "manager_incident" {
		h := sha256.Sum256([]byte(a.JobID + ":" + p.Reason))
		incident = hex.EncodeToString(h[:16])
	}
	hour, n, e := s.Store.DecisionCounts(incident)
	if e != nil {
		return e
	}
	if n >= s.Config.Manager.MaxCallsPerIncident || hour >= s.Config.Manager.MaxCallsPerHour {
		return s.managerWaiting(a, "manager decision call limit reached")
	}
	var recent string
	if e = s.Store.DB.QueryRow("SELECT data FROM manager_decisions ORDER BY at DESC LIMIT 1").Scan(&recent); e == nil {
		var r manager.Record
		if json.Unmarshal([]byte(recent), &r) == nil && r.State == "failed" && time.Since(r.FinishedAt) < time.Duration(s.Config.Manager.FailureCooldownSeconds)*time.Second {
			return s.managerWaiting(a, "manager failure cooldown; supervision remains active")
		}
	}
	rec := manager.Record{ID: a.ID, Incident: incident, Account: s.manager(), State: "executing", StartedAt: time.Now()}
	if e = s.Store.Decision(a.ID, incident, rec); e != nil {
		return e
	}
	roots := []string{}
	snap := []map[string]any{}
	for _, j := range js {
		if a.JobID != "" && j.ID != a.JobID && j.ParentID != a.JobID {
			continue
		}
		roots = append(roots, j.Cwd)
		last := j.LastOutput
		if len([]rune(last)) > 1500 {
			last = string([]rune(last)[len([]rune(last))-1500:])
		}
		snap = append(snap, map[string]any{"id": j.ID, "parent_id": j.ParentID, "kind": j.Kind, "cwd": j.Cwd, "state": j.State, "paused": j.Paused, "managed": j.Managed, "goal": j.Goal, "last_input": j.LastInput, "last_output": last, "error": j.LastError})
	}
	if a.JobID == "" {
		cwd, _ := os.Getwd()
		roots = append(roots, cwd)
	}
	contextBytes, _ := json.Marshal(map[string]any{"instruction": p.Text, "incident": p.Reason, "jobs": snap, "workspace_roots": roots, "allow_create_job": a.Kind == "manager_command", "unknown_dispatches": s.unknownActions()})
	if len([]rune(string(contextBytes))) > s.Config.Manager.MaxInputChars {
		return s.managerFailure(a, rec, fmt.Errorf("manager input exceeds %d characters; select a narrower job", s.Config.Manager.MaxInputChars))
	}
	decisionCtx, cancel := context.WithTimeout(ctx, time.Duration(s.Config.Manager.DecisionTimeoutSeconds)*time.Second)
	defer cancel()
	d, e := manager.Run(decisionCtx, s.Resolver, rec.Account, s.Config.Manager.Model, string(contextBytes), &rec)
	if e == nil {
		e = manager.Validate(d, js, a.JobID, roots, a.Kind == "manager_command")
	}
	if e != nil {
		return s.managerFailure(a, rec, e)
	}
	current, e := s.Store.Jobs()
	if e != nil {
		return e
	}
	if e = manager.Validate(d, current, a.JobID, roots, a.Kind == "manager_command"); e != nil {
		return s.managerFailure(a, rec, e)
	}
	rec.Decision = &d
	rec.State = "validated"
	rec.FinishedAt = time.Now()
	if e = s.Store.UpdateDecision(a.ID, rec); e != nil {
		return e
	}
	for _, act := range d.Actions {
		switch act.Type {
		case "create_job":
			readOnly := false
			model, effort := "", ""
			for _, j := range current {
				if j.ID == act.ParentID || j.ID == a.JobID {
					readOnly = readOnly || j.ReadOnly
					model, effort = j.Model, j.Effort
				}
			}
			_, e = s.newJob(NewArgs{Title: act.Title, Cwd: act.Cwd, Kind: act.Kind, ParentID: act.ParentID, Prompt: act.Prompt, Goal: act.Goal, ReadOnly: readOnly, Model: model, Effort: effort})
		case "send_input":
			_, e = s.Handle(ctx, Call{Command: "send", Args: mustJSON(JobArgs{ID: act.JobID, Text: act.Prompt})})
		case "retry":
			j, err := s.Store.Job(act.JobID)
			if err != nil {
				return err
			}
			if j.Paused || jobs.Busy(j.State) {
				return s.managerWaiting(a, "job state changed while deciding")
			}
			_ = s.update(j.ID, func(j *jobs.Job) { j.State = "idle" })
			e = s.Store.PutAction(policy.Action("send_input", j, jobs.Input{Text: "Continue the interrupted request after checking the recorded outcomes of previous actions.", MessageID: jobs.ID()}))
		case "needs_user":
			if act.JobID != "" {
				e = s.update(act.JobID, func(j *jobs.Job) {
					if !j.Paused && !jobs.Busy(j.State) {
						j.State = "needs_user"
						j.LastError = d.Summary
					}
				})
			}
		case "wait":
			if act.JobID != "" {
				e = s.update(act.JobID, func(j *jobs.Job) {
					if !j.Paused && !jobs.Busy(j.State) {
						j.State = "manager_wait"
						j.LastError = d.Summary
					}
				})
			}
		}
		if e != nil {
			return e
		}
	}
	rec.State = "applied"
	return s.Store.UpdateDecision(a.ID, rec)
}
func (s *Server) unknownActions() []jobs.Action {
	as, _ := s.Store.Actions("outcome_unknown")
	return as
}
func (s *Server) managerWaiting(a jobs.Action, reason string) error {
	if a.JobID != "" {
		_ = s.update(a.JobID, func(j *jobs.Job) {
			if !j.Paused && !jobs.Busy(j.State) {
				j.State = "manager_wait"
				j.LastError = reason
			}
		})
	}
	return &manager.Unavailable{Reason: reason}
}
func (s *Server) managerFailure(a jobs.Action, r manager.Record, e error) error {
	r.State = "failed"
	r.Error = cr.SafeText(e.Error())
	r.FinishedAt = time.Now()
	if err := s.Store.UpdateDecision(a.ID, r); err != nil {
		return err
	}
	return s.managerWaiting(a, r.Error)
}
