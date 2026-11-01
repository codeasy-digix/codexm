package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

type fakeLoginTransport struct {
	mu          sync.Mutex
	events      chan cr.Message
	done        chan struct{}
	closeOnce   sync.Once
	home        string
	methods     []string
	mode        string
	serverID    string
	accountID   string
	readFailure bool
	readBlock   <-chan struct{}
	readStarted chan struct{}
}

func newFakeLoginTransport(home string) *fakeLoginTransport {
	return &fakeLoginTransport{events: make(chan cr.Message, 8), done: make(chan struct{}), home: home, serverID: "official-login-id", accountID: "fixture-account", readStarted: make(chan struct{}, 1)}
}

func (f *fakeLoginTransport) Call(ctx context.Context, method string, args, result any) error {
	f.mu.Lock()
	f.methods = append(f.methods, method)
	readFailure, readBlock := f.readFailure, f.readBlock
	f.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var value any
	switch method {
	case "account/login/start":
		f.mode = args.(map[string]any)["type"].(string)
		value = map[string]any{"type": f.mode, "loginId": f.serverID}
		if f.mode == "chatgpt" {
			value.(map[string]any)["authUrl"] = "https://auth.openai.com/oauth/authorize?state=PRIVATE-UI-CANARY"
		} else {
			value.(map[string]any)["verificationUrl"] = "https://auth.openai.com/codex/device"
			value.(map[string]any)["userCode"] = "PRIVATE-DEVICE-CODE"
		}
	case "account/read":
		if args.(map[string]bool)["refreshToken"] {
			return errors.New("verification must not refresh tokens")
		}
		select {
		case f.readStarted <- struct{}{}:
		default:
		}
		if readBlock != nil {
			select {
			case <-readBlock:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if readFailure {
			return errors.New("PRIVATE-TOKEN-AND-URL-MUST-NOT-BE-RETURNED")
		}
		value = map[string]any{"account": map[string]any{"type": "chatgpt"}, "workspaceRouting": map[string]any{"chatgptAccountId": f.accountID}}
	case "account/login/cancel":
		if args.(map[string]string)["loginId"] != f.serverID {
			return errors.New("another server login was canceled")
		}
		value = map[string]any{"status": "canceled"}
	default:
		return errors.New("login attempted an unrelated runtime method")
	}
	if result == nil {
		return nil
	}
	data, _ := json.Marshal(value)
	return json.Unmarshal(data, result)
}

func (f *fakeLoginTransport) Events() <-chan cr.Message { return f.events }
func (f *fakeLoginTransport) Done() <-chan struct{}     { return f.done }
func (f *fakeLoginTransport) Close() error {
	f.closeOnce.Do(func() { close(f.done) })
	return nil
}
func (f *fakeLoginTransport) complete(success bool, id string) {
	data, _ := json.Marshal(map[string]any{"loginId": id, "success": success, "error": "PRIVATE-COMPLETION-ERROR"})
	f.events <- cr.Message{Method: "account/login/completed", Params: data}
}

func loginFixture(t *testing.T) (config.Config, *LoginBroker, *[]*fakeLoginTransport) {
	t.Helper()
	root := t.TempDir()
	c := config.Defaults()
	c.Path = filepath.Join(root, "config.toml")
	c.Runtime.AccountsDir = filepath.Join(root, "accounts")
	c.Runtime.SharedCodexHome = filepath.Join(root, "shared")
	c.Runtime.EnginesDir = filepath.Join(root, "engines")
	c.Storage.StateDir = filepath.Join(root, "state")
	if e := c.Save(); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(c.Runtime.SharedCodexHome, 0700); e != nil {
		t.Fatal(e)
	}
	if e := config.AtomicWrite(filepath.Join(c.Runtime.SharedCodexHome, "auth.json"), []byte("native-login-canary"), 0600); e != nil {
		t.Fatal(e)
	}
	b := NewLoginBroker(context.Background(), c)
	transports := []*fakeLoginTransport{}
	b.start = func(ctx context.Context, got config.Config, home string) (loginTransport, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if got.Manager.Account != "" || home == c.Runtime.SharedCodexHome || home == filepath.Join(c.Runtime.AccountsDir, "account-secondary") {
			t.Error("sign-in required a manager or used existing credentials")
		}
		info, e := os.Stat(home)
		if e != nil || info.Mode().Perm() != 0700 {
			t.Error("temporary auth home was not private")
		}
		for _, name := range []string{"auth.json", "config.toml", "sessions", "thread-writer-locks"} {
			if _, e := os.Lstat(filepath.Join(home, name)); !os.IsNotExist(e) {
				t.Error("existing auth/config/history was copied into login staging")
			}
		}
		f := newFakeLoginTransport(home)
		transports = append(transports, f)
		return f, nil
	}
	t.Cleanup(b.Close)
	return c, b, &transports
}

func fixtureAuth(id, marker string) []byte {
	data, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "OPENAI_API_KEY": nil, "tokens": map[string]any{"account_id": id, "access_token": "private-access-" + marker, "refresh_token": "private-refresh-" + marker}})
	return data
}

func waitLogin(t *testing.T, b *LoginBroker, id, wanted string) LoginStatus {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status, e := b.Status(id)
		if e != nil {
			t.Fatal(e)
		}
		if status.Status == wanted {
			return status
		}
		time.Sleep(time.Millisecond)
	}
	status, _ := b.Status(id)
	t.Fatalf("sign-in status: wanted %s, got %s", wanted, status.Status)
	return LoginStatus{}
}

func waitLoginCleanup(t *testing.T, b *LoginBroker, id string) {
	t.Helper()
	s, e := b.session(id)
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-s.cleaned:
	case <-time.After(3 * time.Second):
		t.Fatal("owned sign-in cleanup did not complete")
	}
}

func assertSavedBytes(t *testing.T, filename string, expected []byte) {
	t.Helper()
	got, e := os.ReadFile(filename)
	if e != nil || string(got) != string(expected) {
		t.Fatal("saved credentials changed unexpectedly")
	}
}

func TestLoginBrokerRegistersFirstAccountOnlyAfterVerifiedCompletion(t *testing.T) {
	c, b, transports := loginFixture(t)
	before, _ := os.ReadFile(c.Path)
	login, e := b.Start(context.Background(), "account-secondary", "browser")
	if e != nil || login.Status != "pending" || login.AuthURL == "" || login.LoginID == "official-login-id" {
		t.Fatal("private sign-in did not start", e)
	}
	target := filepath.Join(c.Runtime.AccountsDir, "account-secondary", "auth.json")
	if _, e := os.Stat(target); !os.IsNotExist(e) {
		t.Fatal("pending registration created named credentials")
	}
	if names, e := List(c); e != nil || len(names) != 0 {
		t.Fatal("pending registration appeared in account list")
	}
	f := (*transports)[0]
	credentials := fixtureAuth(f.accountID, "verified")
	if e := config.AtomicWrite(filepath.Join(f.home, "auth.json"), credentials, 0600); e != nil {
		t.Fatal(e)
	}
	f.complete(true, "unrelated-login-id")
	if got, _ := b.Status(login.LoginID); got.Status != "pending" {
		t.Fatal("another login's completion was accepted")
	}
	f.complete(true, f.serverID)
	got := waitLogin(t, b, login.LoginID, "succeeded")
	waitLoginCleanup(t, b, login.LoginID)
	if got.AuthURL != "" || got.UserCode != "" || got.VerificationURL != "" {
		t.Fatal("completed sign-in retained private authorization data")
	}
	assertSavedBytes(t, target, credentials)
	assertSavedBytes(t, c.Path, before)
	assertSavedBytes(t, filepath.Join(c.Runtime.SharedCodexHome, "auth.json"), []byte("native-login-canary"))
	if info, e := os.Stat(target); e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("published credentials were not private")
	}
	if names, e := List(c); e != nil || !slices.Equal(names, []string{"account-secondary"}) {
		t.Fatal("verified new account was not discoverable")
	}
	if _, e := os.Stat(f.home); !os.IsNotExist(e) {
		t.Fatal("completed sign-in staging home leaked")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Equal(f.methods, []string{"account/login/start", "account/read"}) {
		t.Fatal("registration started a model, quota probe or unrelated runtime operation")
	}
}

func TestLoginBrokerCancelFailureAndExpirationPreserveOldCredentials(t *testing.T) {
	for _, outcome := range []string{"cancel", "failure", "expiry", "disconnect", "close"} {
		t.Run(outcome, func(t *testing.T) {
			c, b, transports := loginFixture(t)
			target := filepath.Join(c.Runtime.AccountsDir, "account-secondary", "auth.json")
			old := fixtureAuth("fixture-account", "original")
			if e := config.AtomicWrite(target, old, 0600); e != nil {
				t.Fatal(e)
			}
			if outcome == "expiry" {
				// Leave time for private file setup under race/build load before
				// testing expiry of an already pending login.
				b.timeout = time.Second
			}
			login, e := b.Start(context.Background(), "account-secondary", "device")
			if e != nil || login.UserCode == "" || login.AuthURL != "" {
				t.Fatal("device sign-in did not start", e)
			}
			f := (*transports)[0]
			wanted := "failed"
			switch outcome {
			case "cancel":
				wanted = "canceled"
				if _, e := b.Cancel(context.Background(), login.LoginID); e != nil {
					t.Fatal(e)
				}
			case "failure":
				f.complete(false, f.serverID)
			case "expiry":
				wanted = "expired"
			case "disconnect":
				_ = f.Close()
			case "close":
				wanted = "canceled"
				b.Close()
			}
			got := waitLogin(t, b, login.LoginID, wanted)
			waitLoginCleanup(t, b, login.LoginID)
			if got.AuthURL != "" || got.VerificationURL != "" || got.UserCode != "" || strings.Contains(got.Error, "PRIVATE") {
				t.Fatal("terminal status leaked a provider URL, code or raw error")
			}
			assertSavedBytes(t, target, old)
			assertSavedBytes(t, filepath.Join(c.Runtime.SharedCodexHome, "auth.json"), []byte("native-login-canary"))
			if _, e := os.Stat(f.home); !os.IsNotExist(e) {
				t.Fatal("pending sign-in home was not removed")
			}
			lock, e := cr.AccountLoginLock(c, "account-secondary")
			if e != nil {
				t.Fatal("terminal sign-in retained the CLI account lock", e)
			}
			lock.Close()
		})
	}
}

func TestCanceledNewLoginDoesNotCreateGhostAccount(t *testing.T) {
	c, b, _ := loginFixture(t)
	login, e := b.Start(context.Background(), "new-name", "browser")
	if e != nil {
		t.Fatal(e)
	}
	if _, e := b.Cancel(context.Background(), login.LoginID); e != nil {
		t.Fatal(e)
	}
	if names, e := List(c); e != nil || len(names) != 0 {
		t.Fatal("canceled registration left a discoverable account")
	}
}

func TestLoginBrokerRejectsWrongIdentityChangedCredentialAndUnsafeStaging(t *testing.T) {
	for _, problem := range []string{"different-identity", "changed-credentials", "staging-symlink", "missing-staging", "verification-failure", "account-home-symlink"} {
		t.Run(problem, func(t *testing.T) {
			c, b, transports := loginFixture(t)
			target := filepath.Join(c.Runtime.AccountsDir, "account-secondary", "auth.json")
			old := fixtureAuth("fixture-account", "original")
			if e := config.AtomicWrite(target, old, 0600); e != nil {
				t.Fatal(e)
			}
			login, e := b.Start(context.Background(), "account-secondary", "browser")
			if e != nil {
				t.Fatal(e)
			}
			f := (*transports)[0]
			candidate := filepath.Join(f.home, "auth.json")
			if problem == "different-identity" {
				f.accountID = "wrong-signed-in-account"
			}
			if problem != "missing-staging" {
				if e := config.AtomicWrite(candidate, fixtureAuth(f.accountID, "candidate"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			savedHome := ""
			switch problem {
			case "changed-credentials":
				old = fixtureAuth("fixture-account", "another-writer-refreshed")
				if e := config.AtomicWrite(target, old, 0600); e != nil {
					t.Fatal(e)
				}
			case "staging-symlink":
				os.Remove(candidate)
				if e := os.Symlink(target, candidate); e != nil {
					t.Fatal(e)
				}
			case "verification-failure":
				f.mu.Lock()
				f.readFailure = true
				f.mu.Unlock()
			case "account-home-symlink":
				savedHome = filepath.Join(c.Runtime.AccountsDir, "saved-original")
				if e := os.Rename(filepath.Dir(target), savedHome); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(c.Runtime.SharedCodexHome, filepath.Dir(target)); e != nil {
					t.Fatal(e)
				}
			}
			f.complete(true, f.serverID)
			got := waitLogin(t, b, login.LoginID, "failed")
			waitLoginCleanup(t, b, login.LoginID)
			if strings.Contains(got.Error, "PRIVATE") || got.AuthURL != "" {
				t.Fatal("verification returned private provider diagnostics")
			}
			if savedHome != "" {
				assertSavedBytes(t, filepath.Join(savedHome, "auth.json"), old)
			} else {
				assertSavedBytes(t, target, old)
			}
			assertSavedBytes(t, filepath.Join(c.Runtime.SharedCodexHome, "auth.json"), []byte("native-login-canary"))
		})
	}
}

func TestLoginBrokerSharesCLILockAndScopesSessionsToConnection(t *testing.T) {
	c, b, _ := loginFixture(t)
	lock, e := cr.AccountLoginLock(c, "account-secondary")
	if e != nil {
		t.Fatal(e)
	}
	if login, e := b.Start(context.Background(), "account-secondary", "browser"); e == nil {
		t.Fatal("GUI raced an existing CLI sign-in", login.Status)
	} else {
		waitLoginCleanup(t, b, login.LoginID)
	}
	lock.Close()
	login, e := b.Start(context.Background(), "account-secondary", "browser")
	if e != nil {
		t.Fatal(e)
	}
	if _, e := b.Start(context.Background(), "account-work", "device"); e == nil {
		t.Fatal("overlapping sign-in was accepted")
	}
	other := NewLoginBroker(context.Background(), c)
	defer other.Close()
	if _, e := other.Status(login.LoginID); e == nil {
		t.Fatal("another MCP connection inspected a private authorization URL")
	}
	if _, e := other.Cancel(context.Background(), login.LoginID); e == nil {
		t.Fatal("another MCP connection canceled this connection's sign-in")
	}
	if duplicate, e := other.Start(context.Background(), "account-secondary", "browser"); e == nil {
		t.Fatal("different broker raced the same saved account")
	} else {
		waitLoginCleanup(t, other, duplicate.LoginID)
	}
	if _, e := b.Cancel(context.Background(), login.LoginID); e != nil {
		t.Fatal(e)
	}
}

func TestLoginBrokerCancelDuringVerificationPreventsPublication(t *testing.T) {
	c, b, transports := loginFixture(t)
	login, e := b.Start(context.Background(), "account-secondary", "browser")
	if e != nil {
		t.Fatal(e)
	}
	f := (*transports)[0]
	blocked := make(chan struct{})
	f.mu.Lock()
	f.readBlock = blocked
	f.mu.Unlock()
	if e := config.AtomicWrite(filepath.Join(f.home, "auth.json"), fixtureAuth(f.accountID, "candidate"), 0600); e != nil {
		t.Fatal(e)
	}
	f.complete(true, f.serverID)
	select {
	case <-f.readStarted:
	case <-time.After(time.Second):
		t.Fatal("verification did not begin")
	}
	if _, e := b.Cancel(context.Background(), login.LoginID); e != nil {
		t.Fatal(e)
	}
	close(blocked)
	waitLogin(t, b, login.LoginID, "canceled")
	if _, e := os.Stat(filepath.Join(c.Runtime.AccountsDir, "account-secondary", "auth.json")); !os.IsNotExist(e) {
		t.Fatal("canceled verification published credentials")
	}
}

func TestLoginBrokerCloseDuringStartupDoesNotLeakLateOwnedResources(t *testing.T) {
	c, b, _ := loginFixture(t)
	started := make(chan string, 1)
	b.start = func(ctx context.Context, _ config.Config, home string) (loginTransport, error) {
		started <- home
		<-ctx.Done()
		// An engine initialized just as the connection closed is still owned.
		return newFakeLoginTransport(home), nil
	}
	result := make(chan error, 1)
	go func() { _, e := b.Start(context.Background(), "account-secondary", "browser"); result <- e }()
	home := <-started
	b.Close()
	if e := <-result; e == nil {
		t.Fatal("closed connection accepted a late startup")
	}
	if _, e := os.Stat(home); !os.IsNotExist(e) {
		t.Fatal("late auth startup leaked its staging home")
	}
	if _, e := b.Start(context.Background(), "account-work", "browser"); e == nil {
		t.Fatal("closed broker allowed a new sign-in")
	}
	if names, e := List(c); e != nil || len(names) != 0 {
		t.Fatal("closed startup created a saved account")
	}
}

func TestLoginCommandSeparatesAllAuthAndHistoryAndForcesFileStore(t *testing.T) {
	c := config.Defaults()
	t.Setenv("CODEX_HOME", "/parent-native-home")
	t.Setenv("CODEX_SQLITE_HOME", "/parent-history")
	t.Setenv("CODEX_ACCOUNT", "parent-login")
	t.Setenv("OPENAI_API_KEY", "PRIVATE-PARENT-API-KEY")
	t.Setenv("CODEX_ACCESS_TOKEN", "PRIVATE-PARENT-ACCESS-TOKEN")
	home := filepath.Join(t.TempDir(), "auth-only")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := loginCommand(ctx, cr.Resolver{Config: c, Binary: "/official-fixture"}, home)
	args := strings.Join(cmd.Args, " ")
	for _, want := range []string{"--no-daemon", `cli_auth_credentials_store="file"`, `forced_login_method="chatgpt"`, "app-server --listen stdio://"} {
		if !strings.Contains(args, want) {
			t.Fatal("official login command lacks an isolation option")
		}
	}
	for _, item := range cmd.Env {
		key, value, _ := strings.Cut(item, "=")
		if key == "CODEX_ACCOUNT" || key == "OPENAI_API_KEY" || key == "CODEX_ACCESS_TOKEN" {
			t.Fatal("a parent auth override leaked into login")
		}
		if key == "CODEX_HOME" || key == "CODEX_SQLITE_HOME" || key == "CODEX_SHARED_HOME" {
			if value != home {
				t.Fatal("login helper touched the existing auth/history home")
			}
		}
	}
	if cmd.Dir != home || cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("login helper did not use its private owned process context")
	}
	if os.Getenv("CODEX_HOME") != "/parent-native-home" {
		t.Fatal("parent shell account changed")
	}
}

func TestLoginTransportInitializationFailureReapsOwnedHelper(t *testing.T) {
	c, b, _ := loginFixture(t)
	b.Close()
	home := filepath.Join(t.TempDir(), "auth-only")
	if e := os.MkdirAll(home, 0700); e != nil {
		t.Fatal(e)
	}
	binary := filepath.Join(t.TempDir(), "login-fixture")
	body := []byte("#!/bin/sh\nprintf '%s\\n' \"$$\" > \"$CODEX_HOME/owned-pid\"\nsleep 30\n")
	if e := os.WriteFile(binary, body, 0700); e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256(body)
	engine := cr.Engine{Binary: binary, ProtocolChecked: true, Files: map[string]string{filepath.Base(binary): hex.EncodeToString(hash[:])}}
	data, _ := json.Marshal(engine)
	if e := config.AtomicWrite(filepath.Join(c.StateDir(), "engine.json"), data, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := cr.ReadEngine(c); e != nil {
		t.Fatal("isolated helper receipt is invalid", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		transport, e := startLoginTransport(ctx, c, home)
		if transport != nil {
			transport.Close()
		}
		finished <- e
	}()
	var pidBytes []byte
	startupDeadline := time.Now().Add(3 * time.Second)
	for len(pidBytes) == 0 && time.Now().Before(startupDeadline) {
		pidBytes, _ = os.ReadFile(filepath.Join(home, "owned-pid"))
		if len(pidBytes) > 0 {
			break
		}
		select {
		case e := <-finished:
			t.Fatal("isolated fixture exited before its readiness marker", e)
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if len(pidBytes) == 0 {
		t.Fatal("isolated fixture did not start")
	}
	cancel()
	select {
	case e := <-finished:
		if e == nil {
			t.Fatal("a nonresponsive login helper initialized")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled initialization did not stop its owned helper promptly")
	}
	var pid int
	if json.Unmarshal([]byte(strings.TrimSpace(string(pidBytes))), &pid) != nil || pid <= 0 {
		t.Fatal("invalid isolated fixture pid")
	}
	if e := syscall.Kill(pid, 0); !errors.Is(e, syscall.ESRCH) {
		t.Fatal("initialization failure returned before its owned process was reaped")
	}
}

func TestOfficialLoginBrokerPreviewAndCancelUseOnlyEmptyIsolatedAuth(t *testing.T) {
	if os.Getenv("CODEXM_LOGIN_NATIVE_TEST") != "1" {
		t.Skip("opt-in isolated official auth preview; does not complete sign-in")
	}
	c, fixture, _ := loginFixture(t)
	fixture.Close()
	c.Runtime.CodexBinary = os.Getenv("CODEXM_TEST_ENGINE")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b := NewNativeLoginBroker(ctx, c)
	defer b.Close()
	var observed *observedLoginTransport
	start := b.start
	b.start = func(ctx context.Context, c config.Config, home string) (loginTransport, error) {
		transport, e := start(ctx, c, home)
		if e != nil {
			return nil, e
		}
		observed = &observedLoginTransport{loginTransport: transport}
		return observed, nil
	}
	login, e := b.Start(ctx, "preview-only", "browser")
	if e != nil || login.Status != "pending" || !officialAuthURL(login.AuthURL) {
		t.Fatal("isolated official auth preview failed", e)
	}
	if _, e := b.Cancel(ctx, login.LoginID); e != nil {
		t.Fatal(e)
	}
	observed.mu.Lock()
	methods, cancelFailed := slices.Clone(observed.methods), observed.cancelFailed
	observed.mu.Unlock()
	if !slices.Equal(methods, []string{"account/login/start", "account/login/cancel"}) || cancelFailed {
		t.Fatal("official cancel was not acknowledged before owned helper cleanup")
	}
	owned, ok := observed.loginTransport.(*stdioLoginTransport)
	if !ok || owned.client.ProcessDone == nil {
		t.Fatal("official preview did not use an owned stdio process")
	}
	select {
	case <-owned.client.ProcessDone:
	default:
		t.Fatal("official cancellation returned before its helper process exited")
	}
	lock, e := cr.AccountLoginLock(c, "preview-only")
	if e != nil {
		t.Fatal("official cancellation retained the account login lock")
	}
	lock.Close()
	if names, e := List(c); e != nil || len(names) != 0 {
		t.Fatal("preview/cancel created a saved login")
	}
	assertSavedBytes(t, filepath.Join(c.Runtime.SharedCodexHome, "auth.json"), []byte("native-login-canary"))
	if _, e := os.Stat(filepath.Join(c.Runtime.SharedCodexHome, "state_5.sqlite")); !os.IsNotExist(e) {
		t.Fatal("auth preview opened the shared conversation database")
	}
	entries, e := os.ReadDir(c.Runtime.AccountsDir)
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".codexm-login-") {
			t.Fatal("official auth preview leaked a scratch auth home")
		}
	}
}

type observedLoginTransport struct {
	loginTransport
	mu           sync.Mutex
	methods      []string
	cancelFailed bool
}

func (t *observedLoginTransport) Call(ctx context.Context, method string, args, result any) error {
	e := t.loginTransport.Call(ctx, method, args, result)
	t.mu.Lock()
	t.methods = append(t.methods, method)
	if method == "account/login/cancel" && e != nil {
		t.cancelFailed = true
	}
	t.mu.Unlock()
	return e
}
