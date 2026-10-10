package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/policy"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/store"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeReader map[string]accounts.Snapshot

func (r fakeReader) Read(_ context.Context, n string) accounts.Snapshot {
	a := r[n]
	a.Account = n
	a.CheckedAt = time.Now()
	return a
}
func fixture(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	c := config.Defaults()
	c.Manager.Account = "account-backup"
	c.Supervisor.WorkerSessionPrefix = "codexm-test-job-"
	c.Path = filepath.Join(dir, "config.toml")
	c.Storage.StateDir = filepath.Join(dir, "state")
	c.Runtime.AccountsDir = filepath.Join(dir, "accounts")
	c.Runtime.SharedCodexHome = filepath.Join(dir, "codex")
	for _, n := range c.WorkerAccounts.Priority {
		p := filepath.Join(c.Runtime.AccountsDir, n)
		os.MkdirAll(p, 0700)
		os.WriteFile(filepath.Join(p, "auth.json"), []byte(`{}`), 0600)
	}
	st, e := store.Open(c.StateDir())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); st.Close() })
	return &Server{Config: c, Store: st, ctx: ctx, cancel: cancel, clients: map[string]*cr.Client{}, dispatching: map[string]bool{}, viewers: map[string]viewer{}, managerAccount: "account-backup", Reader: fakeReader{}, started: time.Now()}
}

func TestManageFinishedConversationDoesNotRestartOrCallModel(t *testing.T) {
	s := fixture(t)
	for _, kind := range []string{"chat", "goal"} {
		j := jobs.Job{ID: jobs.ID(), Kind: kind, State: "observed", Store: s.Config.Runtime.SharedCodexHome, ThreadID: jobs.ID()}
		if kind == "goal" {
			j.Goal = &jobs.Goal{Status: "complete", TokensUsed: 123}
		}
		if e := s.Store.PutJob(j); e != nil {
			t.Fatal(e)
		}
		if _, e := s.Handle(context.Background(), Call{Command: "manage", Args: mustJSON(JobArgs{ID: j.ID})}); e != nil {
			t.Fatal(e)
		}
		got, _ := s.Store.Job(j.ID)
		if !got.Managed || got.Session != "" || got.RunID != "" || kind == "goal" && (got.State != "complete" || got.Goal.TokensUsed != 123) || kind == "chat" && got.State != "idle" {
			t.Fatal(got)
		}
	}
	as, _ := s.Store.Actions("")
	n, _, _ := s.Store.DecisionCounts("")
	if len(as) != 0 || n != 0 {
		t.Fatal("managing history started work", as, n)
	}
}
func TestFallbackRecheckUsesSavedDeadlineAndDoesNotCallManager(t *testing.T) {
	s := fixture(t)
	yes, no := true, false
	s.Reader = fakeReader{"account-work": {Authenticated: true, Ordinary: &yes}, "account-secondary": {Authenticated: true, Ordinary: &no}, "account-main": {Authenticated: true, Ordinary: &no}, "account-backup": {Authenticated: true, Ordinary: &yes}}
	j := jobs.Job{ID: jobs.ID(), Managed: true, Kind: "chat", State: "running", Account: "account-backup", Cwd: t.TempDir(), LastAccountCheck: time.Now().Add(-599 * time.Second)}
	s.Store.PutJob(j)
	s.reconcileJob(j)
	as, _ := s.Store.Actions("pending")
	if len(as) != 0 {
		t.Fatal("checked preferred accounts before 600 seconds")
	}
	j.LastAccountCheck = time.Now().Add(-601 * time.Second)
	s.Store.PutJob(j)
	s.reconcileJob(j)
	as, _ = s.Store.Actions("pending")
	if len(as) != 1 || as[0].Kind != "prefer_account" {
		t.Fatal(as)
	}
	hour, _, _ := s.Store.DecisionCounts("")
	if hour != 0 {
		t.Fatal("normal recovery used manager tokens")
	}
	var p struct {
		Account string `json:"account"`
	}
	json.Unmarshal(as[0].Payload, &p)
	if p.Account != "account-work" {
		t.Fatal(p)
	}
}
func TestPauseAndWorkspaceReservationsPreventAutomaticEdits(t *testing.T) {
	s := fixture(t)
	cwd := t.TempDir()
	j := jobs.Job{ID: jobs.ID(), Kind: "chat", Managed: true, State: "idle", Cwd: cwd}
	s.Store.PutJob(j)
	other := jobs.Job{ID: jobs.ID(), Managed: true, State: "running", Cwd: filepath.Join(cwd, "child")}
	s.Store.PutJob(other)
	if s.reserve(j.ID) == nil {
		t.Fatal("overlapping edit allowed")
	}
	j.Paused = true
	j.State = "paused"
	s.Store.PutJob(j)
	a := policy.Action("send_input", j, jobs.Input{Text: "work", MessageID: jobs.ID()})
	s.Store.PutAction(a)
	s.execute(a)
	pending, _ := s.Store.Actions("pending")
	if len(pending) != 1 {
		t.Fatal("paused instruction dispatched")
	}
}
func TestRetryDeadlineSurvivesFrequentStateRefresh(t *testing.T) {
	s := fixture(t)
	j := jobs.Job{ID: jobs.ID(), Kind: "chat", Managed: true, State: "retry_wait", Cwd: t.TempDir(), Account: "account-work", UpdatedAt: time.Now()}
	if e := s.Store.PutJob(j); e != nil {
		t.Fatal(e)
	}
	// No account is authenticated here: dispatch must fail at the auth check,
	// rather than be postponed forever by the freshly updated job timestamp.
	e := s.retry(context.Background(), j.ID, time.Now().Add(-6*time.Second))
	var p *postpone
	if errors.As(e, &p) {
		t.Fatal("expired retry was postponed", e)
	}
	got, _ := s.Store.Job(j.ID)
	if got.RetryCount != 1 || got.State != "auth_required" {
		t.Fatal(got.RetryCount, got.State, e)
	}
	j.State = "retry_wait"
	j.RetryCount = 0
	s.Store.PutJob(j)
	e = s.retry(context.Background(), j.ID, time.Now())
	if !errors.As(e, &p) || p.delay <= 0 || p.delay > 5*time.Second {
		t.Fatal("retry did not respect the original action deadline", e)
	}
}
func TestGroupInstructionsRequireManagerAndNeverDispatchToNilRuntime(t *testing.T) {
	s := fixture(t)
	n := NewArgs{Kind: "group", Cwd: t.TempDir(), Title: "group", Prompt: "create children"}
	if _, e := s.newJob(n); e == nil {
		t.Fatal("direct group prompt accepted")
	}
	n.Prompt = ""
	j, e := s.newJob(n)
	if e != nil {
		t.Fatal(e)
	}
	for _, command := range []string{"send", "goal"} {
		if _, e = s.Handle(context.Background(), Call{Command: command, Args: mustJSON(JobArgs{ID: j.ID, Text: "work"})}); e == nil {
			t.Fatal(command, "created a runtime action for a group")
		}
	}
	as, _ := s.Store.Actions("")
	if len(as) != 0 {
		t.Fatal(as)
	}
	s.stopping = true
	if _, e = s.Handle(context.Background(), Call{Command: "new", Args: mustJSON(n)}); e == nil {
		t.Fatal("job accepted while shutting down")
	}
}
func TestGoalCompletionAndLateOldRunDoNotOverwriteNewRun(t *testing.T) {
	s := fixture(t)
	j := jobs.Job{ID: jobs.ID(), RunID: "new", ThreadID: "t", State: "running", Managed: true, Kind: "goal"}
	s.Store.PutJob(j)
	s.handleEvent(j.ID, "old", "old-event", cr.Message{Method: "error", Params: json.RawMessage(`{"threadId":"t","error":{"message":"quota","codexErrorInfo":"usageLimitExceeded"}}`)})
	got, _ := s.Store.Job(j.ID)
	if got.State != "running" {
		t.Fatal(got)
	}
	s.handleEvent(j.ID, "new", "complete", cr.Message{Method: "thread/goal/updated", Params: json.RawMessage(`{"threadId":"t","goal":{"objective":"done","status":"complete"}}`)})
	got, _ = s.Store.Job(j.ID)
	if got.State != "complete" {
		t.Fatal(got)
	}
	as, _ := s.Store.Actions("pending")
	if len(as) != 1 || as[0].Kind != "cleanup" {
		t.Fatal(as)
	}
}
func TestOfficialNativeWorkerLifecycleAndWriterLock(t *testing.T) {
	if os.Getenv("CODEXM_NATIVE_TEST") != "1" {
		t.Skip("requires official CLI and CODEXM_BINARY")
	}
	self := os.Getenv("CODEXM_BINARY")
	if self == "" {
		t.Fatal("CODEXM_BINARY missing")
	}
	s := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	r, e := cr.Resolve(ctx, s.Config)
	if e != nil {
		t.Fatal(e)
	}
	s.Resolver = r
	s.Self = self
	yes := true
	s.Reader = fakeReader{"account-work": {Authenticated: true, Ordinary: &yes}, "account-secondary": {Authenticated: true, Ordinary: &yes}}
	if e = s.Config.Save(); e != nil {
		t.Fatal(e)
	}
	j, e := s.newJob(NewArgs{Cwd: t.TempDir(), Title: "native isolated test", Kind: "chat", Account: "account-work", ReadOnly: true, Model: "gpt-6.1-sol", Effort: "low"})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { s.park(context.Background(), j.ID, false); s.cancel(); s.wg.Wait() }()
	if e = s.ensureRuntime(ctx, j.ID, false); e != nil {
		t.Fatal(e)
	}
	initialActions, _ := s.Store.Actions("pending")
	for _, a := range initialActions {
		if a.Kind == "start" {
			s.finishAction(a, "done", nil)
		}
	}
	j, _ = s.Store.Job(j.ID)
	if j.ThreadID == "" || j.State != "idle" || !cr.HasSession(ctx, j.Session) || !cr.WriterBusy(j.Store, j.ThreadID) {
		t.Fatal(j)
	}
	// Goal persistence without any model generation.
	c := s.client(j.ID)
	thread, e := c.Read(ctx, j.ThreadID, false)
	if e != nil || thread.Name != "native isolated test" || thread.Path == "" {
		t.Fatal("new empty chat was not named and persisted", e)
	}
	if _, e = os.Stat(thread.Path); e != nil {
		t.Fatal("new empty chat has no resume file", e)
	}
	if e = c.Call(ctx, "thread/resume", map[string]any{"threadId": j.ThreadID, "path": thread.Path, "excludeTurns": true}, nil); e != nil {
		t.Fatal("official TUI bootstrap cannot resume an empty chat", e)
	}
	if e = c.Call(ctx, "thread/goal/set", map[string]any{"threadId": j.ThreadID, "objective": "native test", "status": "paused", "origin": "user", "tokenBudget": 30000}, nil); e != nil {
		t.Fatal(e)
	}
	g, e := c.Goal(ctx, j.ThreadID)
	if e != nil || g.Status != "paused" {
		t.Fatal(g, e)
	}
	if e = s.park(ctx, j.ID, false); e != nil {
		t.Fatal(e)
	}
	if cr.WriterBusy(j.Store, j.ThreadID) || cr.HasSession(ctx, j.Session) {
		t.Fatal("worker not cleaned up")
	}
	_ = s.update(j.ID, func(j *jobs.Job) { j.Paused = false; j.State = "queued" })
	if e = s.changeAccount(j.ID, "account-secondary", "검증용 계정 교체"); e != nil {
		t.Fatal(e)
	}
	if e = s.ensureRuntime(ctx, j.ID, false); e != nil {
		t.Fatal(e)
	}
	loaded, _ := s.Store.Job(j.ID)
	if loaded.ThreadID != j.ThreadID || loaded.Account != "account-secondary" || loaded.Model != j.Model || loaded.Effort != j.Effort || loaded.Goal == nil || loaded.Goal.Status != "paused" || loaded.Goal.TokenBudget == nil || *loaded.Goal.TokenBudget != 30000 || loaded.Goal.TokensUsed != g.TokensUsed {
		t.Fatal(loaded)
	}
	log, e := s.Store.WorkLogs(j.ID, 10)
	if e != nil || len(log.Runs) != 2 || len(log.Changes) != 1 || log.Changes[0].From != "account-work" || log.Changes[0].To != "account-secondary" {
		t.Fatal("account switch history", log, e)
	}
	for _, run := range log.Runs {
		if run.StartedAt.IsZero() || run.ThreadID != j.ThreadID {
			t.Fatal("run start or thread lost", run)
		}
	}
	if log.Runs[1].EndedAt == nil || log.Runs[1].Account != "account-work" || log.Runs[0].Account != "account-secondary" {
		t.Fatal("run boundary missing", log.Runs)
	}
	// Opening a finished goal must preserve native counters and keep its engine
	// available for the UI without scheduling any additional model work.
	c = s.client(j.ID)
	if e = c.SetGoalStatus(ctx, j.ThreadID, "complete", "user"); e != nil {
		t.Fatal(e)
	}
	g, e = c.Goal(ctx, j.ThreadID)
	if e != nil || g.Status != "complete" {
		t.Fatal("goal completion", e)
	}
	if e = s.park(ctx, j.ID, false); e != nil {
		t.Fatal(e)
	}
	_ = s.update(j.ID, func(j *jobs.Job) { j.State = "complete"; j.Paused = false; j.Goal = g })
	if _, e = s.Handle(ctx, Call{Command: "view", Args: mustJSON(JobArgs{ID: j.ID, ClientID: "native-view-test"})}); e != nil {
		t.Fatal(e)
	}
	as, _ := s.Store.Actions("pending")
	for _, a := range as {
		if a.Kind == "load_view" {
			s.execute(a)
		}
	}
	loaded, _ = s.Store.Job(j.ID)
	s.mu.Lock()
	v := s.viewState(loaded)
	s.mu.Unlock()
	if !v.Ready || loaded.State != "complete" || loaded.Goal.TokensUsed != g.TokensUsed || loaded.Goal.TokenBudget == nil || *loaded.Goal.TokenBudget != 30000 {
		t.Fatalf("view changed goal state or lost counters: ready=%v state=%s error=%s goal-status=%s used=%d expected=%d budget=%v", v.Ready, loaded.State, loaded.LastError, loaded.Goal.Status, loaded.Goal.TokensUsed, g.TokensUsed, loaded.Goal.TokenBudget)
	}
	as, _ = s.Store.Actions("pending")
	for _, a := range as {
		if a.Kind == "goal_continue" || a.Kind == "send_input" || a.Kind == "set_goal" {
			t.Fatal("view queued model generation", a.Kind)
		}
		if a.Kind == "cleanup" {
			s.execute(a)
		}
	}
	if !cr.HasSession(ctx, loaded.Session) {
		t.Fatal("completed goal lost its attached terminal runtime")
	}
}
