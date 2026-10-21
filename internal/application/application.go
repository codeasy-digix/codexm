package application

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/service"
	"github.com/codeasy-digix/codexm/internal/settings"
	"github.com/codeasy-digix/codexm/internal/store"
	"github.com/codeasy-digix/codexm/internal/supervisor"
	"github.com/codeasy-digix/codexm/internal/terminal"
)

const ProtocolVersion = 1

type Method struct {
	Name     string `json:"name"`
	Mutation bool   `json:"mutation"`
	Model    bool   `json:"model"`
}

var commands = map[string]string{
	"jobs.create": "new", "jobs.register": "register", "jobs.unregister": "unregister", "jobs.import": "resume", "jobs.manage": "manage", "jobs.send": "send", "jobs.pause": "pause", "jobs.stop": "stop", "jobs.resume": "resume-job", "jobs.complete": "complete", "jobs.reply": "reply", "jobs.account": "switch-account", "jobs.goal": "goal", "manager.invoke": "manager", "actions.resolve": "resolve-action", "settings.save": "settings-save",
	"conversation.read": "read", "jobs.details": "details", "logs.read": "logs", "usage.read": "usage", "accounts.refresh": "accounts", "models.list": "models", "operation.read": "operation", "changes.replay": "replay", "viewer.heartbeat": "client", "viewer.detach": "detach",
}

func Methods() []Method {
	out := []Method{}
	for name, cmd := range commands {
		out = append(out, Method{name, mutation(cmd), slices.Contains([]string{"new", "send", "manager", "resume-job", "goal"}, cmd)})
	}
	for _, name := range []string{"capabilities", "snapshot", "sessions.list", "settings.read", "login.status", "service.status"} {
		out = append(out, Method{name, false, false})
	}
	for _, name := range []string{"service.start", "login.start", "login.cancel", "accounts.default", "accounts.remove", "terminal.open"} {
		out = append(out, Method{name, true, false})
	}
	slices.SortFunc(out, func(a, b Method) int { return strings.Compare(a.Name, b.Name) })
	return out
}
func mutation(cmd string) bool {
	return slices.Contains([]string{"new", "register", "unregister", "resume", "manage", "send", "pause", "stop", "resume-job", "complete", "reply", "switch-account", "goal", "manager", "resolve-action", "settings-save"}, cmd)
}

type LoginBroker interface {
	Start(context.Context, string, string) (accounts.LoginStatus, error)
	Status(string) (accounts.LoginStatus, error)
	Cancel(context.Context, string) (accounts.LoginStatus, error)
	Close()
}
type loginOwner struct {
	mu      sync.Mutex
	broker  LoginBroker
	current string
	replies map[string]loginResult
	at      time.Time
}
type loginResult struct {
	hash   string
	result accounts.LoginStatus
	err    error
}
type App struct {
	Config     config.Config
	Self       string
	Generation string
	NewLogins  func(context.Context, config.Config) LoginBroker
	ctx        context.Context
	mu         sync.Mutex
	owners     map[string]*loginOwner
}

func New(ctx context.Context, c config.Config, self string) *App {
	return &App{Config: c, Self: self, Generation: jobs.ID(), ctx: ctx, owners: map[string]*loginOwner{}, NewLogins: func(ctx context.Context, c config.Config) LoginBroker { return accounts.NewLoginBroker(ctx, c) }}
}
func (a *App) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, o := range a.owners {
		o.broker.Close()
	}
	a.owners = map[string]*loginOwner{}
}
func decode(raw json.RawMessage, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("invalid_input: %w", err)
	}
	return nil
}

func (a *App) Call(ctx context.Context, owner, method, operation string, raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	c, err := config.Load(a.Config.Path)
	if err != nil {
		return nil, err
	}
	switch method {
	case "accounts.remove":
		var p accounts.RemoveArgs
		if err = decode(raw, &p); err != nil {
			return nil, err
		}
		return RemoveAccount(ctx, c, p, operation)
	case "capabilities":
		var ping map[string]any
		_ = supervisor.Request(ctx, c, "ping", nil, &ping)
		engine, _ := cr.ReadEngine(c)
		host, _ := os.Hostname()
		return map[string]any{"protocolVersion": ProtocolVersion, "version": config.Version, "generation": a.Generation, "supervisor": ping, "engine": engine.Version, "methods": Methods(), "host": host, "store": config.Expand(c.Runtime.SharedCodexHome), "features": map[string]bool{"journal": ping != nil && ping["web_api"] == float64(1), "goalFork": false, "attachments": false}}, nil
	case "snapshot":
		return a.snapshot(ctx, c)
	case "sessions.list":
		return a.sessions(ctx, c, raw)
	case "settings.read":
		doc, err := settings.Read(c)
		if os.IsNotExist(err) {
			doc.Accounts = []string{}
			err = nil
		}
		return doc, err
	case "service.status":
		return (service.Manager{Config: c, Self: a.Self}).Status(ctx), nil
	case "service.start":
		var p struct {
			Manager string `json:"manager_account"`
		}
		if err = decode(raw, &p); err != nil {
			return nil, err
		}
		if err = Start(ctx, c, a.Self, p.Manager); err != nil {
			return nil, err
		}
		return a.snapshot(ctx, c)
	case "login.start", "login.status", "login.cancel":
		return a.login(ctx, c, owner, method, operation, raw)
	case "accounts.default":
		var p struct {
			Account string `json:"account"`
		}
		if err = decode(raw, &p); err != nil {
			return nil, err
		}
		lock, err := cr.AccountLoginLock(c, p.Account)
		if err != nil {
			return nil, err
		}
		defer lock.Close()
		_, err = accounts.SetNativeDefault(c, p.Account)
		return map[string]any{"account": p.Account}, err
	case "terminal.open":
		var p struct {
			ID string `json:"id"`
		}
		if err = decode(raw, &p); err != nil {
			return nil, err
		}
		opened, err := terminal.Open(ctx, c, a.Self, p.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"message": opened.String()}, nil
	}
	cmd, ok := commands[method]
	if !ok {
		return nil, errors.New("unsupported: unknown method")
	}
	if cmd == "new" {
		var p supervisor.NewArgs
		if err = decode(raw, &p); err != nil {
			return nil, err
		}
	} else if cmd != "settings-save" && cmd != "replay" && cmd != "resolve-action" {
		var p supervisor.JobArgs
		if err = decode(raw, &p); err != nil {
			return nil, err
		}
	}
	if cmd == "settings-save" && supervisor.Ping(ctx, c) != nil {
		r, err := cr.Resolve(ctx, c)
		if err != nil {
			return nil, err
		}
		return settings.Save(ctx, c, accounts.New(r), raw)
	}
	if mutation(cmd) {
		var ping struct {
			Web int `json:"web_api"`
		}
		if err = supervisor.Request(ctx, c, "ping", nil, &ping); err != nil {
			return nil, errors.New("service_unavailable: start the management service")
		}
		if ping.Web != 1 {
			return nil, errors.New("version_mismatch: upgrade the management service before web changes")
		}
		var result json.RawMessage
		err = supervisor.Request(ctx, c, "web-operation", supervisor.WebOperation{ID: operation, Command: cmd, Args: raw}, &result)
		return result, err
	}
	var result json.RawMessage
	err = supervisor.Request(ctx, c, cmd, raw, &result)
	return result, err
}

func (a *App) snapshot(ctx context.Context, c config.Config) (any, error) {
	names, err := accounts.List(c)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if names == nil {
		names = []string{}
	}
	_, rev, err := config.Current(c.Path)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"connected": false, "setup_required": c.Manager.Account == "", "account_names": names, "manager_account": c.Manager.Account, "native_default_account": accounts.CurrentNickname(c, config.Expand(c.Runtime.SharedCodexHome), ""), "priority": c.WorkerAccounts.Priority, "fallback_account": c.WorkerAccounts.FallbackAccount, "preferred_recheck_seconds": c.WorkerAccounts.PreferredRecheckSeconds, "config_revision": rev, "observed_at": time.Now()}
	out["account_removal_available"] = true
	var snap store.Snapshot
	var ping map[string]any
	if supervisor.Request(ctx, c, "ping", nil, &ping) == nil {
		out["connected"] = true
		out["supervisor"] = ping
		out["account_removal_available"] = ping["accounts_api"] == float64(1)
		if ping["web_api"] == float64(1) {
			err = supervisor.Request(ctx, c, "snapshot", nil, &snap)
		} else {
			out["message"] = "관리 서비스 업그레이드가 필요합니다."
		}
	} else if st, e := store.ReadOnly(c.StateDir()); e == nil {
		snap, err = st.ConsistentSnapshot()
		st.Close()
		if err != nil {
			err = nil
		}
	}
	if err != nil {
		return nil, err
	}
	for i := range snap.Jobs {
		j := &snap.Jobs[i]
		j.LastInput = ""
		j.LastOutput = ""
		j.PendingRequests = nil
		if j.Goal != nil {
			j.Goal.Objective = ""
		}
	}
	for i := range snap.Actions {
		snap.Actions[i].Payload = nil
	}
	out["state"] = snap
	return out, nil
}
func (a *App) sessions(ctx context.Context, c config.Config, raw json.RawMessage) (any, error) {
	var p struct {
		Search  string `json:"search"`
		Cursor  string `json:"cursor"`
		Project string `json:"project"`
		Account string `json:"account"`
		State   string `json:"state"`
		Managed bool   `json:"managed"`
		Active  bool   `json:"active"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	offset := 0
	var err error
	if p.Cursor != "" {
		offset, err = strconv.Atoi(p.Cursor)
		if err != nil || offset < 0 {
			return nil, errors.New("invalid_input: invalid cursor")
		}
	}
	list, err := catalog.Read(ctx, config.Expand(c.Runtime.SharedCodexHome))
	if err != nil {
		list.Warning = err.Error()
	}
	if st, e := store.ReadOnly(c.StateDir()); e == nil {
		js, e := st.Jobs()
		st.Close()
		if e != nil {
			return nil, e
		}
		list = catalog.Merge(list, js)
	}
	encoded, _ := json.Marshal(list)
	sum := sha256.Sum256(encoded)
	filtered := []catalog.Entry{}
	for _, e := range list.Sessions {
		if p.Search != "" && !strings.Contains(strings.ToLower(e.Title+" "+e.ID+" "+e.Cwd), strings.ToLower(p.Search)) || p.Project != "" && e.Cwd != p.Project || p.Account != "" && e.Account != p.Account || p.State != "" && e.State != p.State || p.Managed && !e.Managed || p.Active && e.Archived {
			continue
		}
		filtered = append(filtered, e)
	}
	offset = min(offset, len(filtered))
	end := min(offset+50, len(filtered))
	cursor := ""
	if end < len(filtered) {
		cursor = strconv.Itoa(end)
	}
	return map[string]any{"sessions": filtered[offset:end], "cursor": cursor, "total": len(filtered), "revision": hex.EncodeToString(sum[:]), "observed_at": time.Now(), "warning": list.Warning}, nil
}
func (a *App) login(ctx context.Context, c config.Config, owner, method, operation string, raw json.RawMessage) (any, error) {
	if len(owner) < 16 || len(owner) > 128 {
		return nil, errors.New("authentication_required: browser session required")
	}
	var p struct {
		Account    string `json:"account"`
		Mode       string `json:"mode"`
		ID         string `json:"login_id"`
		Generation string `json:"generation"`
	}
	if err := decode(raw, &p); err != nil {
		return nil, err
	}
	a.mu.Lock()
	// Idle owners have no live login after the broker's 15 minute deadline.
	for key, o := range a.owners {
		if time.Since(o.at) > 24*time.Hour {
			o.broker.Close()
			delete(a.owners, key)
		}
	}
	o := a.owners[owner]
	if o == nil {
		if method != "login.start" {
			a.mu.Unlock()
			return nil, errors.New("login_expired: this browser has no active login")
		}
		if len(a.owners) >= 64 {
			a.mu.Unlock()
			return nil, errors.New("busy: too many browser sessions")
		}
		o = &loginOwner{broker: a.NewLogins(a.ctx, c), replies: map[string]loginResult{}}
		a.owners[owner] = o
	}
	o.at = time.Now()
	a.mu.Unlock()
	o.mu.Lock()
	defer o.mu.Unlock()
	if method == "login.status" {
		if p.ID == "" {
			p.ID = o.current
		}
		return o.broker.Status(p.ID)
	}
	if p.Generation != a.Generation {
		return nil, errors.New("login_expired: bridge restarted; check saved accounts before starting again")
	}
	if len(operation) < 16 || len(operation) > 128 {
		return nil, errors.New("invalid_input: operation ID required")
	}
	sum := sha256.Sum256(append([]byte(method), raw...))
	hash := hex.EncodeToString(sum[:])
	if prior, ok := o.replies[operation]; ok {
		if prior.hash != hash {
			return nil, errors.New("operation_conflict: different login request")
		}
		return prior.result, prior.err
	}
	if len(o.replies) >= 128 {
		return nil, errors.New("busy: reopen this browser session before more login attempts")
	}
	var result accounts.LoginStatus
	var err error
	if method == "login.start" {
		result, err = o.broker.Start(ctx, p.Account, p.Mode)
		if err == nil {
			o.current = result.LoginID
		}
	} else {
		result, err = o.broker.Cancel(ctx, p.ID)
	}
	o.replies[operation] = loginResult{hash, result, err}
	return result, err
}
