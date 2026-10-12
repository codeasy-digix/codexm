package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

// LoginStatus is private UI data. Authorization URLs and one-time codes must not
// be included in model results, logs, notifications, or persisted diagnostics.
// Deadline is this broker's cleanup deadline, not the provider's token expiry.
type LoginStatus struct {
	Account         string    `json:"account"`
	LoginID         string    `json:"login_id"`
	Mode            string    `json:"mode"`
	Status          string    `json:"status"`
	Deadline        time.Time `json:"deadline"`
	AuthURL         string    `json:"auth_url,omitempty"`
	VerificationURL string    `json:"verification_url,omitempty"`
	UserCode        string    `json:"user_code,omitempty"`
	Error           string    `json:"error,omitempty"`
}

type loginTransport interface {
	Call(context.Context, string, any, any) error
	Events() <-chan cr.Message
	Done() <-chan struct{}
	Close() error
}

type LoginBroker struct {
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	config    config.Config
	closed    bool
	active    string
	sessions  map[string]*loginSession
	order     []string
	closeOnce sync.Once
	timeout   time.Duration
	start     func(context.Context, config.Config, string) (loginTransport, error)
}

type loginSession struct {
	mu        sync.Mutex
	status    LoginStatus
	ctx       context.Context
	cancel    context.CancelFunc
	setupDone chan struct{}
	cleaned   chan struct{}
	cleanOnce sync.Once
	transport loginTransport
	serverID  string
	home      string
	target    string
	homeInfo  os.FileInfo
	lock      *os.File
	before    authFingerprint
}

type authFingerprint struct {
	exists    bool
	digest    [sha256.Size]byte
	accountID string
}

func NewLoginBroker(ctx context.Context, c config.Config) *LoginBroker {
	ctx, cancel := context.WithCancel(ctx)
	b := &LoginBroker{ctx: ctx, cancel: cancel, config: c, sessions: map[string]*loginSession{}, timeout: 15 * time.Minute, start: startLoginTransport}
	go func() { <-ctx.Done(); b.Close() }()
	return b
}

// Start owns only a temporary file-backed authentication server. It neither
// loads a conversation nor requires a selected manager account or supervisor.
func (b *LoginBroker) Start(ctx context.Context, account, mode string) (LoginStatus, error) {
	if !config.Nickname.MatchString(account) || account == "default" {
		return LoginStatus{}, errors.New("invalid account nickname")
	}
	if mode != "browser" && mode != "device" {
		return LoginStatus{}, errors.New("choose browser or device sign-in")
	}
	if ctx.Err() != nil {
		return LoginStatus{}, errors.New("sign-in request was canceled")
	}
	key := make([]byte, 16)
	if _, e := rand.Read(key); e != nil {
		return LoginStatus{}, errors.New("could not prepare a private sign-in session")
	}
	b.mu.Lock()
	if b.closed || b.ctx.Err() != nil {
		b.mu.Unlock()
		return LoginStatus{}, errors.New("sign-in connection is closed")
	}
	if b.active != "" {
		b.mu.Unlock()
		return LoginStatus{}, errors.New("finish or cancel the current sign-in first")
	}
	id := hex.EncodeToString(key)
	deadline := time.Now().UTC().Add(b.timeout)
	lifetime, cancel := context.WithDeadline(b.ctx, deadline)
	s := &loginSession{ctx: lifetime, cancel: cancel, setupDone: make(chan struct{}), cleaned: make(chan struct{}), status: LoginStatus{Account: account, LoginID: id, Mode: mode, Status: "pending", Deadline: deadline}}
	b.sessions[id], b.active = s, id
	b.order = append(b.order, id)
	// Terminal sessions contain no URLs/codes and are retained only for UI replies.
	for len(b.order) > 32 {
		delete(b.sessions, b.order[0])
		b.order = b.order[1:]
	}
	b.mu.Unlock()
	defer close(s.setupDone)
	setup, done := context.WithCancel(ctx)
	stop := context.AfterFunc(lifetime, done)
	defer func() { stop(); done() }()
	fail := func(message string) (LoginStatus, error) {
		status := "failed"
		if lifetime.Err() != nil {
			status = "canceled"
			if errors.Is(lifetime.Err(), context.DeadlineExceeded) {
				status = "expired"
			}
		}
		b.finish(s, status, message)
		return s.snapshot(), errors.New(message)
	}
	lock, e := cr.AccountLoginLock(b.config, account)
	if e != nil {
		return fail("account home is unavailable or another sign-in owns this account")
	}
	s.lock = lock // Cleanup waits for setupDone before reading these resources.
	root, e := filepath.EvalSymlinks(config.Expand(b.config.Runtime.AccountsDir))
	if e != nil {
		return fail("could not prepare the named account home")
	}
	s.target = filepath.Join(root, account, "auth.json")
	s.homeInfo, e = os.Lstat(filepath.Dir(s.target))
	if e != nil || !s.homeInfo.IsDir() || s.homeInfo.Mode()&os.ModeSymlink != 0 {
		return fail("named account home changed during sign-in startup")
	}
	s.before, e = fingerprintAuth(s.target)
	if e != nil {
		return fail("existing account credentials could not be inspected safely")
	}
	s.home, e = os.MkdirTemp(root, ".codexm-login-")
	if e != nil {
		return fail("could not prepare a private sign-in home")
	}
	if e = os.Chmod(s.home, 0700); e != nil {
		return fail("could not protect the private sign-in home")
	}
	if setup.Err() != nil {
		return fail("sign-in request was canceled")
	}
	s.transport, e = b.start(lifetime, b.config, s.home)
	if e != nil {
		return fail("official Codex sign-in server could not be started")
	}
	params := map[string]any{"type": "chatgpt"}
	if mode == "device" {
		params["type"] = "chatgptDeviceCode"
	}
	var reply struct {
		Type            string `json:"type"`
		LoginID         string `json:"loginId"`
		AuthURL         string `json:"authUrl"`
		VerificationURL string `json:"verificationUrl"`
		UserCode        string `json:"userCode"`
	}
	call, cancelCall := context.WithTimeout(setup, 40*time.Second)
	e = s.transport.Call(call, "account/login/start", params, &reply)
	cancelCall()
	// The engine owns this id; the opaque public id never permits another MCP
	// connection to inspect or cancel a server's unrelated sign-in session.
	s.serverID = reply.LoginID
	if setup.Err() != nil {
		return fail("sign-in request was canceled")
	}
	if e != nil || reply.LoginID == "" || len(reply.LoginID) > 256 {
		return fail("official Codex could not start sign-in; existing credentials were preserved")
	}
	valid := mode == "browser" && reply.Type == "chatgpt" && officialAuthURL(reply.AuthURL) || mode == "device" && reply.Type == "chatgptDeviceCode" && officialAuthURL(reply.VerificationURL) && len(reply.UserCode) > 0 && len(reply.UserCode) <= 128
	if !valid {
		return fail("official Codex returned an unsupported sign-in response")
	}
	s.mu.Lock()
	if s.status.Status == "pending" && lifetime.Err() == nil {
		if mode == "browser" {
			s.status.AuthURL = reply.AuthURL
		} else {
			s.status.VerificationURL, s.status.UserCode = reply.VerificationURL, reply.UserCode
		}
	}
	s.mu.Unlock()
	go b.watch(s)
	return s.snapshot(), nil
}

func (b *LoginBroker) Status(loginID string) (LoginStatus, error) {
	s, e := b.session(loginID)
	if e != nil {
		return LoginStatus{}, e
	}
	return s.snapshot(), nil
}

func (b *LoginBroker) Cancel(ctx context.Context, loginID string) (LoginStatus, error) {
	s, e := b.session(loginID)
	if e != nil {
		return LoginStatus{}, e
	}
	b.finish(s, "canceled", "")
	select {
	case <-s.cleaned:
		return s.snapshot(), nil
	case <-ctx.Done():
		return s.snapshot(), errors.New("sign-in cleanup is still finishing")
	}
}

// Close cancels and cleans up only this connection's pending auth servers.
func (b *LoginBroker) Close() {
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.closed = true
		b.cancel()
		sessions := make([]*loginSession, 0, len(b.sessions))
		for _, s := range b.sessions {
			sessions = append(sessions, s)
		}
		b.mu.Unlock()
		for _, s := range sessions {
			b.finish(s, "canceled", "")
		}
		for _, s := range sessions {
			<-s.cleaned
		}
	})
}

func (b *LoginBroker) session(id string) (*loginSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s := b.sessions[id]; s != nil {
		return s, nil
	}
	return nil, errors.New("sign-in session does not belong to this connection")
}

func (s *loginSession) snapshot() LoginStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (b *LoginBroker) finish(s *loginSession, status, message string) {
	s.mu.Lock()
	if s.status.Status == "pending" || s.status.Status == "verifying" {
		s.status.Status, s.status.Error = status, message
		s.status.AuthURL, s.status.VerificationURL, s.status.UserCode = "", "", ""
	}
	s.mu.Unlock()
	s.cancel()
	s.cleanOnce.Do(func() {
		go func() {
			// A close during startup must not remove a home the owned helper is
			// still opening, or leave resources acquired after cleanup behind.
			<-s.setupDone
			if s.transport != nil {
				if s.serverID != "" && s.snapshot().Status != "succeeded" {
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					_ = s.transport.Call(ctx, "account/login/cancel", map[string]string{"loginId": s.serverID}, nil)
					cancel()
				}
				_ = s.transport.Close()
			}
			if s.home != "" {
				_ = os.RemoveAll(s.home)
			}
			if s.lock != nil {
				_ = s.lock.Close()
			}
			b.mu.Lock()
			if b.active == s.status.LoginID {
				b.active = ""
			}
			b.mu.Unlock()
			close(s.cleaned)
		}()
	})
}

func (b *LoginBroker) watch(s *loginSession) {
	for {
		select {
		case <-s.ctx.Done():
			status := "canceled"
			if errors.Is(s.ctx.Err(), context.DeadlineExceeded) {
				status = "expired"
			}
			b.finish(s, status, "")
			return
		case <-s.transport.Done():
			b.finish(s, "failed", "sign-in server disconnected; existing credentials were preserved")
			return
		case event := <-s.transport.Events():
			if event.Method != "account/login/completed" {
				continue
			}
			var completed struct {
				LoginID string `json:"loginId"`
				Success bool   `json:"success"`
			}
			if json.Unmarshal(event.Params, &completed) != nil || completed.LoginID != s.serverID {
				continue
			}
			if !completed.Success {
				b.finish(s, "failed", "sign-in did not complete; existing credentials were preserved")
				return
			}
			b.verify(s)
			return
		}
	}
}

func (b *LoginBroker) verify(s *loginSession) {
	s.mu.Lock()
	if s.status.Status != "pending" || s.ctx.Err() != nil {
		s.mu.Unlock()
		status := "canceled"
		if errors.Is(s.ctx.Err(), context.DeadlineExceeded) {
			status = "expired"
		}
		b.finish(s, status, "")
		return
	}
	s.status.Status = "verifying"
	s.status.AuthURL, s.status.VerificationURL, s.status.UserCode = "", "", ""
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	var account struct {
		Account *struct {
			Type string `json:"type"`
		} `json:"account"`
		WorkspaceRouting *struct {
			AccountID string `json:"chatgptAccountId"`
		} `json:"workspaceRouting"`
	}
	if e := s.transport.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &account); e != nil || account.Account == nil || account.Account.Type != "chatgpt" {
		b.finish(s, "failed", "completed sign-in could not be verified; existing credentials were preserved")
		return
	}
	data, exists, e := readAuth(filepath.Join(s.home, "auth.json"))
	if e != nil || !exists {
		b.finish(s, "failed", "completed sign-in did not save private credentials")
		return
	}
	id, e := validChatGPTAuth(data)
	if e != nil || account.WorkspaceRouting != nil && account.WorkspaceRouting.AccountID != "" && id != account.WorkspaceRouting.AccountID {
		b.finish(s, "failed", "completed sign-in saved an unexpected account identity")
		return
	}
	if s.before.accountID != "" && s.before.accountID != id {
		b.finish(s, "failed", "sign in to the same account saved under this nickname; existing credentials were preserved")
		return
	}
	// Serialize the last cancel/connection-close check with publication. The
	// shared CLI account lock and fingerprint check protect the named alias.
	b.mu.Lock()
	s.mu.Lock()
	if b.closed || s.ctx.Err() != nil || s.status.Status != "verifying" {
		s.mu.Unlock()
		b.mu.Unlock()
		b.finish(s, "canceled", "")
		return
	}
	now, e := fingerprintAuth(s.target)
	homeInfo, homeErr := os.Lstat(filepath.Dir(s.target))
	message := ""
	if homeErr != nil || !homeInfo.IsDir() || homeInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(homeInfo, s.homeInfo) {
		message = "named account home changed during sign-in; existing credentials were preserved"
	} else if e != nil || now != s.before {
		message = "saved credentials changed during sign-in; refresh and try again"
	} else if e = config.AtomicWrite(s.target, data, 0600); e != nil {
		message = "new credentials could not be saved; existing credentials were preserved"
	}
	if message == "" {
		s.status.Status = "succeeded"
	}
	s.mu.Unlock()
	b.mu.Unlock()
	if message != "" {
		b.finish(s, "failed", message)
	} else {
		b.finish(s, "succeeded", "")
	}
}

func officialAuthURL(value string) bool {
	u, e := url.Parse(value)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" && u.Port() != "443" || len(value) > 16*1024 {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "auth.openai.com", "auth0.openai.com", "chatgpt.com":
		return true
	}
	return false
}

func readAuth(filename string) ([]byte, bool, error) {
	fd, e := syscall.Open(filename, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if errors.Is(e, syscall.ENOENT) {
		return nil, false, nil
	}
	if e != nil {
		return nil, false, e
	}
	f := os.NewFile(uintptr(fd), "private account credentials")
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > 256*1024 {
		return nil, false, errors.New("credentials require a bounded regular file")
	}
	data, e := io.ReadAll(io.LimitReader(f, 256*1024+1))
	if e != nil || len(data) > 256*1024 {
		return nil, false, errors.New("credentials could not be read safely")
	}
	return data, true, nil
}

func fingerprintAuth(filename string) (authFingerprint, error) {
	data, exists, e := readAuth(filename)
	if e != nil {
		return authFingerprint{}, e
	}
	fingerprint := authFingerprint{exists: exists}
	if exists {
		fingerprint.digest = sha256.Sum256(data)
		var auth struct {
			Tokens struct {
				AccountID string `json:"account_id"`
			} `json:"tokens"`
		}
		if json.Unmarshal(data, &auth) == nil {
			fingerprint.accountID = auth.Tokens.AccountID
		}
	}
	return fingerprint, nil
}

func validChatGPTAuth(data []byte) (string, error) {
	var auth struct {
		Mode   string  `json:"auth_mode"`
		APIKey *string `json:"OPENAI_API_KEY"`
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal(data, &auth) != nil || auth.Mode != "" && auth.Mode != "chatgpt" || auth.APIKey != nil && *auth.APIKey != "" || auth.Tokens.AccessToken == "" || auth.Tokens.RefreshToken == "" || auth.Tokens.AccountID == "" {
		return "", errors.New("private managed ChatGPT credentials required")
	}
	return auth.Tokens.AccountID, nil
}

type stdioLoginTransport struct {
	client *cr.Client
	cancel context.CancelFunc
}

func (t *stdioLoginTransport) Call(ctx context.Context, method string, args, result any) error {
	return t.client.Call(ctx, method, args, result)
}
func (t *stdioLoginTransport) Events() <-chan cr.Message { return t.client.Events }
func (t *stdioLoginTransport) Done() <-chan struct{}     { return t.client.Done }
func (t *stdioLoginTransport) Close() error {
	_ = t.client.Close()
	t.cancel()
	// The child is context-owned, its whole private group is killed, and pipe
	// cleanup has a WaitDelay. Do not release the alias lock while it can still
	// own the browser callback port or write inside the staging home.
	if t.client.ProcessDone != nil {
		<-t.client.ProcessDone
	}
	return nil
}

func loginCommand(ctx context.Context, r cr.Resolver, home string) *exec.Cmd {
	// A login probe must never inherit the default login, keychain store,
	// manager's credentials, shared history, or the account's previous tokens.
	r.Config.Runtime.SharedCodexHome = home
	r.Config.Runtime.AccountsDir = filepath.Join(home, ".accounts")
	cmd := r.Command(ctx, "", "app-server", "--listen", "stdio://", "-c", `model_provider="openai"`)
	cmd.Dir = home
	cmd.WaitDelay = time.Second
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		// This newly created process group contains only the owned login helper
		// and its code-mode child. No shared Codex process is signaled.
		if e := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); errors.Is(e, syscall.ESRCH) {
			return os.ErrProcessDone
		} else {
			return e
		}
	}
	return cmd
}

func startLoginTransport(ctx context.Context, c config.Config, home string) (loginTransport, error) {
	r := cr.Resolver{Config: c}
	if pinned, e := cr.ReadEngine(c); e == nil {
		r.Binary, r.Version = pinned.Binary, pinned.Version
	} else if os.IsNotExist(e) {
		// The first sign-in can use the installed official engine without
		// bootstrapping supervision or writing a persistent engine receipt.
		var locateErr error
		r, locateErr = cr.Locate(c)
		if locateErr != nil {
			return nil, locateErr
		}
	} else {
		return nil, e
	}
	// Keep the process alive long enough to acknowledge account/login/cancel
	// after the broker's session context is canceled. Initialization still uses
	// the session context; Close is the sole owner of this detached child.
	owned, cancel := context.WithCancel(context.WithoutCancel(ctx))
	cmd := loginCommand(owned, r, home)
	init, done := context.WithTimeout(ctx, 8*time.Second)
	client, e := cr.StartStdio(init, cmd)
	done()
	if e != nil {
		cancel()
		return nil, errors.New("official sign-in server initialization failed")
	}
	return &stdioLoginTransport{client: client, cancel: cancel}, nil
}
