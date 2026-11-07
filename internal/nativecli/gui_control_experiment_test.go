package nativecli

import (
	"context"
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

// Research only: an official app-server with GUI-like stdio ownership, isolated
// stores and a local model fixture. This does not automate or restart a real GUI.
type guiControlFixture struct {
	t       *testing.T
	ctx     context.Context
	c       config.Config
	binary  string
	cmd     *exec.Cmd
	client  *cr.Client
	socket  string
	active  chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	bodies  []string
}

func newGUIControlFixture(t *testing.T, transport string) *guiControlFixture {
	t.Helper()
	if os.Getenv("CODEXM_GUI_CONTROL_TEST") != "1" {
		t.Skip("opt-in GUI ownership protocol experiment")
	}
	binary := os.Getenv("CODEXM_NATIVE_RUNTIME")
	if !filepath.IsAbs(binary) {
		t.Fatal("absolute official Codex binary required")
	}
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Fatal("lsof is required to verify that the original writer is preserved")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	f := &guiControlFixture{t: t, ctx: ctx, binary: binary, active: make(chan struct{}), release: make(chan struct{})}
	var activeOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/responses") {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, string(body))
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"fixture-response\"}}\n\n")
		w.(http.Flusher).Flush()
		if strings.Contains(string(body), "GUI_FIXTURE_ACTIVE") {
			activeOnce.Do(func() { close(f.active) })
			select {
			case <-f.release:
			case <-r.Context().Done():
				return
			}
		}
		fmt.Fprint(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"fixture-message\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"fixture complete\"}]}}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"fixture-response\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
	f.c = config.Defaults()
	f.c.Runtime.SharedCodexHome = filepath.Join(home, "shared")
	f.c.Runtime.AccountsDir = filepath.Join(home, "accounts")
	f.c.Storage.StateDir = filepath.Join(home, "state")
	f.c.Runtime.CodexBinary = binary
	if err := os.MkdirAll(f.c.Runtime.SharedCodexHome, 0700); err != nil {
		t.Fatal(err)
	}
	settings := fmt.Sprintf("model='fixture'\nmodel_provider='fixture'\ncli_auth_credentials_store='file'\ncheck_for_update_on_startup=false\napproval_policy='never'\n[model_providers.fixture]\nname='fixture'\nbase_url=%q\nwire_api='responses'\nrequires_openai_auth=false\n[projects.%q]\ntrust_level='trusted'\n", server.URL, home)
	if err := os.WriteFile(filepath.Join(f.c.Runtime.SharedCodexHome, "config.toml"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	endpoint := "stdio://"
	if transport == "unix" {
		short, err := os.MkdirTemp("/tmp", "codexm-gui-rpc-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(short) })
		f.socket = filepath.Join(short, "control.sock")
		endpoint = "unix://" + f.socket
	}
	f.cmd = exec.CommandContext(ctx, binary, "--no-daemon", "--disable", "plugins", "--disable", "apps", "--disable", "hooks", "--enable", "skip_host_skill_discovery", "app-server", "--listen", endpoint)
	f.cmd.Env, f.cmd.Dir = environment(f.c, "default"), home
	var err error
	var processDone <-chan struct{}
	if transport == "stdio" {
		f.client, err = cr.StartStdio(ctx, f.cmd)
		if f.client != nil {
			processDone = f.client.ProcessDone
		}
	} else {
		err = f.cmd.Start()
		if err == nil {
			done := make(chan struct{})
			processDone = done
			go func() { _ = f.cmd.Wait(); close(done) }()
			for until := time.Now().Add(8 * time.Second); time.Now().Before(until); {
				f.client, err = cr.DialUnix(ctx, f.socket)
				if err == nil {
					break
				}
				time.Sleep(40 * time.Millisecond)
			}
		}
	}
	t.Cleanup(func() {
		f.once.Do(func() { close(f.release) })
		if f.client != nil {
			f.client.Close()
		}
		if f.cmd.Process != nil {
			cancel() // This fixture's process only; never a user's GUI/server.
			if processDone != nil {
				<-processDone
			}
		}
		server.Close()
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *guiControlFixture) thread() string {
	f.t.Helper()
	var r struct{ Thread cr.Thread }
	if err := f.client.Call(f.ctx, "thread/start", map[string]any{"cwd": f.cmd.Dir, "sandbox": "read-only", "approvalPolicy": "never"}, &r); err != nil {
		f.t.Fatal(err)
	}
	return r.Thread.ID
}

func (f *guiControlFixture) start(id, text string) string {
	f.t.Helper()
	var r struct{ Turn cr.Turn }
	if err := f.client.Call(f.ctx, "turn/start", map[string]any{"threadId": id, "input": cr.TextInput(text)}, &r); err != nil {
		f.t.Fatal(err)
	}
	return r.Turn.ID
}

func (f *guiControlFixture) completed(id string, count int) cr.Thread {
	f.t.Helper()
	for until := time.Now().Add(15 * time.Second); time.Now().Before(until); {
		thread, _, err := f.client.Recent(f.ctx, id, 30, "")
		done := 0
		for _, turn := range thread.Turns {
			if turn.Status == "completed" {
				done++
			}
		}
		if err == nil && done == count && len(thread.Turns) == count {
			return thread
		}
		time.Sleep(50 * time.Millisecond)
	}
	f.t.Fatalf("expected %d completed turns", count)
	return cr.Thread{}
}

func (f *guiControlFixture) queueEndpoint() string {
	f.t.Helper()
	if f.socket == "" {
		// Model the separate shared control server without starting/installing
		// a daemon. The original GUI-like stdio process keeps its writer.
		short, err := os.MkdirTemp("/tmp", "codexm-queue-relay-")
		if err != nil {
			f.t.Fatal(err)
		}
		f.socket = filepath.Join(short, "control.sock")
		cmd := exec.CommandContext(f.ctx, f.binary, "--no-daemon", "--disable", "plugins", "--disable", "apps", "--disable", "hooks", "--enable", "skip_host_skill_discovery", "app-server", "--listen", "unix://"+f.socket)
		cmd.Env, cmd.Dir = environment(f.c, "default"), f.cmd.Dir
		if err := cmd.Start(); err != nil {
			f.t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		f.t.Cleanup(func() { _ = cmd.Process.Kill(); <-done; os.RemoveAll(short) })
		for until := time.Now().Add(5 * time.Second); ; {
			probe, err := cr.DialUnix(f.ctx, f.socket)
			if err == nil {
				probe.Close()
				break
			}
			if time.Now().After(until) {
				f.t.Fatal("isolated queue relay unavailable", err)
			}
			time.Sleep(40 * time.Millisecond)
		}
	}
	return "unix://" + f.socket
}

func (f *guiControlFixture) queue(id, text string) {
	f.t.Helper()
	cmd := exec.CommandContext(f.ctx, f.binary, "queue", "--remote", f.queueEndpoint(), "--thread", id, "--message", text)
	cmd.Env, cmd.Dir = environment(f.c, "default"), f.cmd.Dir
	if b, err := cmd.CombinedOutput(); err != nil {
		f.t.Fatalf("queue: %v: %s", err, b)
	}
}

func TestGUIControlDirectQueueRPCNeverLoadsTheRelay(t *testing.T) {
	f := newGUIControlFixture(t, "stdio")
	id := f.thread()
	f.start(id, "GUI_FIXTURE_ACTIVE")
	select {
	case <-f.active:
	case <-f.ctx.Done():
		t.Fatal("owner did not start its turn")
	}
	cmd := exec.CommandContext(f.ctx, f.binary, "--no-daemon", "--disable", "plugins", "--disable", "apps", "--disable", "hooks", "--enable", "skip_host_skill_discovery", "app-server", "--listen", "stdio://")
	cmd.Env, cmd.Dir = environment(f.c, "default"), f.cmd.Dir
	relay, err := cr.StartStdio(f.ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		relay.Close()
		select {
		case <-relay.ProcessDone:
		case <-time.After(3 * time.Second):
			t.Error("owned unloaded relay did not exit on stdin EOF")
			_ = cmd.Process.Kill() // Isolated fixture only.
			<-relay.ProcessDone
		}
	})
	request := "00000000-0000-4000-8000-000000000012"
	var ack struct {
		QueuedSubmission struct {
			ID                  string `json:"id"`
			ClientUserMessageID string `json:"clientUserMessageId"`
		}
	}
	err = relay.Call(f.ctx, "thread/queue/add", map[string]any{"threadId": id, "clientUserMessageId": request, "input": cr.TextInput("GUI_FIXTURE_DIRECT_QUEUE")}, &ack)
	if err != nil || ack.QueuedSubmission.ClientUserMessageID != request || ack.QueuedSubmission.ID == "" {
		t.Fatal("direct queue acknowledgement", err)
	}
	var loaded struct{ Data []string }
	if err := relay.Call(f.ctx, "thread/loaded/list", map[string]any{}, &loaded); err != nil || len(loaded.Data) != 0 {
		t.Fatal("queue relay must not own or resume any chat", err)
	}
	if f.requests() != 1 {
		t.Fatal("queue interrupted the active GUI-like turn")
	}
	f.once.Do(func() { close(f.release) })
	f.completed(id, 2)
	var page struct{ Data []json.RawMessage }
	if err := relay.Call(f.ctx, "thread/turns/list", map[string]any{"threadId": id, "limit": 5, "itemsView": "full", "sortDirection": "desc"}, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 2 || !strings.Contains(string(page.Data[0]), request) || !strings.Contains(string(page.Data[0]), `"completed"`) {
		t.Fatal("native transcript did not preserve the client request ID")
	}
	writers, err := writerProcesses(f.ctx, f.c.Runtime.SharedCodexHome, id)
	if err != nil || len(writers) != 1 || writers[0] != f.cmd.Process.Pid {
		t.Fatal("direct queue replaced its GUI-like owner", err)
	}
}

func (f *guiControlFixture) requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies)
}

func (f *guiControlFixture) assertOwner(key, id, other string) {
	f.t.Helper()
	current, err := cr.NativeProcessIdentity(f.cmd.Process.Pid)
	if err != nil || current != key || !cr.WriterBusy(f.c.Runtime.SharedCodexHome, id) || !cr.WriterBusy(f.c.Runtime.SharedCodexHome, other) {
		f.t.Fatal("original server or one of its writer locks was changed", err)
	}
	for _, thread := range []string{id, other} {
		writers, err := writerProcesses(f.ctx, f.c.Runtime.SharedCodexHome, thread)
		if err != nil || len(writers) != 1 || writers[0] != f.cmd.Process.Pid {
			f.t.Fatal("writer ownership moved away from the original server", err)
		}
	}
}

func TestGUIControlQueuePreservesStdioOwner(t *testing.T) {
	f := newGUIControlFixture(t, "stdio")
	control, id := f.thread(), f.thread()
	f.start(control, "GUI_FIXTURE_CONTROL")
	controlBefore := f.completed(control, 1)
	key, err := cr.NativeProcessIdentity(f.cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	originalTurn := f.start(id, "GUI_FIXTURE_ACTIVE")
	select {
	case <-f.active:
	case <-f.ctx.Done():
		t.Fatal("active fixture did not reach the model")
	}
	f.queue(id, "GUI_FIXTURE_DUPLICATE")
	f.queue(id, "GUI_FIXTURE_DUPLICATE")
	time.Sleep(200 * time.Millisecond)
	if f.requests() != 2 {
		t.Fatal("queue interrupted the active turn or started a concurrent request")
	}
	f.assertOwner(key, id, control)
	f.once.Do(func() { close(f.release) })
	thread := f.completed(id, 3)
	if thread.Turns[0].ID != originalTurn || f.requests() != 4 {
		t.Fatal("queue replaced the initial turn or did not execute both explicit submissions")
	}
	for _, turn := range thread.Turns[1:] {
		b, _ := json.Marshal(turn.Items)
		if !strings.Contains(string(b), "GUI_FIXTURE_DUPLICATE") {
			t.Fatal("queued user message missing from history")
		}
	}
	f.queue(id, "GUI_FIXTURE_IDLE")
	f.completed(id, 4)
	f.assertOwner(key, id, control)
	controlAfter := f.completed(control, 1)
	before, _ := json.Marshal(controlBefore.Turns)
	after, _ := json.Marshal(controlAfter.Turns)
	if string(before) != string(after) || f.requests() != 5 {
		t.Fatal("queue affected a neighboring chat or replayed a submission")
	}
	// Official archive/unarchive unloads just this synthetic thread. A queue
	// acknowledgement alone must not be confused with starting an unloaded chat.
	if err := f.client.Call(f.ctx, "thread/archive", map[string]any{"threadId": id}, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.client.Call(f.ctx, "thread/unarchive", map[string]any{"threadId": id}, nil); err != nil {
		t.Fatal(err)
	}
	if cr.WriterBusy(f.c.Runtime.SharedCodexHome, id) {
		t.Fatal("fixture was not unloaded")
	}
	f.queue(id, "GUI_FIXTURE_UNLOADED")
	time.Sleep(400 * time.Millisecond)
	if f.requests() != 5 || cr.WriterBusy(f.c.Runtime.SharedCodexHome, id) {
		t.Fatal("queue without --resume unexpectedly started an unloaded chat")
	}
	if _, err := f.client.Resume(f.ctx, id); err != nil {
		t.Fatal(err)
	}
	f.completed(id, 5)
	f.assertOwner(key, id, control)
	if f.requests() != 6 {
		t.Fatal("explicit owner resume did not consume the unloaded thread's pending input once")
	}
	t.Log("active queue waited; duplicate submissions produced two turns; loaded idle queue ran; unloaded queue waited for owner resume; original server and neighboring chat preserved")
}

func TestGUIControlSteerNeedsTheOwningConnection(t *testing.T) {
	for _, transport := range []string{"stdio", "unix"} {
		t.Run(transport, func(t *testing.T) {
			f := newGUIControlFixture(t, transport)
			id, other := f.thread(), f.thread()
			turn := f.start(id, "GUI_FIXTURE_ACTIVE")
			select {
			case <-f.active:
			case <-f.ctx.Done():
				t.Fatal("active fixture did not start")
			}
			key, err := cr.NativeProcessIdentity(f.cmd.Process.Pid)
			if err != nil {
				t.Fatal(err)
			}
			controller := f.client
			if transport == "unix" {
				controller, err = cr.DialUnix(f.ctx, f.socket)
				if err != nil {
					t.Fatal(err)
				}
				defer controller.Close()
			}
			params := map[string]any{"threadId": id, "expectedTurnId": "stale-fixture-turn", "input": cr.TextInput("GUI_FIXTURE_STALE")}
			if err := controller.Call(f.ctx, "turn/steer", params, nil); err == nil {
				t.Fatal("stale expected turn was accepted")
			}
			params["expectedTurnId"], params["input"] = turn, cr.TextInput("GUI_FIXTURE_STEER")
			if transport == "stdio" {
				f.queueEndpoint()
				peer, err := cr.DialUnix(f.ctx, f.socket)
				if err != nil {
					t.Fatal(err)
				}
				defer peer.Close()
				if err := peer.Call(f.ctx, "turn/steer", params, nil); err == nil {
					t.Fatal("a separate server steered a thread it does not own")
				}
			}
			var accepted struct{ TurnID string }
			if err := controller.Call(f.ctx, "turn/steer", params, &accepted); err != nil || accepted.TurnID != turn {
				t.Fatal("owner did not accept steering for the active turn", err)
			}
			f.once.Do(func() { close(f.release) })
			thread := f.completed(id, 1)
			if thread.Turns[0].ID != turn {
				t.Fatal("steering created a different turn")
			}
			f.assertOwner(key, id, other)
			f.mu.Lock()
			bodies := append([]string{}, f.bodies...)
			f.mu.Unlock()
			if len(bodies) != 2 || !strings.Contains(bodies[1], "GUI_FIXTURE_STEER") || strings.Contains(bodies[1], "GUI_FIXTURE_STALE") {
				t.Fatal("steering was not consumed exactly once or stale input reached the model")
			}
			if err := controller.Call(f.ctx, "turn/steer", params, nil); err == nil {
				t.Fatal("steering an idle turn succeeded")
			}
			t.Log("steer kept one turn and the original owner; stale and idle steer rejected; external client works only on the owner's exposed Unix transport")
		})
	}
}
