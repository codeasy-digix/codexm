package nativecli

import (
	"bufio"
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

// Real official exec -> confirmed foreground takeover -> same-ID interactive
// CLI under another synthetic login. Model traffic stays on this local fixture.
func TestOfficialExternalExecForegroundTakeover(t *testing.T) {
	product, native := os.Getenv("CODEXM_HANDOFF_TEST_BINARY"), os.Getenv("CODEXM_NATIVE_RUNTIME")
	if product == "" || native == "" || os.Getenv("CODEXM_NATIVE_TEST") != "1" {
		t.Skip("opt-in isolated official exec takeover")
	}
	isolatedViewerWorkspace(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	c := config.Defaults()
	c.Path = filepath.Join(home, "config.toml")
	c.Runtime.SharedCodexHome = filepath.Join(home, "shared")
	c.Runtime.AccountsDir = filepath.Join(home, "accounts")
	c.Storage.StateDir = filepath.Join(home, "state")
	c.Runtime.CodexBinary = native
	var mu sync.Mutex
	var requests []string
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
		if n == 1 {
			w.(http.Flusher).Flush()
			active <- struct{}{}
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"fixture-message\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"fixture complete\"}]}}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"fixture-response\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
	defer server.Close()
	requestCount := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(requests)
	}
	os.MkdirAll(c.Runtime.SharedCodexHome, 0700)
	settings := fmt.Sprintf("model='fixture'\nmodel_provider='fixture'\ncli_auth_credentials_store='file'\ncheck_for_update_on_startup=false\napproval_policy='never'\n[projects.%q]\ntrust_level='trusted'\n[model_providers.fixture]\nname='fixture'\nbase_url=%q\nwire_api='responses'\nrequires_openai_auth=false\n", home, server.URL)
	os.WriteFile(filepath.Join(c.Runtime.SharedCodexHome, "config.toml"), []byte(settings), 0600)
	for name, dir := range map[string]string{"first-account": c.Runtime.SharedCodexHome, "second-account": filepath.Join(c.Runtime.AccountsDir, "beta")} {
		os.MkdirAll(dir, 0700)
		claims, _ := json.Marshal(map[string]any{"sub": name, "email": "fixture@example.invalid", "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": name, "chatgpt_plan_type": "plus"}})
		token := "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
		b, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "last_refresh": time.Now().UTC().Format(time.RFC3339), "tokens": map[string]string{"id_token": token, "access_token": "fixture-access", "refresh_token": "fixture-refresh", "account_id": name}})
		os.WriteFile(filepath.Join(dir, "auth.json"), b, 0600)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	source := exec.CommandContext(ctx, native, "--no-daemon", "exec", "--json", "--skip-git-repo-check", "ORIGINAL_EXTERNAL_EXEC_REQUEST")
	source.Env, source.Dir = environment(c, "default"), home
	stdout, err := source.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	sourceLog, err := os.Create(filepath.Join(home, "exec-stderr.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer sourceLog.Close()
	source.Stderr = sourceLog
	if err = source.Start(); err != nil {
		t.Fatal(err)
	}
	ids := make(chan string, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		scan.Buffer(make([]byte, 4096), 4<<20)
		for scan.Scan() {
			var event struct {
				Type   string `json:"type"`
				Thread string `json:"thread_id"`
			}
			if json.Unmarshal(scan.Bytes(), &event) == nil && event.Type == "thread.started" {
				select {
				case ids <- event.Thread:
				default:
				}
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- source.Wait(); close(done) }()
	defer func() {
		_ = source.Process.Kill() // Owned synthetic account/provider only.
		<-done
		server.CloseClientConnections()
	}()
	var id string
	select {
	case id = <-ids:
	case <-ctx.Done():
		t.Fatal("official exec did not create a fixture thread")
	}
	select {
	case <-active:
	case <-ctx.Done():
		t.Fatal("official exec did not reach the local model fixture")
	}
	l, err := Plan(ctx, c, id, "beta")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := findExecOwner(ctx, c, l)
	if err != nil || owner.Process.PID != source.Process.Pid {
		t.Fatalf("real exec owner not verified: %v", err)
	}
	background := exec.CommandContext(ctx, product, "--config", c.Path, "resume", id, "--account", "beta")
	background.Env = append(os.Environ(), "HOME="+home)
	background.Stdin = strings.NewReader("y\n")
	if output, e := background.CombinedOutput(); e == nil || !strings.Contains(string(output), "exec takeover was not confirmed") {
		t.Fatalf("piped approval was accepted: %v %s", e, output)
	}
	if current, e := findExecOwner(ctx, c, l); e != nil || current != owner || requestCount() != 1 {
		t.Fatal("noninteractive invocation changed the original exec", e)
	}
	neighbor := viewerJob(t, "unrelated-fixture")
	neighborPID := viewerTmux(t, "display-message", "-p", "-t", neighbor.Pane, "#{pane_pid}")
	entry := viewerTmux(t, "new-window", "-d", "-P", "-F", "#{pane_id}", "-t", WorkspaceSession+":", "-n", "exec-takeover-confirmation", "-e", "HOME="+home, "-e", "PATH="+os.Getenv("PATH"), "/bin/sleep", "75")
	viewerTmux(t, "set-option", "-w", "-t", entry, "remain-on-exit", "on")
	entryWindow := viewerTmux(t, "display-message", "-p", "-t", entry, "#{window_id}")
	client := startViewer(t, Launch{TmuxServer: "default", TmuxSession: WorkspaceSession, Window: entryWindow, Pane: entry})
	waitText := func(text string) {
		t.Helper()
		for until := time.Now().Add(12 * time.Second); time.Now().Before(until); {
			out, _ := tmux(ctx, "default", "capture-pane", "-p", "-J", "-t", entry, "-S", "-50")
			if strings.Contains(out, text) {
				return
			}
			time.Sleep(60 * time.Millisecond)
		}
		t.Fatalf("missing %q: %s", text, viewerTmux(t, "capture-pane", "-p", "-J", "-t", entry, "-S", "-50"))
	}
	launchArgs := []string{"respawn-pane", "-k", "-t", entry, "-e", "HOME=" + home, "-e", "PATH=" + os.Getenv("PATH"), product, "--config", c.Path, "resume", id, "--account", "beta"}
	viewerTmux(t, launchArgs...)
	waitText("[y/N]")
	viewerTmux(t, "send-keys", "-t", entry, "Enter")
	waitText("exec takeover was not confirmed")
	if current, e := findExecOwner(ctx, c, l); e != nil || current != owner || requestCount() != 1 {
		t.Fatal("default No stopped or replayed the original exec", e)
	}
	viewerTmux(t, launchArgs...)
	waitText("[y/N]")
	viewerTmux(t, "send-keys", "-t", entry, "y", "Enter")
	var next Launch
	for until := time.Now().Add(20 * time.Second); time.Now().Before(until); {
		next, err = readLast(c, id)
		if err == nil && next.Account == "beta" && verifyOwnedCLI(ctx, c, next) == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil || next.Account != "beta" || verifyOwnedCLI(ctx, c, next) != nil {
		t.Fatalf("confirmed exec was not resumed under beta: %v; %s", err, viewerTmux(t, "capture-pane", "-p", "-J", "-t", entry, "-S", "-50"))
	}
	if key, e := cr.NativeProcessIdentity(owner.Process.PID); e == nil && key == owner.Key {
		t.Fatal("original exec still runs alongside the replacement")
	}
	if !next.RequireFreeWriter || next.Thread != id || next.Continue || requestCount() != 1 {
		t.Fatal("takeover forked/replayed work or lost the writer race guard")
	}
	awaitViewer(t, func() bool {
		clients := viewerTmux(t, "list-clients", "-F", "#{session_name}|#{pane_id}")
		return strings.Contains(clients, "|"+next.Pane) && !strings.Contains(clients, client.name+"|")
	})
	if viewerTmux(t, "display-message", "-p", "-t", neighbor.Pane, "#{pane_pid}") != neighborPID {
		t.Fatal("takeover restarted an unrelated job")
	}
	if err = Input(ctx, c, next.Session, "NEXT_AFTER_TAKEOVER", ""); err != nil {
		t.Fatal(err)
	}
	for until := time.Now().Add(10 * time.Second); requestCount() < 2 && time.Now().Before(until); {
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 || !strings.Contains(requests[1], "ORIGINAL_EXTERNAL_EXEC_REQUEST") || !strings.Contains(requests[1], "NEXT_AFTER_TAKEOVER") {
		t.Fatal("same-chat history or literal new input was not preserved")
	}
}
