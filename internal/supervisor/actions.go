package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/activity"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/manager"
	"github.com/codeasy-digix/codexm/internal/policy"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/store"
)

type postpone struct {
	reason string
	delay  time.Duration
}

func (e *postpone) Error() string { return e.reason }
func later(reason string) error   { return &postpone{reason, 3 * time.Second} }

var errUncertain = errors.New("dispatch outcome unknown; inspect history before retrying")
var errForeignWriter = errors.New("another Codex process owns this conversation; exit that process before taking ownership")

func (s *Server) update(id string, f func(*jobs.Job)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.Store.Job(id)
	if e != nil {
		return e
	}
	f(&j)
	return s.persist(j)
}
func (s *Server) client(id string) *cr.Client { s.mu.Lock(); defer s.mu.Unlock(); return s.clients[id] }
func (s *Server) actionLoop() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
		}
		as, e := s.Store.Actions("pending")
		if e != nil {
			s.fail(e)
			continue
		}
		for _, a := range as {
			if time.Now().Before(a.NotBefore) {
				continue
			}
			key := a.JobID
			if key == "" || strings.HasPrefix(a.Kind, "manager_") {
				key = "@manager"
			}
			s.mu.Lock()
			if s.unhealthy != "" || s.dispatching[key] || len(s.dispatching) >= 6 {
				s.mu.Unlock()
				continue
			}
			s.dispatching[key] = true
			s.mu.Unlock()
			s.wg.Add(1)
			go func(a jobs.Action, key string) {
				defer s.wg.Done()
				defer func() { s.mu.Lock(); delete(s.dispatching, key); s.mu.Unlock() }()
				s.execute(a)
			}(a, key)
		}
	}
}
func (s *Server) execute(a jobs.Action) {
	if s.isStopping() && a.Kind != "pause_execution" && a.Kind != "cleanup" {
		s.finishAction(a, "cancelled", nil)
		return
	}
	j, e := s.Store.Job(a.JobID)
	if e != nil && !(a.Kind == "manager_command" && a.JobID == "") {
		s.finishAction(a, "failed", e)
		return
	}
	if !j.Managed {
		switch a.Kind {
		case "start", "send_input", "goal_continue", "switch_account", "prefer_account", "change_account":
			s.finishAction(a, "cancelled", nil)
			return
		}
	}
	if j.TargetAccount != "" && a.Kind != "change_account" && a.Kind != "pause_execution" && a.Kind != "cleanup" {
		a.NotBefore = time.Now().Add(time.Second)
		_ = s.Store.PutAction(a)
		return
	}
	if j.Paused && a.Kind != "change_account" && a.Kind != "pause_execution" && a.Kind != "cleanup" && a.Kind != "load_view" && a.Kind != "set_goal" && a.Kind != "manager_command" {
		if jobs.Terminal(j.State) {
			s.finishAction(a, "cancelled", nil)
			return
		}
		a.NotBefore = time.Now().Add(5 * time.Second)
		_ = s.Store.PutAction(a)
		return
	}
	// A user input may wait across account runs. Lifecycle actions belong to one run.
	if a.RunID != "" && a.RunID != j.RunID && a.Kind != "change_account" && a.Kind != "send_input" && a.Kind != "start" && a.Kind != "load_view" && a.Kind != "manager_command" {
		s.finishAction(a, "cancelled", nil)
		return
	}
	a.State = "executing"
	a.Attempts++
	if e = s.Store.PutAction(a); e != nil {
		s.fail(e)
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 90*time.Second)
	defer cancel()
	switch a.Kind {
	case "change_account":
		e = s.manualAccountChange(ctx, &a)
	case "load_view":
		e = s.loadView(ctx, j)
	case "start":
		var p struct {
			Origin string `json:"origin"`
		}
		_ = json.Unmarshal(a.Payload, &p)
		e = s.ensureRuntime(ctx, j.ID, p.Origin == "user")
		if e == nil {
			cur, _ := s.Store.Job(j.ID)
			if cur.State == "complete" {
				s.enqueueOnce(cur, "cleanup", nil)
			}
			if cur.Kind == "goal" && cur.Goal != nil && cur.Goal.Status == "active" && !cur.Paused {
				s.enqueueOnce(cur, "goal_continue", nil)
			}
		}
	case "send_input":
		var in jobs.Input
		if e = json.Unmarshal(a.Payload, &in); e == nil {
			e = s.send(ctx, j.ID, in)
		}
	case "pause_execution":
		e = s.park(ctx, j.ID, true)
	case "cleanup":
		if s.hasJobViewer(j.ID) {
			break
		}
		if j.State == "complete" && j.TurnID != "" {
			e = later("goal is complete; waiting for the final turn to finish")
			break
		}
		if jobs.Terminal(j.State) || j.Paused {
			e = s.park(ctx, j.ID, false)
		} else {
			e = later("waiting for the active turn to finish before cleanup")
		}
	case "switch_account":
		if j.State == "quota_wait" || j.State == "switching" {
			e = s.switchAccount(ctx, j.ID, "", true)
		}
	case "prefer_account":
		var p struct {
			Account string `json:"account"`
		}
		_ = json.Unmarshal(a.Payload, &p)
		if j.Account == s.workerConfig().WorkerAccounts.FallbackAccount {
			e = s.switchAccount(ctx, j.ID, p.Account, false)
		}
	case "retry":
		e = s.retry(ctx, j.ID, a.CreatedAt)
	case "goal_continue":
		e = s.continueGoal(ctx, j.ID)
	case "set_goal":
		var p map[string]any
		if e = json.Unmarshal(a.Payload, &p); e != nil {
			break
		}
		if p["objective"] == "" {
			e = errors.New("goal objective is empty")
			break
		}
		// Editing a parked goal is an explicit metadata operation. Load its
		// native state without treating the edit as a request to resume work.
		if e = s.loadView(ctx, j); e != nil {
			break
		}
		j, _ = s.Store.Job(j.ID)
		p["threadId"] = j.ThreadID
		p["origin"] = "user"
		if p["status"] == nil {
			p["status"] = "active"
			g, err := s.client(j.ID).Goal(ctx, j.ThreadID)
			if err != nil {
				e = err
				break
			}
			if g != nil {
				p["status"] = g.Status
			}
		}
		e = s.client(j.ID).Call(ctx, "thread/goal/set", p, nil)
		if e == nil {
			var g *jobs.Goal
			g, e = s.client(j.ID).Goal(ctx, j.ThreadID)
			if e == nil {
				e = s.update(j.ID, func(j *jobs.Job) {
					j.Goal = g
					j.ContinuationDue = g != nil && g.Status == "active"
					if g != nil {
						switch g.Status {
						case "active":
							j.Paused = false
							j.State = "goal_wait"
						case "complete":
							j.State = "complete"
						case "paused":
							j.Paused = true
							j.State = "paused"
						default:
							j.State = "goal_wait"
						}
					}
				})
			}
		}

	case "check_completion":
		e = s.checkCompletion(ctx, j.ID)
	case "manager_command", "manager_incident":
		cancel()
		e = s.decide(s.ctx, a)
	default:
		e = fmt.Errorf("unsupported action %s", a.Kind)
	}
	var p *postpone
	if e != nil && s.ctx.Err() != nil && s.Persistent && !s.isStopping() {
		// A service restart is observation loss, not evidence of command failure.
		// Keep the durable handoff intent and reconcile it after reconnecting.
		s.finishAction(a, "outcome_unknown", e)
		return
	}
	if errors.As(e, &p) {
		a.State = "pending"
		a.Error = cr.SafeText(p.reason)
		a.NotBefore = time.Now().Add(p.delay)
		if e = s.Store.PutAction(a); e != nil {
			s.fail(e)
		}
		return
	}
	state := "done"
	if e != nil {
		state = "failed"
		if errors.Is(e, errUncertain) {
			state = "outcome_unknown"
		}
		var unavailable *manager.Unavailable
		if errors.As(e, &unavailable) {
			state = "manager_wait"
		}
		if a.JobID != "" {
			_ = s.update(a.JobID, func(j *jobs.Job) {
				j.LastError = cr.SafeText(e.Error())
				if a.Kind == "change_account" {
					j.TargetAccount = ""
					if j.State == "switching" {
						j.State = "error"
					}
					return
				}
				if !j.Paused && !jobs.Terminal(j.State) && j.State != "quota_wait" && j.State != "auth_required" && j.State != "budget_limited" && j.State != "manager_wait" {
					j.State = "error"
				}
				if errors.Is(e, errForeignWriter) {
					j.State = "external_writer"
				}
			})
		}
	}
	s.finishAction(a, state, e)
}
func (s *Server) finishAction(a jobs.Action, state string, e error) {
	a.State = state
	if e != nil {
		a.Error = cr.SafeText(e.Error())
	}
	if e = s.Store.PutAction(a); e != nil {
		s.fail(e)
	}
}

func overlapping(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	return a == b || strings.HasPrefix(a, b+string(filepath.Separator)) || strings.HasPrefix(b, a+string(filepath.Separator))
}
func (s *Server) reserve(id string) error {
	return s.reserveRuntime(id, false)
}

func (s *Server) reserveRuntime(id string, viewing bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.Store.Job(id)
	if e != nil {
		return e
	}
	if j.Paused && !viewing {
		return later("job paused")
	}
	all, e := s.Store.Jobs()
	if e != nil {
		return e
	}
	busy := 0
	for _, x := range all {
		if x.ID == id || !x.Managed || (!jobs.Busy(x.State) && x.TurnID == "") {
			continue
		}
		busy++
		if !j.ReadOnly && !x.ReadOnly && overlapping(j.Cwd, x.Cwd) {
			return later("workspace is reserved by " + x.Title)
		}
	}
	if busy >= s.workerConfig().Execution.MaxParallelJobs {
		return later("parallel job limit reached")
	}
	if j.State != "running" && j.State != "waiting_on_user" && j.State != "waiting_on_approval" {
		j.State = "starting"
	}
	return s.persist(j)
}
func (s *Server) snapshots() (map[string]accounts.Snapshot, map[string]bool, error) {
	raw, e := s.Store.Snapshots()
	m := map[string]accounts.Snapshot{}
	blocked := map[string]bool{}
	for _, b := range raw {
		var x accounts.Snapshot
		if json.Unmarshal(b, &x) == nil {
			m[x.Account] = x
			blocked[x.Account] = x.WasExhausted
		}
	}
	return m, blocked, e
}
func (s *Server) initialAccount(ctx context.Context, j jobs.Job) (string, error) {
	if !j.AutoAccount && slices.Contains(s.workerConfig().AccountRegistry.Removed, j.Account) {
		return "", errors.New("이 계정은 등록 해제됐습니다. 작업의 계정을 변경하거나 다시 등록하세요.")
	}
	// A manual selection must either use that exact account or fail visibly.
	// It must never silently fall through to a different priority account.
	if j.TargetAccount != "" {
		return j.TargetAccount, s.checkSelectedAccount(ctx, j, j.TargetAccount)
	}
	if j.AutoAccount {
		return s.automaticAccount(ctx, j)
	}
	// Opening a viewer reuses a recently verified quota instead of launching
	// another authentication server and waiting for the same network response.
	if snapshots, blocked, e := s.snapshots(); e == nil && j.Account != s.workerConfig().WorkerAccounts.FallbackAccount {
		if a, ok := snapshots[j.Account]; ok && !blocked[j.Account] && a.Authenticated && a.Error == "" && time.Since(a.CheckedAt) >= 0 && time.Since(a.CheckedAt) < time.Minute {
			if state := a.State(j.Model); state == "available" || state == "candidate" {
				return j.Account, nil
			}
		}
	}
	a := s.Reader.Read(ctx, j.Account)
	if e := s.recordAccount(a); e != nil {
		return "", e
	}
	if !a.Authenticated {
		_ = s.update(j.ID, func(j *jobs.Job) { j.State = "auth_required" })
		return "", fmt.Errorf("account %s: %s", j.Account, a.Error)
	}
	if a.Error != "" {
		return "", later("account availability could not be checked: " + a.Error)
	}
	if j.Account != s.workerConfig().WorkerAccounts.FallbackAccount {
		_, blocked, err := s.snapshots()
		if err != nil {
			return "", err
		}
		state := a.State(j.Model)
		if state == "available" || state == "candidate" && !blocked[j.Account] {
			return j.Account, nil
		}
	}
	if _, e := s.refreshAccounts(ctx); e != nil {
		return "", e
	}
	m, blocked, e := s.snapshots()
	if e != nil {
		return "", e
	}
	choice, reason := accounts.Choose(s.workerConfig(), m, j.Model, blocked)
	if choice == "" {
		_ = s.update(j.ID, func(j *jobs.Job) { j.State = "quota_wait"; j.LastError = reason })
		return "", &postpone{reason, 30 * time.Second}
	}
	return choice, nil
}
func (s *Server) recordAccount(a accounts.Snapshot) error {
	s.accountMu.Lock()
	defer s.accountMu.Unlock()
	m, _, e := s.snapshots()
	if e != nil {
		return e
	}
	a.WasExhausted = a.WasExhausted || m[a.Account].WasExhausted || a.State("") == "exhausted"
	if a.Ordinary != nil && *a.Ordinary && a.State("") == "available" {
		a.WasExhausted = false
	}
	if e = s.Store.Snapshot(a.Account, a); e != nil {
		return e
	}
	s.queueQuotaNotice(a)
	return nil
}
func (s *Server) ensureRuntime(ctx context.Context, id string, user bool) error {
	return s.ensureRuntimeMode(ctx, id, user, false)
}

func (s *Server) ensureRuntimeMode(ctx context.Context, id string, user, viewing bool) error {
	j, e := s.Store.Job(id)
	if e != nil {
		return e
	}
	if j.Kind == "group" {
		return nil
	}
	if c := s.client(id); c != nil {
		select {
		case <-c.Done:
		default:
			if user && j.Goal != nil && j.Goal.Status != "complete" {
				if e = c.SetGoalStatus(ctx, j.ThreadID, "active", "user"); e != nil {
					return e
				}
				_ = s.update(id, func(j *jobs.Job) { j.Paused = false; j.State = "goal_wait"; j.ContinuationDue = true })
			}
			return nil
		}
	}
	if e = s.reserveRuntime(id, viewing); e != nil {
		return e
	}
	// A live owned worker wins over a local timeout. Reattach instead of launching twice.
	if j.Session != "" && cr.HasSession(ctx, j.Session) {
		return s.connect(ctx, j, user)
	}
	if cr.WriterBusy(j.Store, j.ThreadID) {
		return errForeignWriter
	}
	account, e := s.initialAccount(ctx, j)
	if e != nil {
		return e
	}
	if e = s.changeAccount(id, account, "시작 시 사용 가능한 우선 계정 선택"); e != nil {
		return e
	}
	j, e = s.Store.Job(id)
	if e != nil {
		return e
	}
	j.Account = account
	if j.EngineBinary == "" {
		j.EngineBinary = s.Resolver.Binary
		j.EngineVersion = s.Resolver.Version
	}
	if _, e = os.Stat(j.EngineBinary); e != nil {
		return fmt.Errorf("pinned job engine unavailable: %w", e)
	}
	j.RunID = jobs.ID()
	j.Session = cr.ProcessSession(s.Config, j.RunID)
	j.Socket = cr.SocketPath(s.Config, j.RunID[:12])
	j.State = "starting"
	j.PendingRequests = nil
	j.TurnID = ""
	if e = s.update(id, func(cur *jobs.Job) { j.Paused = cur.Paused; j.LastInput = cur.LastInput; *cur = j }); e != nil {
		return e
	}
	run := jobs.Run{ID: j.RunID, JobID: id, ThreadID: j.ThreadID, Account: account, Socket: j.Socket, Session: j.Session, EngineBinary: j.EngineBinary, EngineVersion: j.EngineVersion, StartedAt: time.Now(), State: "starting"}
	if e = s.Store.PutRun(run); e != nil {
		return e
	}
	if snapshots, _, err := s.snapshots(); err == nil {
		if snap, ok := snapshots[account]; ok {
			if e = s.Store.SampleRun(j.RunID, activity.Usage(snap, j.Model)); e != nil {
				return e
			}
		}
	}
	r := s.Resolver
	r.Binary = j.EngineBinary
	r.Version = j.EngineVersion
	if e = r.LaunchWorker(ctx, s.Self, cr.WorkerSpec{Account: account, Socket: j.Socket, Session: j.Session, Cwd: j.Cwd}); e != nil {
		_ = s.Store.EndRun(j.RunID, "launch_failed", time.Now())
		return e
	}
	return s.connect(ctx, j, user)
}
func (s *Server) connect(ctx context.Context, j jobs.Job, user bool) error {
	var c *cr.Client
	var e error
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		c, e = cr.DialUnix(ctx, j.Socket)
		if e == nil {
			break
		}
		if !cr.HasSession(ctx, j.Session) {
			return fmt.Errorf("worker stopped before its socket became ready: %w", e)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	if c == nil {
		return later("worker is alive; waiting for its socket, without launching a replacement")
	}
	s.mu.Lock()
	old := s.clients[j.ID]
	s.clients[j.ID] = c
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	s.watch(j.ID, j.RunID, c)
	var r cr.ResumeResponse
	hadThread := j.ThreadID != ""
	if j.ThreadID == "" {
		var loaded struct {
			Data []string `json:"data"`
		}
		if e = c.Call(ctx, "thread/loaded/list", map[string]any{}, &loaded); e != nil {
			return e
		}
		if len(loaded.Data) == 1 {
			j.ThreadID = loaded.Data[0]
		} else if len(loaded.Data) > 1 {
			return fmt.Errorf("%w: multiple loaded conversations need inspection", errUncertain)
		}
	}
	if j.ThreadID == "" {
		p := map[string]any{"cwd": j.Cwd, "modelProvider": "openai", "approvalPolicy": "on-request", "sandbox": "workspace-write"}
		if j.ReadOnly {
			p["sandbox"] = "read-only"
		}
		if j.Model != "" {
			p["model"] = j.Model
		}
		if j.Effort != "" {
			p["config"] = map[string]any{"model_reasoning_effort": j.Effort}
		}
		e = c.Call(ctx, "thread/start", p, &r)
		if e == nil && j.Title != "" {
			e = c.Call(ctx, "thread/name/set", map[string]any{"threadId": r.Thread.ID, "name": j.Title}, nil)
		}
	} else {
		r, e = c.Resume(ctx, j.ThreadID)
		if e == nil {
			var recent cr.Thread
			recent, _, e = c.Recent(ctx, j.ThreadID, 1, "")
			if e == nil {
				r.Thread = recent
			}
		}
	}
	if e != nil {
		c.Close()
		if cr.IsWriterCollision(e) {
			return errForeignWriter
		}
		return e
	}
	if e = s.update(j.ID, func(j *jobs.Job) {
		j.ThreadID = r.Thread.ID
		j.Model = r.Model
		j.Effort = r.ReasoningEffort
		j.Approval = string(r.ApprovalPolicy)
		j.Sandbox = string(r.Sandbox)
		j.LastObserved = time.Now()
		if !j.Paused && !jobs.Terminal(j.State) {
			j.State = "idle"
		}
	}); e != nil {
		return e
	}
	j, _ = s.Store.Job(j.ID)
	if e = s.Store.IdentifyRun(j.RunID, j.ThreadID); e != nil {
		return e
	}
	g, e := c.Goal(ctx, j.ThreadID)
	if e != nil {
		return e
	}
	if g != nil && g.Status == "usageLimited" && j.ResumeLimitedGoal && !j.Paused {
		if e = c.SetGoalStatus(ctx, j.ThreadID, "active", "automatic"); e != nil {
			return e
		}
		g, e = c.Goal(ctx, j.ThreadID)
		if e != nil {
			return e
		}
	}
	if g == nil && j.Goal != nil {
		p, err := initialGoalParams(j, hadThread)
		if err != nil {
			return err
		}
		e = c.Call(ctx, "thread/goal/set", p, nil)
		if e != nil {
			return e
		}
		g, e = c.Goal(ctx, j.ThreadID)
		if e != nil {
			return e
		}
	} else if g != nil && g.Status != "complete" && user {
		if e = c.SetGoalStatus(ctx, j.ThreadID, "active", "user"); e != nil {
			return e
		}
		g, e = c.Goal(ctx, j.ThreadID)
		if e != nil {
			return e
		}
	}
	return s.update(j.ID, func(j *jobs.Job) {
		j.Goal = g
		j.ResumeLimitedGoal = false
		if j.Registration != nil {
			r := *j.Registration
			r.HandoffDue = false
			j.Registration = &r
		}
		if len(r.Thread.Turns) > 0 {
			t := r.Thread.Turns[len(r.Thread.Turns)-1]
			if user && j.Kind != "goal" && (t.Status == "interrupted" || t.Status == "failed") {
				j.ContinuationDue = true
			}
			if t.Status == "inProgress" {
				j.TurnID = t.ID
				j.State = "running"
			}
		}
		if g != nil && g.Status == "active" && !j.Paused && j.TurnID == "" {
			j.State = "goal_wait"
			j.ContinuationDue = true
		}
		if g != nil && g.Status == "complete" {
			j.State = "complete"
		}
	})
}

func initialGoalParams(j jobs.Job, hadThread bool) (map[string]any, error) {
	if hadThread {
		return nil, fmt.Errorf("%w: native goal is missing from an existing thread; inspect it before recreating its budget or usage", errUncertain)
	}
	return map[string]any{"threadId": j.ThreadID, "objective": j.Goal.Objective, "status": j.Goal.Status, "origin": "user", "tokenBudget": j.Goal.TokenBudget}, nil
}
func (s *Server) send(ctx context.Context, id string, in jobs.Input) error {
	j, e := s.Store.Job(id)
	if e != nil {
		return e
	}
	if !in.Steer && j.TurnID != "" {
		return later("input queued for the next turn")
	}
	if e = s.reserve(id); e != nil {
		return e
	}
	if e = s.ensureRuntime(ctx, id, false); e != nil {
		return e
	}
	j, e = s.Store.Job(id)
	if e != nil {
		return e
	}
	if j.Paused {
		return later("job paused")
	}
	if !in.Steer && j.TurnID != "" {
		return later("input queued for the next turn")
	}
	c := s.client(id)
	if c == nil {
		return cr.ErrDisconnected
	}
	t, e := c.SendInput(ctx, j.ThreadID, in, j.TurnID)
	if e != nil {
		var rpc *cr.RPCError
		if !errors.As(e, &rpc) {
			return fmt.Errorf("%w: %v", errUncertain, e)
		}
		return e
	}
	return s.update(id, func(j *jobs.Job) {
		j.LastInput = in.Text
		j.ContinuationDue = false
		if !j.Paused && !jobs.Terminal(j.State) && j.LastTurnID != t.ID && t.ID != "" {
			j.TurnID = t.ID
			j.State = "running"
		}
	})
}
func (s *Server) park(ctx context.Context, id string, pauseGoal bool) error {
	j, e := s.Store.Job(id)
	if e != nil {
		return e
	}
	c := s.client(id)
	if c == nil && j.Session != "" && cr.HasSession(ctx, j.Session) {
		if e = s.connect(ctx, j, false); e != nil {
			return e
		}
		c = s.client(id)
		j, _ = s.Store.Job(id)
	}
	if c != nil {
		if pauseGoal && j.Goal != nil && j.Goal.Status != "complete" {
			if e = c.SetGoalStatus(ctx, j.ThreadID, "paused", "user"); e != nil {
				return e
			}
		}
		if j.TurnID != "" {
			if e = c.Interrupt(ctx, j.ThreadID, j.TurnID); e != nil {
				return e
			}
			deadline := time.Now().Add(15 * time.Second)
			for time.Now().Before(deadline) {
				t, e := c.Read(ctx, j.ThreadID, true)
				if e != nil {
					return e
				}
				if t.Status.Type != "active" {
					break
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(100 * time.Millisecond):
				}
			}
			t, e := c.Read(ctx, j.ThreadID, false)
			if e != nil {
				return e
			}
			if t.Status.Type == "active" {
				return later("waiting for interrupt acknowledgement")
			}
		}
	}
	s.sampleUsage(ctx, j, c, true)
	// Never kill a process that we did not create. Thread writer is checked again on launch.
	if j.Session != "" && cr.HasSession(ctx, j.Session) {
		if e = cr.KillSession(ctx, j.Session); e != nil {
			return e
		}
	}
	if c != nil {
		c.Close()
	}
	s.mu.Lock()
	delete(s.clients, id)
	s.mu.Unlock()
	if j.Session != "" {
		// tmux removal is asynchronous: reconnecting immediately can mistake the
		// dying worker's lock for a foreign writer and stop an account rotation.
		wait, cancel := context.WithTimeout(ctx, 5*time.Second)
		e = cr.WaitWriterRelease(wait, j.Store, j.ThreadID)
		cancel()
		if e != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return later("waiting for the previous writer lock to be released")
		}
	}
	if j.RunID != "" {
		if e = s.Store.EndRun(j.RunID, "ended", time.Now()); e != nil && !store.NotFound(e) {
			return e
		}
	}
	if j.Socket != "" {
		_ = os.Remove(j.Socket)
	}
	return s.update(id, func(j *jobs.Job) { j.Session = ""; j.Socket = ""; j.TurnID = ""; j.PendingRequests = nil })
}
func (s *Server) switchAccount(ctx context.Context, id, preferred string, exhausted bool) error {
	j, e := s.Store.Job(id)
	if e != nil {
		return e
	}
	if j.Paused {
		return nil
	}
	continueWork := exhausted || j.TurnID != "" || j.ContinuationDue || j.Goal != nil && j.Goal.Status == "active"
	if exhausted {
		m, _, _ := s.snapshots()
		a := m[j.Account]
		a.Account = j.Account
		a.WasExhausted = true
		a.Authenticated = true
		a.Ordinary = new(bool)
		a.CheckedAt = time.Now()
		a.Error = ""
		if e = s.recordAccount(a); e != nil {
			return e
		}
	}
	if _, e = s.refreshAccounts(ctx); e != nil {
		return e
	}
	m, blocked, e := s.snapshots()
	if e != nil {
		return e
	}
	choice, reason := accounts.Choose(s.workerConfig(), m, j.Model, blocked)
	if preferred != "" && choice != preferred {
		return nil
	}
	if choice == j.Account && !exhausted {
		return nil
	}
	if choice == j.Account && exhausted && blocked[choice] {
		choice = ""
		reason = "waiting for authoritative usage recovery"
	}
	if e = s.update(id, func(j *jobs.Job) { j.State = "switching" }); e != nil {
		return e
	}
	if e = s.park(ctx, id, false); e != nil {
		return e
	}
	if choice == "" {
		_ = s.update(id, func(j *jobs.Job) { j.State = "quota_wait"; j.LastError = reason; j.LastAccountCheck = time.Now() })
		return &postpone{reason, 30 * time.Second}
	}
	changeReason := "상위 계정 한도 회복"
	if exhausted {
		changeReason = "기존 계정 한도 소진"
	}
	if e = s.changeAccount(id, choice, changeReason); e != nil {
		return e
	}
	if e = s.update(id, func(j *jobs.Job) {
		j.State = "queued"
		j.LastAccountCheck = time.Now()
		j.ContinuationDue = continueWork
		j.ResumeLimitedGoal = true
	}); e != nil {
		return e
	}
	if e = s.ensureRuntime(ctx, id, false); e != nil {
		return e
	}
	j, _ = s.Store.Job(id)
	if !continueWork {
		return nil
	}
	if j.Goal != nil && j.Goal.Status == "usageLimited" {
		c := s.client(id)
		if e = c.SetGoalStatus(ctx, j.ThreadID, "active", "automatic"); e != nil {
			return e
		}
		g, _ := c.Goal(ctx, j.ThreadID)
		_ = s.update(id, func(j *jobs.Job) { j.Goal = g; j.State = "goal_wait" })
	}
	if j.Kind == "goal" {
		s.enqueueOnce(j, "goal_continue", nil)
		return nil
	}
	return s.queueContinuation(j)
}
func (s *Server) queueContinuation(j jobs.Job) error {
	if s.hasQueuedInput(j.ID) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.Store.Job(j.ID)
	if e != nil {
		return e
	}
	if j.Paused || jobs.Terminal(j.State) {
		return nil
	}
	j.ContinuationDue = false
	return s.persist(j, policy.Action("send_input", j, jobs.Input{Text: "Continue the interrupted request in this conversation. Check recorded tool outcomes before acting, preserving completed work and existing instructions.", MessageID: jobs.ID()}))
}
func (s *Server) retry(ctx context.Context, id string, scheduledAt time.Time) error {
	j, e := s.Store.Job(id)
	if e != nil {
		return e
	}
	if j.Paused || j.State != "retry_wait" {
		return nil
	}
	if j.RetryCount >= 3 {
		return s.queueIncident(j, "transient error repeated three times")
	}
	delay := time.Duration(1<<j.RetryCount) * 5 * time.Second
	if remaining := time.Until(scheduledAt.Add(delay)); remaining > 0 {
		return &postpone{"bounded retry backoff", remaining}
	}
	_ = s.update(id, func(j *jobs.Job) { j.RetryCount++; j.State = "idle" })
	return s.send(ctx, id, jobs.Input{Text: "Continue the interrupted request, checking prior results before repeating any action.", MessageID: jobs.ID()})
}
func (s *Server) continueGoal(ctx context.Context, id string) error {
	j, e := s.Store.Job(id)
	if e != nil {
		return e
	}
	if j.Paused || j.Kind != "goal" || j.Goal == nil || j.Goal.Status != "active" || jobs.Terminal(j.State) {
		return nil
	}
	if j.TurnID != "" {
		return nil
	}
	if s.hasQueuedInput(id) {
		return later("user input precedes automatic goal continuation")
	}
	return s.send(ctx, id, jobs.Input{Text: "Continue working toward the persisted goal in this conversation. Preserve its objective and budget. Check prior results before acting, and update the goal status when achieved or blocked.", MessageID: jobs.ID()})
}
func (s *Server) hasQueuedInput(id string) bool {
	as, _ := s.Store.Actions("")
	for _, a := range as {
		if a.JobID == id && a.Kind == "send_input" && (a.State == "pending" || a.State == "executing" || a.State == "outcome_unknown") {
			return true
		}
	}
	return false
}
func (s *Server) queueIncident(j jobs.Job, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, e := s.Store.Job(j.ID)
	if e != nil {
		return e
	}
	cur.State = "error"
	return s.persist(cur, policy.Action("manager_incident", cur, map[string]string{"reason": reason}))
}
func (s *Server) checkCompletion(ctx context.Context, id string) error {
	j, e := s.Store.Job(id)
	if e != nil {
		return e
	}
	if j.Paused || len(j.CompletionCommand) == 0 || j.State != "result_ready" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, j.CompletionCommand[0], j.CompletionCommand[1:]...)
	cmd.Dir = j.Cwd
	b, e := cmd.CombinedOutput()
	if e != nil {
		_ = s.update(id, func(j *jobs.Job) { j.LastError = "completion check: " + cr.SafeText(string(b)) })
		return s.queueIncident(j, "completion check failed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e = s.Store.Job(id)
	if e != nil {
		return e
	}
	if j.Paused {
		return nil
	}
	j.State = "complete"
	return s.persist(j, policy.Action("cleanup", j, nil))
}
