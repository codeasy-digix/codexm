package nativecli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func webTestCall(t *testing.T, w *WebChat, ctx context.Context) func(string, any) (map[string]any, error) {
	t.Helper()
	n := 0
	return func(method string, params any) (map[string]any, error) {
		n++
		b, _ := json.Marshal(params)
		value, err := w.Dispatch(ctx, Request{Version: 1, Owner: "isolated-web-test", Operation: fmt.Sprintf("10000000-0000-4000-8000-%012d", n), Method: method, Params: b})
		if err != nil {
			return nil, err
		}
		b, _ = json.Marshal(value)
		var r map[string]any
		err = json.Unmarshal(b, &r)
		return r, err
	}
}

func TestWebRejectsInvalidRequestsBeforeStartingRuntime(t *testing.T) {
	c := config.Defaults()
	c.Storage.StateDir = t.TempDir()
	c.Runtime.CodexBinary = "/not-a-runtime"
	w := NewWebChat(context.Background(), c)
	defer w.Close()
	for _, r := range []Request{
		{Version: 2, Method: "web.catalog", Params: json.RawMessage(`{}`)},
		{Version: 1, Method: "web.unknown", Params: json.RawMessage(`{}`)},
		{Version: 1, Method: "web.history", Params: json.RawMessage(`{"thread_id":"../wrong"}`)},
		{Version: 1, Method: "web.send", Owner: "test", Operation: "too-short", Params: json.RawMessage(`{}`)},
	} {
		if _, err := w.Dispatch(context.Background(), r); err == nil {
			t.Fatal("accepted invalid web request", r.Method)
		}
	}
	if len(w.connections) != 0 {
		t.Fatal("invalid request started a runtime")
	}
	if _, err := Dispatch(context.Background(), c, "", Request{Version: 1, Method: "web.catalog"}); err == nil {
		t.Fatal("normal TUI bridge exposed web control")
	}
}

func TestWebBridgeRetiresTerminalControlsAndFullCatalogPolling(t *testing.T) {
	c := config.Defaults()
	c.Path = filepath.Join(t.TempDir(), "absent.toml")
	c.Storage.StateDir = t.TempDir()
	c.Runtime.SharedCodexHome = filepath.Join(t.TempDir(), "no-index")
	c.Runtime.AccountsDir = t.TempDir()
	requests := `{"protocolVersion":1,"id":1,"method":"capabilities","params":{}}
{"protocolVersion":1,"id":2,"method":"snapshot","params":{}}
{"protocolVersion":1,"id":3,"method":"terminal.open","params":{}}
`
	var out bytes.Buffer
	if err := ServeWeb(context.Background(), c, "/unused", strings.NewReader(requests), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatal("missing replies")
	}
	if strings.Contains(lines[0], "terminal.open") || strings.Contains(lines[0], "accounts.activate") || !strings.Contains(lines[0], "web.send") {
		t.Fatal("incorrect web allowlist")
	}
	if strings.Contains(lines[1], "sessions") || !strings.Contains(lines[1], "accounts") {
		t.Fatal("web snapshot performed a full conversation scan")
	}
	if !strings.Contains(lines[2], "not available") {
		t.Fatal("web bridge accepted a retired terminal control")
	}
}

func TestWebAccountPreferencePreservesRetiredPinsWithoutUsingThem(t *testing.T) {
	c := config.Defaults()
	c.Storage.StateDir = t.TempDir()
	c.Runtime.SharedCodexHome = filepath.Join(t.TempDir(), "native-untouched")
	w := NewWebChat(context.Background(), c)
	defer w.Close()
	id := "10000000-0000-4000-8000-000000000001"
	if err := w.editPreferences(func(p *webPreferences) error { p.Pinned = []string{id}; return nil }); err != nil {
		t.Fatal(err)
	}
	w.rememberAccount(id, "example")
	p, err := w.preferences()
	if err != nil || len(p.Pinned) != 1 || p.Accounts[id] != "example" {
		t.Fatal(p, err)
	}
	path := filepath.Join(c.StateDir(), "web-chat", "preferences.json")
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("private pin permissions", err)
	}
	if _, err := os.Stat(config.Expand(c.Runtime.SharedCodexHome)); !os.IsNotExist(err) {
		t.Fatal("pin modified native home", err)
	}
}

func TestWebPendingApprovalsDisappearWhenNativeRequestsResolve(t *testing.T) {
	client := &cr.Client{Events: make(chan cr.Message), Done: make(chan struct{})}
	c := &webConnection{client: client, pending: map[string]cr.Message{
		"7":               {Params: json.RawMessage(`{"threadId":"one"}`)},
		`"other-request"`: {Params: json.RawMessage(`{"threadId":"two"}`)},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { c.events(ctx); close(done) }()
	client.Events <- cr.Message{Method: "serverRequest/resolved", Params: json.RawMessage(`{"threadId":"one","requestId":7}`)}
	client.Events <- cr.Message{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"two"}`)}
	for until := time.Now().Add(time.Second); ; {
		c.mu.Lock()
		count := len(c.pending)
		c.mu.Unlock()
		if count == 0 {
			break
		}
		if time.Now().After(until) {
			t.Fatal("resolved native approvals stayed pending")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
}

func TestWebChatOfficialForeignOwner(t *testing.T) {
	f := newGUIControlFixture(t, "stdio")
	t.Setenv("CODEXM_GUI_FIXTURE_KEY", "fixture-web")
	id := f.thread()
	turn := f.start(id, "GUI_FIXTURE_ACTIVE_FOREIGN")
	select {
	case <-f.active:
	case <-time.After(5 * time.Second):
		t.Fatal("fixture not active")
	}
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	call := webTestCall(t, w, f.ctx)
	history, err := call("web.history", map[string]any{"thread_id": id})
	if err != nil {
		t.Fatal(err)
	}
	if history["thread"].(map[string]any)["externalOwner"] != true {
		t.Fatal("foreign writer not identified")
	}
	for _, method := range []string{"web.send", "web.archive", "web.delete", "web.interrupt"} {
		_, err = call(method, map[string]any{"thread_id": id, "mode": "immediate", "text": "MUST_NOT_EXECUTE", "turn_id": turn, "archived": true})
		if err == nil || !strings.Contains(err.Error(), "another GUI/CLI") {
			t.Fatal("foreign mutation accepted", method, err)
		}
	}
	if _, err = call("web.send", map[string]any{"thread_id": id, "mode": "queue", "text": "FOREIGN_QUEUE_ONLY"}); err != nil {
		t.Fatal(err)
	}
	f.once.Do(func() { close(f.release) })
	completed := f.completed(id, 2)
	b, _ := json.Marshal(completed)
	if !strings.Contains(string(b), "FOREIGN_QUEUE_ONLY") || strings.Contains(string(b), "MUST_NOT_EXECUTE") {
		t.Fatal("queue delivery or rejected input was incorrect")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, key := range f.keys {
		if key != "Bearer fixture-owner-a" {
			t.Fatal("foreign queued input changed execution authentication")
		}
	}
}

func TestWebChatOfficialToolOutput(t *testing.T) {
	f := newGUIControlFixture(t, "stdio")
	f.mu.Lock()
	f.commandFixture = true
	f.mu.Unlock()
	t.Setenv("CODEXM_GUI_FIXTURE_KEY", "fixture-web")
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	call := webTestCall(t, w, f.ctx)
	created, err := call("web.create", map[string]any{"cwd": f.cmd.Dir})
	if err != nil {
		t.Fatal(err)
	}
	id := created["thread"].(map[string]any)["id"].(string)
	if _, err = call("web.send", map[string]any{"thread_id": id, "mode": "immediate", "text": "WEB_COMMAND_FIXTURE"}); err != nil {
		t.Fatal(err)
	}
	var history map[string]any
	for until := time.Now().Add(12 * time.Second); ; {
		history, err = call("web.history", map[string]any{"thread_id": id})
		if err != nil {
			t.Fatal(err)
		}
		turns := history["turns"].([]any)
		if len(turns) == 1 && turns[0].(map[string]any)["status"] == "completed" {
			break
		}
		if time.Now().After(until) {
			t.Fatal("command fixture did not complete")
		}
		time.Sleep(50 * time.Millisecond)
	}
	summary, _ := json.Marshal(history)
	if len(summary) > 15000 {
		t.Fatal("summary eagerly included command output", len(summary))
	}
	turn := history["turns"].([]any)[0].(map[string]any)["id"].(string)
	items, err := call("web.items", map[string]any{"thread_id": id, "turn_id": turn})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range items["data"].([]any) {
		e := entry.(map[string]any)
		item := e["item"].(map[string]any)
		if item["type"] != "commandExecution" {
			continue
		}
		if e["outputDeferred"] != true {
			t.Fatal("large command output not deferred")
		}
		value, err := call("web.item", map[string]any{"thread_id": id, "turn_id": turn, "item_id": item["id"]})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(value)
		if len(b) < 40000 {
			t.Fatal("explicit item read did not return long output", len(b), string(b))
		}
		return
	}
	t.Fatal("native command item missing")
}
