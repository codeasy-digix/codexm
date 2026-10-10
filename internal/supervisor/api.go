package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/policy"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

type NewArgs struct {
	Model             string   `json:"model,omitempty"`
	Effort            string   `json:"effort,omitempty"`
	Title             string   `json:"title"`
	Cwd               string   `json:"cwd"`
	Kind              string   `json:"kind"`
	Account           string   `json:"account"`
	Prompt            string   `json:"prompt"`
	ParentID          string   `json:"parent_id"`
	ReadOnly          bool     `json:"read_only"`
	Goal              string   `json:"goal"`
	CompletionCommand []string `json:"completion_command"`
}
type JobArgs struct {
	ID        string          `json:"id"`
	Text      string          `json:"text"`
	Steer     bool            `json:"steer"`
	ThreadID  string          `json:"thread_id"`
	Account   string          `json:"account"`
	Manage    bool            `json:"manage"`
	Kind      string          `json:"kind"`
	Search    string          `json:"search"`
	Cursor    string          `json:"cursor"`
	ClientID  string          `json:"client_id"`
	Limit     int             `json:"limit,omitempty"`
	From      string          `json:"from,omitempty"`
	RequestID json.RawMessage `json:"request_id"`
	Result    json.RawMessage `json:"result"`
}

func (s *Server) Handle(ctx context.Context, call Call) (any, error) {
	if s.isStopping() {
		switch call.Command {
		case "new", "resume", "resume-job", "manage", "record-handoff", "send", "goal", "manager", "reply", "set-manager", "resolve-action":
			return nil, errors.New("supervisor is shutting down; restart it before submitting work")
		}
	}
	var a JobArgs
	if len(call.Args) > 0 && string(call.Args) != "null" {
		if e := json.Unmarshal(call.Args, &a); e != nil {
			return nil, e
		}
	}
	switch call.Command {
	case "ping":
		return map[string]any{"version": config.Version}, nil
	case "status":
		return s.status()
	case "client":
		s.mu.Lock()
		if a.ClientID != "" {
			s.viewers[a.ClientID] = time.Now()
		}
		s.mu.Unlock()
		return true, nil
	case "detach":
		s.mu.Lock()
		delete(s.viewers, a.ClientID)
		s.mu.Unlock()
		return true, nil
	case "events":
		return s.Store.Events(a.ID, 100)
	case "logs":
		return s.Store.WorkLogs(a.ID, a.Limit)
	case "record-handoff":
		if a.From != "unknown" && !config.Nickname.MatchString(a.From) {
			return nil, errors.New("invalid previous account")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		j, e := s.Store.Job(a.ID)
		if e != nil {
			return nil, e
		}
		change := jobs.WorkChange{ID: jobs.ID(), JobID: j.ID, ThreadID: j.ThreadID, From: a.From, To: j.Account, At: time.Now(), Reason: "기존 터미널에서 codexm으로 인계 (이전 사용량 미측정)"}
		return change, s.Store.ChangeAccount(j, change)
	case "accounts":
		return s.refreshAccounts(ctx)
	case "usage":
		return s.usage()
	case "settings":
		c := s.Config
		c.Manager.Account = s.manager()
		return c, nil
	case "set-manager":
		if !config.Nickname.MatchString(a.Account) {
			return nil, errors.New("invalid manager account")
		}
		snap := s.Reader.Read(ctx, a.Account)
		if !snap.Authenticated || snap.Error != "" {
			return nil, fmt.Errorf("manager account: %s", snap.Error)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		next := s.Config
		next.Manager.Account = a.Account
		if e := next.Save(); e != nil {
			return nil, e
		}
		s.managerAccount = a.Account
		return a.Account, nil
	case "new":
		var n NewArgs
		if e := json.Unmarshal(call.Args, &n); e != nil {
			return nil, e
		}
		return s.newJob(n)
	case "threads":
		p, e := s.Resolver.Probe(ctx, s.manager())
		if e != nil {
			return nil, e
		}
		defer p.Close()
		ts, cursor, e := p.List(ctx, a.Search, a.Cursor)
		return map[string]any{"threads": ts, "cursor": cursor}, e
	case "read":
		if a.ThreadID == "" {
			j, e := s.Store.Job(a.ID)
			if e != nil {
				return nil, e
			}
			a.ThreadID = j.ThreadID
		}
		p, e := s.Resolver.Probe(ctx, s.manager())
		if e != nil {
			return nil, e
		}
		defer p.Close()
		t, cursor, e := p.Recent(ctx, a.ThreadID, 20, a.Cursor)
		if e != nil {
			return nil, e
		}
		g, e := p.Goal(ctx, a.ThreadID)
		return map[string]any{"thread": t, "goal": g, "cursor": cursor}, e
	case "resume":
		return s.importThread(ctx, a)
	case "send":
		if strings.TrimSpace(a.Text) == "" {
			return nil, errors.New("empty input")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		j, e := s.Store.Job(a.ID)
		if e != nil {
			return nil, e
		}
		if !j.Managed {
			return nil, errors.New("read-only imported conversation; use resume-job to take ownership")
		}
		if j.Kind == "group" {
			return nil, errors.New("select a child conversation or send a manager instruction for this group")
		}
		if j.Paused {
			return nil, errors.New("job is paused; resume it before sending")
		}
		if a.Steer && (j.TurnID == "" || j.State != "running") {
			return nil, errors.New("steering requires a running turn")
		}
		input := jobs.Input{Text: a.Text, Steer: a.Steer, MessageID: jobs.ID()}
		action := policy.Action("send_input", j, input)
		j.LastInput = a.Text
		if jobs.Terminal(j.State) {
			j.State = "idle"
			if j.Goal != nil && j.Goal.Status == "complete" {
				j.Kind = "chat"
			}
		}
		if e = s.persist(j, action); e != nil {
			return nil, e
		}
		return action, nil
	case "pause", "stop":
		s.mu.Lock()
		j, e := s.Store.Job(a.ID)
		if e != nil {
			s.mu.Unlock()
			return nil, e
		}
		j.Paused = true
		if j.TurnID != "" {
			j.ContinuationDue = true
		}
		if call.Command == "stop" {
			j.State = "stopped"
		} else {
			j.State = "paused"
		}
		e = s.persist(j, policy.Action("pause_execution", j, nil))
		s.mu.Unlock()
		return j, e
	case "resume-job":
		s.mu.Lock()
		defer s.mu.Unlock()
		j, e := s.Store.Job(a.ID)
		if e != nil {
			return nil, e
		}
		j.Managed = true
		if slices.Contains([]string{"error", "quota_wait", "retry_wait", "auth_required", "needs_user", "manager_wait"}, j.State) {
			j.ContinuationDue = true
		}
		j.Paused = false
		j.State = "queued"
		j.LastError = ""
		if e = s.persist(j, policy.Action("start", j, map[string]any{"origin": "user"})); e != nil {
			return nil, e
		}
		return j, nil
	case "manage":
		s.mu.Lock()
		defer s.mu.Unlock()
		j, e := s.Store.Job(a.ID)
		if e != nil {
			return nil, e
		}
		if j.Managed {
			return j, nil
		}
		if cr.WriterBusy(j.Store, j.ThreadID) {
			return nil, errForeignWriter
		}
		j.Managed = true
		j.State = "idle"
		if j.Goal != nil {
			j.Paused = true
			j.State = "paused"
			if j.Goal.Status == "complete" {
				j.Paused = false
				j.State = "complete"
			}
		}
		return j, s.persist(j)
	case "complete":
		s.mu.Lock()
		defer s.mu.Unlock()
		j, e := s.Store.Job(a.ID)
		if e != nil {
			return nil, e
		}
		if j.Kind == "goal" && j.Goal != nil && j.Goal.Status != "complete" {
			return nil, errors.New("finish the Codex goal before completing this job")
		}
		j.State = "complete"
		if j.TurnID != "" {
			return nil, errors.New("wait for the active turn before marking complete")
		}
		return j, s.persist(j, policy.Action("cleanup", j, nil))
	case "attach":
		j, e := s.Store.Job(a.ID)
		if e != nil {
			return nil, e
		}
		if j.Socket == "" || !cr.HasSession(ctx, j.Session) {
			return nil, errors.New("job runtime is not loaded; use resume-job or send an instruction")
		}
		return map[string]any{"binary": j.EngineBinary, "socket": j.Socket, "thread_id": j.ThreadID, "session": j.Session, "attach": "tmux attach -t " + j.Session}, nil
	case "reply":
		return s.reply(ctx, a)
	case "goal":
		s.mu.Lock()
		j, e := s.Store.Job(a.ID)
		if e != nil {
			s.mu.Unlock()
			return nil, e
		}
		if j.Kind == "group" || !j.Managed {
			s.mu.Unlock()
			return nil, errors.New("a goal requires a managed conversation")
		}
		j.Kind = "goal"
		action := policy.Action("set_goal", j, map[string]string{"objective": a.Text})
		e = s.persist(j, action)
		s.mu.Unlock()
		return action, e
	case "manager":
		if strings.TrimSpace(a.Text) == "" {
			return nil, errors.New("empty manager instruction")
		}
		j, e := s.Store.Job(a.ID)
		if e != nil && a.ID != "" {
			return nil, e
		}
		action := policy.Action("manager_command", j, map[string]string{"text": a.Text})
		return action, s.Store.PutAction(action)
	case "shutdown":
		go s.shutdown()
		return true, nil
	case "resolve-action":
		var p struct {
			ActionID string `json:"action_id"`
			Decision string `json:"decision"`
		}
		if e := json.Unmarshal(call.Args, &p); e != nil {
			return nil, e
		}
		as, e := s.Store.Actions("outcome_unknown")
		if e != nil {
			return nil, e
		}
		for _, x := range as {
			if x.ID == p.ActionID {
				if p.Decision != "done" && p.Decision != "cancelled" {
					return nil, errors.New("inspect history, then resolve as done or cancelled")
				}
				x.State = p.Decision
				return x, s.Store.PutAction(x)
			}
		}
		return nil, errors.New("uncertain action not found")
	default:
		return nil, fmt.Errorf("unknown command %q", call.Command)
	}
}
func (s *Server) newJob(n NewArgs) (jobs.Job, error) {
	if n.Kind == "" {
		n.Kind = "chat"
	}
	if !jobs.ValidKind(n.Kind) {
		return jobs.Job{}, errors.New("kind must be chat, task, goal or group")
	}
	if n.Kind == "goal" && n.Goal == "" {
		return jobs.Job{}, errors.New("a new goal job requires an objective")
	}
	if n.Kind == "group" && (n.Prompt != "" || n.Goal != "") {
		return jobs.Job{}, errors.New("a group organizes child jobs; send its instructions to the manager")
	}
	if n.Cwd == "" {
		return jobs.Job{}, errors.New("a workspace directory is required")
	}
	p, e := filepath.Abs(config.Expand(n.Cwd))
	if e != nil {
		return jobs.Job{}, e
	}
	p, e = filepath.EvalSymlinks(p)
	if e != nil {
		return jobs.Job{}, e
	}
	info, e := os.Stat(p)
	if e != nil || !info.IsDir() {
		return jobs.Job{}, errors.New("workspace directory is unavailable")
	}
	if n.Account == "" {
		n.Account = s.Config.WorkerAccounts.Priority[0]
	}
	if !slices.Contains(s.Config.WorkerAccounts.Priority, n.Account) {
		return jobs.Job{}, errors.New("worker account is not in the configured priority list")
	}
	if n.Title == "" {
		n.Title = filepath.Base(p)
	}
	if len([]rune(n.Title)) > 150 {
		return jobs.Job{}, errors.New("title is too long")
	}
	host, _ := os.Hostname()
	now := time.Now()
	j := jobs.Job{ID: jobs.ID(), ParentID: n.ParentID, Title: n.Title, Kind: n.Kind, Cwd: p, Host: host, Store: config.Expand(s.Config.Runtime.SharedCodexHome), Account: n.Account, InitialAccount: n.Account, State: "queued", Managed: true, ReadOnly: n.ReadOnly, CompletionCommand: n.CompletionCommand, CreatedAt: now, UpdatedAt: now, LastInput: n.Prompt}
	j.Model = n.Model
	j.Effort = n.Effort
	if n.Effort != "" && !slices.Contains([]string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}, n.Effort) {
		return j, errors.New("invalid reasoning effort")
	}
	if n.Goal != "" {
		if len([]rune(n.Goal)) > 4000 {
			return j, errors.New("goal objective exceeds 4000 characters")
		}
		j.Goal = &jobs.Goal{Objective: n.Goal, Status: "active"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all, e := s.Store.Jobs()
	if e != nil {
		return j, e
	}
	if e = policy.ValidateParent(j, all, s.Config.Execution.MaxChildDepth); e != nil {
		return j, e
	}
	as := []jobs.Action{}
	if n.Kind != "group" {
		as = append(as, policy.Action("start", j, map[string]any{"new": true, "origin": "user"}))
	} else {
		j.State = "idle"
	}
	if n.Prompt != "" {
		as = append(as, policy.Action("send_input", j, jobs.Input{Text: n.Prompt, MessageID: jobs.ID()}))
	}
	return j, s.persist(j, as...)
}
func (s *Server) importThread(ctx context.Context, a JobArgs) (jobs.Job, error) {
	if a.ThreadID == "" {
		return jobs.Job{}, errors.New("thread ID required")
	}
	all, e := s.Store.Jobs()
	if e != nil {
		return jobs.Job{}, e
	}
	for _, j := range all {
		if j.ThreadID == a.ThreadID {
			if a.Manage {
				v, e := s.Handle(ctx, Call{Command: "resume-job", Args: mustJSON(JobArgs{ID: j.ID})})
				if e != nil {
					return j, e
				}
				return v.(jobs.Job), nil
			}
			return j, nil
		}
	}
	p, e := s.Resolver.Probe(ctx, s.manager())
	if e != nil {
		return jobs.Job{}, e
	}
	defer p.Close()
	t, _, e := p.Recent(ctx, a.ThreadID, 3, "")
	if e != nil {
		return jobs.Job{}, e
	}
	g, e := p.Goal(ctx, a.ThreadID)
	if e != nil {
		return jobs.Job{}, e
	}
	if a.Kind == "" {
		a.Kind = "chat"
		if g != nil {
			a.Kind = "goal"
		}
	}
	if !jobs.ValidKind(a.Kind) {
		return jobs.Job{}, errors.New("invalid job kind")
	}
	if a.Kind == "group" {
		return jobs.Job{}, errors.New("an existing conversation cannot be imported as a group")
	}
	if a.Account == "" {
		a.Account = s.Config.WorkerAccounts.Priority[0]
	}
	if !slices.Contains(s.Config.WorkerAccounts.Priority, a.Account) {
		return jobs.Job{}, errors.New("account is not in worker priority")
	}
	host, _ := os.Hostname()
	input, output := cr.LastMessages(t)
	title := t.Name
	if title == "" {
		title = t.Preview
	}
	if title == "" {
		title = a.ThreadID
	}
	if len([]rune(title)) > 120 {
		title = string([]rune(title)[:120])
	}
	j := jobs.Job{ID: jobs.ID(), ThreadID: t.ID, Host: host, Store: config.Expand(s.Config.Runtime.SharedCodexHome), Cwd: t.Cwd, Title: title, Kind: a.Kind, Account: a.Account, InitialAccount: a.Account, Managed: a.Manage, State: "observed", Goal: g, LastInput: input, LastOutput: output, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if g != nil && g.Status == "complete" {
		j.State = "complete"
	}
	if a.Manage {
		j.State = "queued"
	}
	// Native subagents are controlled through their parent engine, not restarted independently.
	if strings.Contains(string(t.Source), "subAgent") && a.Manage {
		return j, errors.New("native subagent must be managed through its parent conversation")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	as := []jobs.Action{}
	if a.Manage {
		as = append(as, policy.Action("start", j, map[string]string{"origin": "user"}))
	}
	return j, s.persist(j, as...)
}
func (s *Server) refreshAccounts(ctx context.Context) ([]accounts.Snapshot, error) {
	s.accountMu.Lock()
	defer s.accountMu.Unlock()
	names, e := accounts.List(s.Config)
	if e != nil {
		return nil, e
	}
	raw, e := s.Store.Snapshots()
	if e != nil {
		return nil, e
	}
	prev := map[string]accounts.Snapshot{}
	for _, b := range raw {
		var a accounts.Snapshot
		if json.Unmarshal(b, &a) == nil {
			prev[a.Account] = a
		}
	}
	out := []accounts.Snapshot{}
	for _, name := range names {
		a := s.Reader.Read(ctx, name)
		a.WasExhausted = prev[name].WasExhausted || a.State("") == "exhausted"
		if a.Ordinary != nil && *a.Ordinary && a.State("") == "available" {
			a.WasExhausted = false
		}
		if e = s.Store.Snapshot(name, a); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, nil
}
func (s *Server) reply(ctx context.Context, a JobArgs) (any, error) {
	s.mu.Lock()
	j, e := s.Store.Job(a.ID)
	c := s.clients[a.ID]
	s.mu.Unlock()
	if e != nil {
		return nil, e
	}
	if c == nil {
		return nil, errors.New("runtime is disconnected; unresolved input must be inspected in the original chat")
	}
	if j.Paused {
		return nil, errors.New("job is paused; resume before responding")
	}
	var req *jobs.Request
	for i := range j.PendingRequests {
		if string(j.PendingRequests[i].ID) == string(a.RequestID) && j.PendingRequests[i].RunID == j.RunID {
			req = &j.PendingRequests[i]
			break
		}
	}
	if req == nil {
		return nil, errors.New("pending request not found in current run")
	}
	if e = cr.ValidateReply(*req, a.Result); e != nil {
		return nil, e
	}
	var result any
	if e = json.Unmarshal(a.Result, &result); e != nil {
		return nil, e
	}
	if e = c.Reply(req.ID, result); e != nil {
		return nil, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e = s.Store.Job(a.ID)
	if e != nil {
		return nil, e
	}
	rs := []jobs.Request{}
	for _, r := range j.PendingRequests {
		if string(r.ID) != string(req.ID) {
			rs = append(rs, r)
		}
	}
	j.PendingRequests = rs
	j.State = "running"
	return true, s.persist(j)
}
func (s *Server) usage() (any, error) {
	rows, e := s.Store.DB.Query("SELECT data FROM manager_decisions ORDER BY at DESC LIMIT 30")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var b string
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		out = append(out, json.RawMessage(b))
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	js, e := s.Store.Jobs()
	if e != nil {
		return nil, e
	}
	workers := []map[string]any{}
	for _, j := range js {
		workers = append(workers, map[string]any{"job_id": j.ID, "thread_id": j.ThreadID, "account": j.Account, "reported_usage": j.TokenUsage, "goal": j.Goal})
	}
	return map[string]any{"manager": out, "workers": workers}, nil
}
func (s *Server) shutdown() {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return
	}
	s.stopping = true
	s.mu.Unlock()
	js, _ := s.Store.Jobs()
	for _, j := range js {
		if !j.Managed {
			continue
		}
		if jobs.Terminal(j.State) {
			if j.Session != "" {
				s.mu.Lock()
				j.Paused = true
				_ = s.persist(j, policy.Action("pause_execution", j, nil))
				s.mu.Unlock()
			}
			continue
		}
		_, _ = s.Handle(context.Background(), Call{Command: "stop", Args: mustJSON(JobArgs{ID: j.ID})})
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		n := len(s.clients)
		s.mu.Unlock()
		if n == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.cancel()
}
func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
