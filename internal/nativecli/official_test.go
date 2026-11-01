package nativecli

import (
	"context"
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

// Uses the installed official binary with an isolated HOME and local mock model.
// No real account, user conversation, external model call or GUI is involved.
func TestOfficialUnarchiveAndResumePreparation(t *testing.T) {
	if os.Getenv("CODEXM_NATIVE_TEST") != "1" {
		t.Skip("isolated official CLI integration")
	}
	binary := os.Getenv("CODEXM_NATIVE_RUNTIME")
	if binary == "" {
		t.Fatal("missing official binary")
	}
	h := t.TempDir()
	t.Setenv("HOME", h)
	var requests []string
	var requestMu sync.Mutex
	active := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestMu.Lock()
		requests = append(requests, string(body))
		requestMu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"fixture-response\"}}\n\n")
		if strings.Contains(string(body), "hold active fixture") && !strings.Contains(string(body), "follow-up fixture instruction") {
			w.(http.Flusher).Flush()
			select {
			case active <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"fixture-message\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"fixture complete\"}]}}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"fixture-response\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
	defer server.Close()
	c := config.Defaults()
	c.Path = filepath.Join(h, "codexm.toml")
	c.Runtime.SharedCodexHome = filepath.Join(h, "shared")
	c.Runtime.AccountsDir = filepath.Join(h, "accounts")
	c.Storage.StateDir = filepath.Join(h, "state")
	c.Runtime.CodexBinary = binary
	if e := os.MkdirAll(c.Runtime.SharedCodexHome, 0700); e != nil {
		t.Fatal(e)
	}
	settings := fmt.Sprintf("model='fixture'\nmodel_provider='fixture'\ncli_auth_credentials_store='file'\n[model_providers.fixture]\nname='fixture'\nbase_url=%q\nwire_api='responses'\nrequires_openai_auth=false\n", server.URL)
	if e := os.WriteFile(filepath.Join(c.Runtime.SharedCodexHome, "config.toml"), []byte(settings), 0600); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--no-daemon", "app-server", "--listen", "stdio://")
	cmd.Env = environment(c, "default")
	cmd.Dir = h
	client, e := cr.StartStdio(ctx, cmd)
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		client.Close()
		select {
		case <-client.ProcessDone:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-client.ProcessDone
		}
	}()
	var started struct {
		Thread cr.Thread `json:"thread"`
	}
	if e = client.Call(ctx, "thread/start", map[string]any{"cwd": h, "sandbox": "read-only", "approvalPolicy": "never"}, &started); e != nil {
		t.Fatal(e)
	}
	if e = client.Call(ctx, "turn/start", map[string]any{"threadId": started.Thread.ID, "input": []map[string]string{{"type": "text", "text": "fixture input"}}}, nil); e != nil {
		t.Fatal(e)
	}
	completed := false
	for !completed {
		select {
		case event := <-client.Events:
			completed = event.Method == "turn/completed"
		case <-ctx.Done():
			t.Fatal("mock native turn did not complete")
		}
	}
	l, e := Plan(ctx, c, started.Thread.ID, "default")
	if e != nil {
		t.Fatal(e)
	}
	liveCtx, liveCancel := context.WithTimeout(ctx, 8*time.Second)
	e = Prepare(liveCtx, c, l)
	liveCancel()
	if e == nil || !strings.Contains(e.Error(), "active writer") || !cr.WriterBusy(c.Runtime.SharedCodexHome, l.Thread) {
		t.Fatal("unreachable owner must remain intact and cannot be claimed as handed off", e)
	}
	var control struct {
		Thread cr.Thread `json:"thread"`
	}
	if e = client.Call(ctx, "thread/start", map[string]any{"cwd": h, "sandbox": "read-only", "approvalPolicy": "never"}, &control); e != nil {
		t.Fatal(e)
	}
	if e = client.Call(ctx, "thread/goal/set", map[string]any{"threadId": l.Thread, "objective": "preserve fixture goal", "status": "paused", "origin": "user", "tokenBudget": 100000}, nil); e != nil {
		t.Fatal(e)
	}
	goalBefore, e := client.Goal(ctx, l.Thread)
	if e != nil || goalBefore == nil {
		t.Fatal("fixture goal", e)
	}
	if e = client.Call(ctx, "turn/start", map[string]any{"threadId": l.Thread, "input": []map[string]string{{"type": "text", "text": "hold active fixture"}}}, nil); e != nil {
		t.Fatal(e)
	}
	select {
	case <-active:
	case <-ctx.Done():
		t.Fatal("fixture turn did not become active")
	}
	released, e := archiveLoadedThread(ctx, client, l.Thread)
	if e != nil || !released {
		t.Fatal("owner-side archive failed", e)
	}
	waitCtx, stopWait := context.WithTimeout(ctx, 3*time.Second)
	e = cr.WaitWriterRelease(waitCtx, c.Runtime.SharedCodexHome, l.Thread)
	stopWait()
	if e != nil {
		t.Fatal("owner did not release the selected active chat", e)
	}
	if !cr.WriterBusy(c.Runtime.SharedCodexHome, control.Thread.ID) {
		t.Fatal("handoff released a different chat")
	}
	if e = Prepare(ctx, c, l); e != nil {
		t.Fatal("unarchive after owner-side archive", e)
	}
	// The owner's other chat remains loaded while the target is resumed elsewhere.
	archive := exec.CommandContext(ctx, binary, "--no-daemon", "archive", l.Thread)
	archive.Env = environment(c, "default")
	archive.Dir = h
	if b, e := archive.CombinedOutput(); e != nil {
		t.Fatalf("official archive: %v %s", e, cr.SafeText(string(b)))
	}
	if e = Prepare(ctx, c, l); e != nil {
		t.Fatal(e)
	}
	if e = Prepare(ctx, c, l); e != nil {
		t.Fatal("already-unarchived thread cannot be reopened", e)
	}
	secondCmd := exec.CommandContext(ctx, binary, "--no-daemon", "app-server", "--listen", "stdio://")
	secondCmd.Env, secondCmd.Dir = environment(c, "default"), h
	second, e := cr.StartStdio(ctx, secondCmd)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = second.Resume(ctx, l.Thread); e != nil {
		second.Close()
		t.Fatal("immediate official resume after the cycle", e)
	}
	if !cr.WriterBusy(c.Runtime.SharedCodexHome, control.Thread.ID) {
		t.Fatal("other owner lost its writer during resume")
	}
	goalAfter, e := second.Goal(ctx, l.Thread)
	if e != nil || goalAfter == nil || goalAfter.Objective != goalBefore.Objective || goalAfter.Status != goalBefore.Status || goalAfter.TokenBudget == nil || goalBefore.TokenBudget == nil || *goalAfter.TokenBudget != *goalBefore.TokenBudget {
		t.Fatal("native goal or budget changed", e)
	}
	if e = second.Call(ctx, "turn/start", map[string]any{"threadId": l.Thread, "input": []map[string]string{{"type": "text", "text": "follow-up fixture instruction"}}}, nil); e != nil {
		t.Fatal(e)
	}
	completed = false
	for !completed {
		select {
		case event := <-second.Events:
			completed = event.Method == "turn/completed"
		case <-ctx.Done():
			t.Fatal("resumed chat did not complete its follow-up instruction")
		}
	}
	requestMu.Lock()
	lastRequest := requests[len(requests)-1]
	requestMu.Unlock()
	if !strings.Contains(lastRequest, "fixture input") || !strings.Contains(lastRequest, "follow-up fixture instruction") {
		t.Fatal("official resume did not retain history and deliver the follow-up instruction")
	}
	second.Close()
	select {
	case <-second.ProcessDone:
	case <-time.After(3 * time.Second):
		_ = secondCmd.Process.Kill()
		<-second.ProcessDone
	}
	list, e := List(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	targetFound := false
	for _, s := range list.Sessions {
		if s.ID == l.Thread && !s.Archived {
			targetFound = true
		}
	}
	if !targetFound {
		t.Fatal("native identity or archive state was not preserved")
	}
}
