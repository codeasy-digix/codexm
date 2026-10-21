package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/manager"
	"github.com/codeasy-digix/codexm/internal/policy"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/service"
	"github.com/codeasy-digix/codexm/internal/store"
	"golang.org/x/sys/unix"
)

type Server struct {
	Config         config.Config
	Resolver       cr.Resolver
	Store          *store.Store
	Reader         AccountReader
	Self           string
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.Mutex
	clients        map[string]*cr.Client
	viewers        map[string]viewer
	unhealthy      string
	stopping       bool
	started        time.Time
	dispatching    map[string]bool
	importMu       sync.Mutex
	accountMu      sync.Mutex
	managerAccount string
	wg             sync.WaitGroup
	previews       map[string]preview
	Persistent     bool
	lease          *os.File
}
type preview struct{ RunID, Text string }
type viewer struct {
	At    time.Time
	JobID string
}
type AccountReader interface {
	Read(context.Context, string) accounts.Snapshot
}
type Call struct {
	Command string          `json:"command"`
	Args    json.RawMessage `json:"args"`
}
type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}
type Status struct {
	Jobs                 []jobs.Job          `json:"jobs"`
	Accounts             []accounts.Snapshot `json:"accounts"`
	ManagerAccount       string              `json:"manager_account"`
	WorkerPriority       []string            `json:"priority"`
	NativeDefaultAccount string              `json:"native_default_account,omitempty"`
	ManagerCalls         int                 `json:"manager_calls_last_hour"`
	Health               string              `json:"health"`
	Session              string              `json:"session"`
	Attach               string              `json:"attach"`
	Version              string              `json:"version"`
	CodexVersion         string              `json:"codex_version"`
	ManagerDecision      *manager.Record     `json:"manager_decision,omitempty"`
	Terminals            map[string]int      `json:"terminals,omitempty"`
	ServiceMode          bool                `json:"service_mode"`
}

func New(ctx context.Context, c config.Config, self string) (*Server, error) {
	if c.Manager.Account == "" {
		return nil, errors.New("choose a manager account first: codexm setup")
	}
	if e := os.MkdirAll(c.StateDir(), 0700); e != nil {
		return nil, e
	}
	lease, e := os.OpenFile(filepath.Join(c.StateDir(), "supervisor.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = unix.Flock(int(lease.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		lease.Close()
		return nil, errors.New("another supervisor already owns this state directory")
	}
	r, e := cr.Resolve(ctx, c)
	if e != nil {
		lease.Close()
		return nil, e
	}
	st, e := store.Open(c.StateDir())
	if e != nil {
		lease.Close()
		return nil, e
	}
	if e = st.RecoverOperations(); e != nil {
		st.Close()
		lease.Close()
		return nil, e
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Server{Config: c, Resolver: r, Reader: accounts.New(r), Store: st, Self: self, ctx: ctx, cancel: cancel, clients: map[string]*cr.Client{}, viewers: map[string]viewer{}, dispatching: map[string]bool{}, managerAccount: c.Manager.Account, started: time.Now()}
	s.lease = lease
	return s, nil
}
func Socket(c config.Config) string { return cr.SocketPath(c, "control") }
func (s *Server) Run() error {
	defer s.cancel()
	lock := s.lease
	var e error
	if lock == nil {
		lock, e = os.OpenFile(filepath.Join(s.Config.StateDir(), "supervisor.lock"), os.O_CREATE|os.O_RDWR, 0600)
		if e != nil {
			return e
		}
	}
	defer lock.Close()
	defer s.Store.Close()
	if e = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		return errors.New("another supervisor already owns this state directory")
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	if e = s.Store.RecoverActions(); e != nil {
		return e
	}
	socket := Socket(s.Config)
	if e = cr.PrivateSocketDir(filepath.Dir(socket)); e != nil {
		return e
	}
	if st, e := os.Lstat(socket); e == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("control path is not a socket: %s", socket)
		}
		if e = os.Remove(socket); e != nil {
			return e
		}
	}
	l, e := net.Listen("unix", socket)
	if e != nil {
		return e
	}
	defer l.Close()
	_ = os.Chmod(socket, 0600)
	defer os.Remove(socket)
	httpServer := &http.Server{Handler: http.HandlerFunc(s.serveHTTP), ReadHeaderTimeout: 5 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- httpServer.Serve(l) }()
	s.wg.Add(3)
	go func() { defer s.wg.Done(); s.actionLoop() }()
	go func() { defer s.wg.Done(); s.reconcileLoop() }()
	go func() { defer s.wg.Done(); s.quotaNotificationLoop() }()
	fmt.Printf("codexm %s · manager service · persistent=%t\n", config.Version, s.Persistent)
	select {
	case <-s.ctx.Done():
	case e = <-errc:
		if errors.Is(e, http.ErrServerClosed) {
			e = nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
	s.cancel()
	s.wg.Wait()
	s.mu.Lock()
	for _, c := range s.clients {
		c.Close()
	}
	s.clients = map[string]*cr.Client{}
	s.mu.Unlock()
	// Service restarts release observer connections while keeping owned workers
	// alive. Explicit shutdown parks jobs before cancellation in the API handler.
	if s.Persistent && !s.isStopping() {
		return errors.New("manager observer exited; OS recovery will reconnect existing workers")
	}
	js, err := s.Store.Jobs()
	if err != nil {
		return err
	}
	for _, j := range js {
		if !j.Managed || j.Session == "" {
			continue
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if cr.HasSession(cleanupCtx, j.Session) {
			err = cr.KillSession(cleanupCtx, j.Session)
		}
		cleanupCancel()
		if err != nil {
			return err
		}
		if j.RunID != "" {
			if err = s.Store.EndRun(j.RunID, "shutdown", time.Now()); err != nil && !store.NotFound(err) {
				return err
			}
		}
		if j.Socket != "" {
			_ = os.Remove(j.Socket)
		}
		if err = s.update(j.ID, func(j *jobs.Job) { j.Session = ""; j.Socket = ""; j.TurnID = ""; j.PendingRequests = nil }); err != nil {
			return err
		}
	}
	return e
}
func (s *Server) isStopping() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.stopping }
func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || r.URL.Path != "/rpc" {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	var c Call
	if e := json.NewDecoder(r.Body).Decode(&c); e != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	result, e := s.Handle(r.Context(), c)
	w.Header().Set("Content-Type", "application/json")
	reply := Response{}
	if e != nil {
		reply.Error = cr.SafeText(e.Error())
	} else {
		reply.Result, _ = json.Marshal(result)
	}
	_ = json.NewEncoder(w).Encode(reply)
}
func (s *Server) fail(e error) {
	s.mu.Lock()
	s.unhealthy = e.Error()
	s.mu.Unlock()
	fmt.Fprintln(os.Stderr, "state error:", e)
}
func (s *Server) status() (Status, error) {
	js, e := s.Store.Jobs()
	if e != nil {
		return Status{}, e
	}
	raw, e := s.Store.Snapshots()
	if e != nil {
		return Status{}, e
	}
	as := []accounts.Snapshot{}
	for _, b := range raw {
		var a accounts.Snapshot
		if json.Unmarshal(b, &a) == nil {
			as = append(as, a)
		}
	}
	count, _, _ := s.Store.DecisionCounts("")
	s.mu.Lock()
	health := s.unhealthy
	account := s.managerAccount
	terminals := s.liveViewersLocked()
	for i := range js {
		if p, ok := s.previews[js[i].ID]; ok && p.RunID == js[i].RunID {
			js[i].LastOutput = p.Text
		}
	}
	s.mu.Unlock()
	if health == "" {
		health = "connected"
	}
	var latest string
	var decision *manager.Record
	if s.Store.DB.QueryRow("SELECT data FROM manager_decisions ORDER BY at DESC LIMIT 1").Scan(&latest) == nil {
		var r manager.Record
		if json.Unmarshal([]byte(latest), &r) == nil {
			decision = &r
		}
	}
	attach := "codexm service logs"
	if !s.Persistent {
		attach = "tmux attach -t " + s.Config.Supervisor.TmuxSession
	}
	return Status{Jobs: js, Accounts: as, ManagerAccount: account, WorkerPriority: s.workerConfig().WorkerAccounts.Priority, NativeDefaultAccount: accounts.CurrentNickname(s.Config, config.Expand(s.Config.Runtime.SharedCodexHome), ""), ManagerCalls: count, Health: health, Session: s.Config.Supervisor.TmuxSession, Attach: attach, Version: config.Version, CodexVersion: s.Resolver.Version, ManagerDecision: decision, Terminals: terminals, ServiceMode: s.Persistent}, nil
}
func (s *Server) manager() string { s.mu.Lock(); defer s.mu.Unlock(); return s.managerAccount }
func (s *Server) persist(j jobs.Job, actions ...jobs.Action) error {
	j.UpdatedAt = time.Now()
	return s.Store.Commit(j, nil, actions)
}
func (s *Server) watch(jobID, runID string, c *cr.Client) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		seq := uint64(0)
		for {
			select {
			case m := <-c.Events:
				if s.client(jobID) != c {
					continue
				}
				seq++
				s.handleEvent(jobID, runID, fmt.Sprintf("%s:%s:%d", runID, jobs.ID(), seq), m)
			case <-c.Done:
				// Drain already-received lifecycle events before recording disconnection.
				for {
					select {
					case m := <-c.Events:
						if s.client(jobID) != c {
							continue
						}
						seq++
						s.handleEvent(jobID, runID, fmt.Sprintf("%s:%s:%d", runID, jobs.ID(), seq), m)
					default:
						if s.client(jobID) == c {
							s.handleEvent(jobID, runID, jobs.ID(), cr.Message{Method: "connection/lost", Params: json.RawMessage(`{}`)})
						}
						return
					}
				}
			case <-s.ctx.Done():
				return
			}
		}
	}()
}
func (s *Server) handleEvent(jobID, runID, key string, m cr.Message) {
	// Thinking deltas are render-only. Final items remain available in official history.
	if m.Method == "item/reasoning/textDelta" || m.Method == "item/reasoning/summaryTextDelta" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j, e := s.Store.Job(jobID)
	if e != nil {
		s.unhealthy = e.Error()
		return
	}
	if j.RunID != runID {
		return
	}
	if m.Method == "item/agentMessage/delta" {
		var d struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Delta    string `json:"delta"`
		}
		if json.Unmarshal(m.Params, &d) != nil || d.ThreadID != j.ThreadID || (j.TurnID != "" && d.TurnID != j.TurnID) {
			return
		}
		if s.previews == nil {
			s.previews = map[string]preview{}
		}
		p := s.previews[jobID]
		if p.RunID != runID {
			p = preview{RunID: runID}
		}
		p.Text += d.Delta
		r := []rune(p.Text)
		if len(r) > 16000 {
			p.Text = string(r[len(r)-16000:])
		}
		s.previews[jobID] = p
		return
	}
	if m.Method == "item/commandExecution/outputDelta" || m.Method == "item/fileChange/outputDelta" {
		return
	}
	if m.Method == "turn/started" {
		delete(s.previews, jobID)
	}
	if m.Method == "item/completed" {
		var p struct {
			Item cr.Item `json:"item"`
		}
		if json.Unmarshal(m.Params, &p) == nil && p.Item.Type == "agentMessage" {
			delete(s.previews, jobID)
		}
	}
	ev := jobs.Event{Key: key, JobID: jobID, RunID: runID, Method: m.Method, Payload: m.Params, At: time.Now()}
	if len(ev.Payload) == 0 {
		ev.Payload = json.RawMessage(`{}`)
	}
	next, as := policy.Reduce(j, ev, m)
	if e = s.Store.Commit(next, &ev, as); e != nil {
		s.unhealthy = e.Error()
	} else if m.Method == "account/rateLimits/updated" {
		s.recordQuotaUpdate(j.Account, m.Params)
	}
}
func Start(ctx context.Context, c config.Config, self string) error {
	if Ping(ctx, c) == nil {
		return nil
	}
	m := service.Manager{Config: c, Self: self}
	if e := m.Ensure(ctx); e != nil {
		return e
	}
	return service.WaitReady(ctx, func(ctx context.Context) error { return Ping(ctx, c) })
}

// StartLegacy exists only for recovery of an earlier installation.
func StartLegacy(ctx context.Context, c config.Config, self string) error {
	if Ping(ctx, c) == nil {
		return nil
	}
	if e := os.MkdirAll(c.StateDir(), 0700); e != nil {
		return e
	}
	lock, e := os.OpenFile(filepath.Join(c.StateDir(), "starter.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	for {
		e = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if e == nil {
			break
		}
		if !errors.Is(e, unix.EWOULDBLOCK) {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	if Ping(ctx, c) == nil {
		return nil
	}
	if cr.HasSession(ctx, c.Supervisor.TmuxSession) {
		return errors.New("supervisor tmux session exists but is not responding; inspect its pane before restarting")
	}
	if e := os.MkdirAll(c.StateDir(), 0700); e != nil {
		return e
	}
	if e := c.Save(); e != nil {
		return e
	}
	cmd := exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", c.Supervisor.TmuxSession, cr.ShellQuote(self)+" --config "+cr.ShellQuote(c.Path)+" __supervisor")
	if b, e := cmd.CombinedOutput(); e != nil {
		return fmt.Errorf("start supervisor: %s: %w", cr.SafeText(string(b)), e)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if Ping(ctx, c) == nil {
			return nil
		}
		if !cr.HasSession(ctx, c.Supervisor.TmuxSession) {
			return errors.New("supervisor exited; check configuration and runtime")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return errors.New("supervisor is still starting; inspect the same tmux session")
}
