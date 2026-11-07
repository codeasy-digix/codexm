//go:build gui_experiment

package nativecli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/gorilla/websocket"
)

type daemonFixture struct {
	*guiControlFixture
	servers map[string]*daemonConnection
}

// Real packaged native daemons, fake logins, local account/routing service
// and local SSE model. No real credentials, paid requests or GUI restarts.
func newDaemonFixture(t *testing.T, names ...string) *daemonFixture {
	f := newGUIControlFixture(t, "stdio")
	df := &daemonFixture{guiControlFixture: f, servers: map[string]*daemonConnection{}}
	tokens := map[string]string{}
	for _, name := range names {
		saveToolbarAuth(t, f.c, name, name, name)
		claims, _ := json.Marshal(map[string]any{"sub": name, "exp": time.Now().Add(time.Hour).Unix(), "email": name + "@example.invalid", "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": name, "chatgpt_user_id": name, "chatgpt_plan_type": "team"}})
		tokens[name] = "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(claims) + ".fixture" // gitleaks:allow -- unsigned synthetic JWT
		b, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "last_refresh": time.Now().UTC().Format(time.RFC3339), "tokens": map[string]string{"access_token": tokens[name], "id_token": tokens[name], "refresh_token": "fixture-refresh-" + name, "account_id": name}})
		if err := os.WriteFile(filepath.Join(f.c.Runtime.AccountsDir, name, "auth.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var auth *httptest.Server
	auth = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/accounts/check") {
			entries := []any{}
			for _, name := range names {
				entries = append(entries, map[string]any{"id": name, "name": "Fixture", "plan_type": "team", "workspace_backend_origin": "https://fixture.invalid", "account_routing_override": "NO_CONSTRAINT"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"accounts": entries})
			return
		}
		if r.URL.Path == "/refresh" {
			var body struct {
				Token string `json:"refresh_token"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			name := strings.TrimPrefix(body.Token, "fixture-refresh-")
			if tokens[name] == "" {
				http.Error(w, "unknown synthetic login", 400)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": tokens[name], "id_token": tokens[name], "refresh_token": body.Token, "token_type": "Bearer", "expires_in": 3600})
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(auth.Close)
	t.Setenv("CODEX_REFRESH_TOKEN_URL_OVERRIDE", auth.URL+"/refresh")
	p := filepath.Join(f.c.Runtime.SharedCodexHome, "config.toml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	settings := fmt.Sprintf("chatgpt_base_url=%q\nthread_unload_delay_secs=1\n", auth.URL) + strings.ReplaceAll(string(b), "requires_openai_auth=false", "requires_openai_auth=true") + "\n[features]\nplugins=false\napps=false\nhooks=false\nskip_host_skill_discovery=true\n"
	if err := os.WriteFile(p, []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		name := name
		t.Setenv("CODEXM_GUI_FIXTURE_KEY", "fixture-daemon-"+name)
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			b, err := daemonNative(ctx, f.c, name, "app-server", "daemon", "version")
			var v daemonVersion
			if err == nil && json.Unmarshal(b, &v) == nil && sameDaemonPath(v.Socket, filepath.Join(f.c.Runtime.AccountsDir, name, "app-server-control", "app-server-control.sock")) {
				executable, _ := filepath.EvalSymlinks(v.Binary)
				if _, err := daemonNative(ctx, f.c, name, "app-server", "daemon", "stop"); err != nil {
					t.Error("stop isolated daemon", err)
				}
				// The tested native release can leave its owned exec-server behind.
				// Reap only this fixture's exact immutable executable, never a user
				// installation or a daemon reached through a shared/default home.
				rows, _ := exec.Command("/bin/ps", "-axo", "pid=,comm=").Output()
				for _, row := range strings.Split(string(rows), "\n") {
					fields := strings.Fields(row)
					if len(fields) < 2 || strings.Join(fields[1:], " ") != executable {
						continue
					}
					pid, _ := strconv.Atoi(fields[0])
					p, e := inspectProcess(ctx, pid)
					if e == nil && p.Executable == executable && sameProcess(ctx, p) {
						_ = syscall.Kill(pid, syscall.SIGTERM)
					}
				}
			}
		})
		info, err := StartDaemonExperiment(f.ctx, f.c, name)
		if err != nil {
			t.Fatal("start isolated daemon", name, err)
		}
		d, err := openDaemon(f.ctx, f.c, name, true)
		if err != nil {
			t.Fatal(err)
		}
		if info.Process.PID != d.info.Process.PID {
			t.Fatal("registered daemon changed")
		}
		df.servers[name] = d
		t.Cleanup(func() { d.client.Close() })
	}
	t.Cleanup(func() { f.once.Do(func() { close(f.release) }) })
	return df
}

func TestDaemonExperimentManagedAccountsAndTransfer(t *testing.T) {
	f := newDaemonFixture(t, "alpha", "beta")
	a, b := f.servers["alpha"], f.servers["beta"]
	if a.info.Process.PID == b.info.Process.PID || a.info.Socket == b.info.Socket || a.info.Identity == b.info.Identity {
		t.Fatal("daemon accounts not isolated")
	}
	f.client = a.client
	registered, err := StartDaemonExperiment(f.ctx, f.c, "alpha")
	if err != nil || registered.Process.PID != a.info.Process.PID || registered.Process.Started != a.info.Process.Started {
		t.Fatal("registering running daemon restarted it", err)
	}
	id := f.thread()
	f.start(id, "FIRST_ACCOUNT_TURN")
	f.completed(id, 1)
	neighbor := f.thread()
	f.start(neighbor, "GUI_FIXTURE_ACTIVE_NEIGHBOR")
	select {
	case <-f.active:
	case <-time.After(10 * time.Second):
		t.Fatal("neighbor did not become active")
	}
	request := "00000000-0000-4000-8000-000000000021"
	r, err := TransferDaemonChat(f.ctx, f.c, request, id, "alpha", "beta", false)
	if err != nil || r.State != "completed" {
		t.Fatalf("same-ID transfer: %+v %v", r, err)
	}
	if err := a.verify(f.ctx, f.c, neighbor, true); err != nil {
		t.Fatal("neighbor writer changed", err)
	}
	thread, err := a.client.Read(f.ctx, neighbor, false)
	if err != nil || thread.Status.Type != "active" {
		t.Fatal("neighbor was interrupted", thread.Status, err)
	}
	f.client = b.client
	if _, err := b.client.Resume(f.ctx, id); err != nil {
		t.Fatal(err)
	}
	f.start(id, "SECOND_ACCOUNT_TURN")
	after := f.completed(id, 2)
	if after.ID != id {
		t.Fatal("thread was forked")
	}
	f.mu.Lock()
	first, last := f.keys[0], f.keys[len(f.keys)-1]
	f.mu.Unlock()
	if first == last {
		t.Fatal("model request did not use a different daemon credential")
	}
	again, err := TransferDaemonChat(f.ctx, f.c, request, id, "alpha", "beta", false)
	if err != nil || !again.Updated.Equal(r.Updated) {
		t.Fatal("receipt request replayed", again, err)
	}
	if _, err := TransferDaemonChat(f.ctx, f.c, request, id, "beta", "alpha", false); err == nil {
		t.Fatal("conflicting receipt accepted")
	}
	if err := b.verify(f.ctx, f.c, id, true); err != nil {
		t.Fatal("target writer missing", err)
	}
	if !sameProcess(f.ctx, a.info.Process) || !sameProcess(f.ctx, b.info.Process) {
		t.Fatal("daemon restarted")
	}
	f.start(id, "GUI_FIXTURE_ACTIVE_TRANSFER")
	activeRequest := "00000000-0000-4000-8000-000000000022"
	if _, err := TransferDaemonChat(f.ctx, f.c, activeRequest, id, "beta", "alpha", false); err == nil || !strings.Contains(err.Error(), "--interrupt") {
		t.Fatal("active turn was not protected", err)
	}
	r, err = TransferDaemonChat(f.ctx, f.c, activeRequest, id, "beta", "alpha", true)
	if err != nil || r.State != "completed" {
		t.Fatal("explicit selected-turn interrupt/transfer", r, err)
	}
	thread, err = a.client.Read(f.ctx, neighbor, false)
	if err != nil || thread.Status.Type != "active" {
		t.Fatal("selected-turn interrupt affected neighbor", err)
	}
	t.Log("same thread ID, different authenticated servers, two turns, active neighbor preserved, receipt replay suppressed")
}

func TestDaemonExperimentGUITransport(t *testing.T) {
	f := newDaemonFixture(t, "alpha")
	d := f.servers["alpha"]
	f.client = d.client
	id := f.thread()
	f.start(id, "GUI_TRANSPORT_FIXTURE")
	f.completed(id, 1)
	// The desktop's observed transport uses /rpc. Test the official protocol;
	// this is not an assertion that the real GUI was reconfigured or restarted.
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second, NetDialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", d.info.Socket)
	}}
	ws, resp, err := dialer.DialContext(f.ctx, "ws://localhost/rpc", nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatal("GUI Unix WebSocket path", err)
	}
	defer ws.Close()
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := ws.WriteJSON(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "codexm-fixture", "version": "0"}, "capabilities": map[string]bool{"experimentalApi": true}}}); err != nil {
		t.Fatal(err)
	}
	for {
		var m cr.Message
		if err := ws.ReadJSON(&m); err != nil {
			t.Fatal(err)
		}
		if string(m.ID) == "1" {
			if m.Error != nil {
				t.Fatal(m.Error)
			}
			break
		}
	}
	cmd := exec.CommandContext(f.ctx, f.binary, "app-server", "proxy", "--sock", d.info.Socket)
	cmd.Env, cmd.Dir = environment(f.c, "alpha"), f.cmd.Dir
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = in.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			_ = cmd.Process.Signal(syscall.SIGTERM)
			<-done
		}
	})
	// Official proxy forwards raw socket bytes, including the HTTP Upgrade and
	// WebSocket frames; it is not a JSON-lines app-server subprocess.
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	go func() { _, _ = io.Copy(in, left); _ = in.Close() }()
	go func() { _, _ = io.Copy(left, out); _ = left.Close() }()
	tunnel := websocket.Dialer{HandshakeTimeout: 5 * time.Second, NetDialContext: func(context.Context, string, string) (net.Conn, error) { return right, nil }}
	proxy, resp, err := tunnel.DialContext(f.ctx, "ws://localhost/rpc", nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		t.Fatal("official stdio WebSocket tunnel", err)
	}
	defer proxy.Close()
	_ = proxy.SetReadDeadline(time.Now().Add(5 * time.Second))
	_ = proxy.WriteJSON(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "codexm-proxy-fixture", "version": "0"}, "capabilities": map[string]bool{"experimentalApi": true}}})
	for {
		var m cr.Message
		if err := proxy.ReadJSON(&m); err != nil {
			t.Fatal(err)
		}
		if string(m.ID) == "1" {
			if m.Error != nil {
				t.Fatal(m.Error)
			}
			break
		}
	}
	_ = proxy.WriteJSON(map[string]any{"method": "initialized", "params": map[string]any{}})
	_ = proxy.WriteJSON(map[string]any{"id": 2, "method": "thread/read", "params": map[string]any{"threadId": id, "includeTurns": false}})
	for {
		var m cr.Message
		if err := proxy.ReadJSON(&m); err != nil {
			t.Fatal(err)
		}
		if string(m.ID) == "2" {
			var r struct{ Thread cr.Thread }
			if m.Error != nil || json.Unmarshal(m.Result, &r) != nil || r.Thread.ID != id {
				t.Fatal("proxy did not reach same chat", m.Error)
			}
			break
		}
	}
	proxy.Close()
	right.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("owned proxy did not exit on EOF")
	}
	if err := d.verify(f.ctx, f.c, id, true); err != nil {
		t.Fatal("proxy changed writer", err)
	}
	t.Log("desktop /rpc Unix handshake and official stdio proxy share existing daemon; GUI UI attachment not attempted")
}

func TestDaemonExperimentPTYLossKeepsTurn(t *testing.T) {
	verifyDaemonTerminalLoss(t, false)
}

func TestDaemonExperimentFrontendSIGTERMKeepsTurn(t *testing.T) {
	verifyDaemonTerminalLoss(t, true)
}

func verifyDaemonTerminalLoss(t *testing.T, terminate bool) {
	product := os.Getenv("CODEXM_GUI_EXPERIMENT_BINARY")
	if !filepath.IsAbs(product) {
		t.Skip("requires experimental product binary for real native PTY")
	}
	f := newDaemonFixture(t, "alpha")
	d := f.servers["alpha"]
	f.client = d.client
	id := f.thread()
	f.start(id, "TERMINAL_SEED")
	f.completed(id, 1)
	d.client.Close() // Remove the seed subscriber before opening the terminal.
	f.c.Path = filepath.Join(f.cmd.Dir, "codexm.toml")
	if err := f.c.Save(); err != nil {
		t.Fatal(err)
	}
	l, err := OpenDaemonTerminal(f.ctx, f.c, product, "alpha", id, "")
	if err != nil {
		t.Fatal("daemon frontend", err)
	}
	if _, err := tmux(f.ctx, CodexTmuxServer, "set-option", "-p", "-t", l.Pane, "remain-on-exit", "on"); err != nil {
		t.Fatal("retain fixture frontend diagnostics", err)
	}
	t.Cleanup(func() { _, _ = tmux(context.Background(), CodexTmuxServer, "kill-pane", "-t", l.Pane) })
	var frontend processIdentity
	for until := time.Now().Add(15 * time.Second); time.Now().Before(until); {
		pidText, err := tmux(f.ctx, CodexTmuxServer, "display-message", "-p", "-t", l.Pane, "#{pane_pid}")
		if err == nil {
			pid, _ := strconv.Atoi(strings.TrimSpace(pidText))
			p, e := inspectProcess(f.ctx, pid)
			if e == nil && sameDaemonPath(p.Executable, d.info.Process.Executable) {
				frontend = p
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if frontend.PID == 0 {
		screen, _ := tmux(f.ctx, CodexTmuxServer, "capture-pane", "-p", "-S", "-100", "-t", l.Pane)
		t.Fatal("official remote frontend not running", screen)
	}
	// Wait for the actual native composer, then send exactly one test input.
	for until := time.Now().Add(10 * time.Second); ; {
		screen, _ := tmux(f.ctx, CodexTmuxServer, "capture-pane", "-p", "-t", l.Pane)
		if strings.Contains(screen, "Ask Codex to do anything") || strings.Contains(screen, "context left") || strings.Contains(screen, "? for shortcuts") {
			break
		}
		if time.Now().After(until) {
			t.Fatal("native remote composer unavailable", screen)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, err := tmux(f.ctx, CodexTmuxServer, "send-keys", "-t", l.Pane, "-l", "GUI_FIXTURE_ACTIVE_TERMINAL"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // Native paste-burst detection must settle before Enter.
	if _, err := tmux(f.ctx, CodexTmuxServer, "send-keys", "-t", l.Pane, "Enter"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.active:
	case <-time.After(12 * time.Second):
		screen, _ := tmux(f.ctx, CodexTmuxServer, "capture-pane", "-p", "-t", l.Pane)
		t.Fatal("terminal did not start selected turn", screen)
	}
	observer, err := openDaemon(f.ctx, f.c, "alpha", true)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.client.Close()
	before, _, err := observer.client.Recent(f.ctx, id, 1, "")
	if err != nil || len(before.Turns) != 1 {
		t.Fatal("observe terminal turn", err)
	}
	turnID := before.Turns[0].ID
	// Destroy the owned frontend itself, not merely detach a tmux viewer.
	if terminate {
		if !sameProcess(f.ctx, frontend) {
			t.Fatal("owned frontend changed before signal")
		}
		if err := syscall.Kill(frontend.PID, syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := tmux(f.ctx, CodexTmuxServer, "kill-pane", "-t", l.Pane); err != nil {
			t.Fatal(err)
		}
	}
	for until := time.Now().Add(8 * time.Second); sameProcess(f.ctx, frontend); {
		if time.Now().After(until) {
			t.Fatal("frontend survived PTY loss")
		}
		time.Sleep(40 * time.Millisecond)
	}
	time.Sleep(1500 * time.Millisecond)
	after, _, err := observer.client.Recent(f.ctx, id, 1, "")
	if err != nil || after.Status.Type != "active" || len(after.Turns) != 1 || after.Turns[0].ID != turnID || !sameProcess(f.ctx, d.info.Process) {
		t.Fatal("PTY loss did not preserve daemon turn", after.Status, err)
	}
	observer.client.Close() // No controller connection during completion either.
	f.once.Do(func() { close(f.release) })
	time.Sleep(300 * time.Millisecond)
	observer, err = openDaemon(f.ctx, f.c, "alpha", true)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.client.Close()
	f.client = observer.client
	completed := f.completed(id, 2)
	if completed.Turns[1].ID != turnID {
		t.Fatal("terminal work was restarted instead of retained")
	}
	t.Log("native frontend PID exited after PTY loss; same daemon PID and turn ID completed without any subscribing controller")
}
