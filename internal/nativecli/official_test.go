package nativecli

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"fixture-response\"}}\n\n")
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
	defer client.Close()
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
	if e != nil {
		t.Log("Live owner rejected the official archive cycle; the launcher must report it without altering locks")
	} else {
		t.Log("Official cycle accepted by this version with a connected owner")
	}
	client.Close()
	select {
	case <-client.ProcessDone:
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		<-client.ProcessDone
	}
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
	if len(list.Sessions) != 1 || list.Sessions[0].ID != l.Thread || list.Sessions[0].Archived {
		t.Fatal("native identity or archive state was not preserved")
	}
}
