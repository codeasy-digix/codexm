package nativecli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

// Real official CLI, real active request, same-ID replacement. Quota preflight
// is covered by the separate full request test; here a prepared request avoids
// real OpenAI login/quota calls. Model traffic goes only to the local fixture.
func TestOfficialActiveCLIAccountReplacement(t *testing.T) {
	product, native := os.Getenv("CODEXM_HANDOFF_TEST_BINARY"), os.Getenv("CODEXM_NATIVE_RUNTIME")
	if product == "" || native == "" || os.Getenv("CODEXM_NATIVE_TEST") != "1" {
		t.Skip("isolated official handoff integration")
	}
	h := t.TempDir()
	t.Setenv("HOME", h)
	c := config.Defaults()
	c.Path = filepath.Join(h, "codexm.toml")
	c.Runtime.SharedCodexHome = filepath.Join(h, "shared")
	c.Runtime.AccountsDir = filepath.Join(h, "accounts")
	c.Storage.StateDir = filepath.Join(h, "state")
	c.Runtime.CodexBinary = native
	var mu sync.Mutex
	requests := []string{}
	active := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/responses") {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, string(body))
		n := len(requests)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"fixture-response\"}}\n\n")
		if n == 2 {
			w.(http.Flusher).Flush()
			active <- struct{}{}
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"fixture-message\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"fixture complete\"}]}}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"fixture-response\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
	defer server.Close()
	os.MkdirAll(c.Runtime.SharedCodexHome, 0700)
	settings := fmt.Sprintf("model='fixture'\nmodel_provider='fixture'\ncli_auth_credentials_store='file'\ncheck_for_update_on_startup=false\napproval_policy='never'\n[projects.%q]\ntrust_level='trusted'\n[model_providers.fixture]\nname='fixture'\nbase_url=%q\nwire_api='responses'\nrequires_openai_auth=false\n", h, server.URL)
	os.WriteFile(filepath.Join(c.Runtime.SharedCodexHome, "config.toml"), []byte(settings), 0600)
	if e := c.Save(); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	seedCmd := exec.CommandContext(ctx, native, "--no-daemon", "app-server", "--listen", "stdio://")
	seedCmd.Env = environment(c, "default")
	seedCmd.Dir = h
	seed, e := cr.StartStdio(ctx, seedCmd)
	if e != nil {
		t.Fatal(e)
	}
	defer seed.Close()
	var started struct {
		Thread cr.Thread `json:"thread"`
	}
	if e = seed.Call(ctx, "thread/start", map[string]any{"cwd": h, "sandbox": "read-only", "approvalPolicy": "never"}, &started); e != nil {
		t.Fatal(e)
	}
	if e = seed.Call(ctx, "turn/start", map[string]any{"threadId": started.Thread.ID, "input": []map[string]string{{"type": "text", "text": "ORIGINAL_REQUEST_PRESERVED"}}}, nil); e != nil {
		t.Fatal(e)
	}
	for {
		select {
		case event := <-seed.Events:
			if event.Method == "turn/completed" {
				goto seeded
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
seeded:
	seed.Close()
	select {
	case <-seed.ProcessDone:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Syntactically valid, entirely fake logins; the custom fixture provider does
	// not authenticate with OpenAI. These hashes prove account-home replacement.
	for name, dir := range map[string]string{"first-account": c.Runtime.SharedCodexHome, "second-account": filepath.Join(c.Runtime.AccountsDir, "beta")} {
		os.MkdirAll(dir, 0700)
		claims, _ := json.Marshal(map[string]any{"sub": name, "email": "fixture@example.invalid", "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": name, "chatgpt_plan_type": "plus"}})
		token := "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
		b, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "last_refresh": time.Now().UTC().Format(time.RFC3339), "tokens": map[string]string{"id_token": token, "access_token": "fixture-access", "refresh_token": "fixture-refresh", "account_id": name}})
		os.WriteFile(filepath.Join(dir, "auth.json"), b, 0600)
	}
	l, e := OpenContinuation(ctx, c, product, started.Thread.ID, "default", true, "")
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		for _, s := range Sessions(c) {
			_ = removeTerminal(context.Background(), s)
		}
		server.CloseClientConnections()
	}()
	select {
	case <-active:
	case <-time.After(25 * time.Second):
		screen, _ := Screen(ctx, c, l.Session)
		t.Fatalf("native CLI did not start local fixture request: %s", screen.Text)
	}
	source, e := readLast(c, l.Thread)
	if e != nil {
		t.Fatal(e)
	}
	writerDeadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(writerDeadline) {
		if e = verifyOwnedCLI(ctx, c, source); e == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if e != nil {
		screen, _ := Screen(ctx, c, l.Session)
		ls, _ := exec.Command("lsof", "-nP", "-a", "-p", fmt.Sprint(source.Owner.PID), "-F", "pn").Output()
		t.Fatalf("%v; %s; writer files: %s", e, screen.Text, ls)
	}
	viewer := startViewer(t, source)
	target, e := Plan(ctx, c, l.Thread, "beta")
	if e != nil {
		t.Fatal(e)
	}
	key, e := authKey(c, "beta")
	if e != nil {
		t.Fatal(e)
	}
	s := SwitchRequest{ID: "0123456789abcdef0123456789abcdef", Session: switchSession(c, source.Thread), Source: source, Target: target, TargetAuth: key, Caller: os.Getpid(), CallerKey: "completed-request", Created: time.Now().Unix()}
	dir := switchDir(c, s.ID)
	os.MkdirAll(dir, 0700)
	file := filepath.Join(dir, "request.json")
	if e = writeJSON(file, s); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(dir, "armed"), []byte("fixture"), 0600)
	helperPane, e := tmux(ctx, CodexTmuxServer, "new-window", "-d", "-P", "-F", "#{pane_id}", "-t", WorkspaceSession+":", "-n", s.Session, "-e", "PATH="+os.Getenv("PATH"), "-e", "HOME="+h, product, "--config", c.Path, "__handoff", file)
	if e != nil {
		t.Fatal(e)
	}
	defer tmux(context.Background(), CodexTmuxServer, "kill-pane", "-t", strings.TrimSpace(helperPane))
	until := time.Now().Add(50 * time.Second)
	for time.Now().Before(until) {
		status, e := HandoffStatus(c, l.Thread)
		if e == nil && status.Phase == "failed" {
			screen, _ := Screen(ctx, c, terminalName(c, l.Thread, "beta"))
			t.Fatalf("native handoff failed: %s; %s", status.Error, screen.Text)
		}
		mu.Lock()
		n := len(requests)
		mu.Unlock()
		if e == nil && status.Phase == "started" && n >= 3 {
			goto replaced
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("replacement did not acquire writer and submit continuation")
replaced:
	last, e := readLast(c, l.Thread)
	if e != nil || last.Account != "beta" || last.AuthKey == source.AuthKey {
		t.Fatal("login identity did not change", e)
	}
	if verifyOwnedCLI(ctx, c, source) == nil {
		t.Fatal("old native CLI still owns chat")
	}
	if e = verifyOwnedCLI(ctx, c, last); e != nil {
		t.Fatal(e)
	}
	clients := viewerTmux(t, "list-clients", "-F", "#{session_name}|#{pane_id}")
	if !strings.Contains(clients, "|"+last.Pane) || strings.Contains(clients, viewer.name+"|") {
		t.Fatal("native handoff did not follow the independent viewer")
	}
	mu.Lock()
	final := requests[len(requests)-1]
	count := len(requests)
	mu.Unlock()
	if count != 3 || !strings.Contains(final, "ORIGINAL_REQUEST_PRESERVED") || !strings.Contains(final, ContinuePrompt) {
		t.Fatal("history or one-time continuation not preserved", count)
	}
}
