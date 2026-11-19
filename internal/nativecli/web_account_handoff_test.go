package nativecli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/catalog"
)

func TestWebOnlyOfficialAccountHandoff(t *testing.T) {
	for _, mode := range []string{"immediate", "queue"} {
		for _, active := range []bool{false, true} {
			t.Run(mode+map[bool]string{true: "-active", false: "-idle"}[active], func(t *testing.T) {
				f := webOnlyFixture(t)
				w := NewWebChat(f.ctx, f.c)
				defer w.Close()
				call := webTestCall(t, w, f.ctx)
				newChat := func() string {
					result, err := call("web.create", map[string]any{"account": "alpha", "cwd": f.cmd.Dir})
					if err != nil {
						t.Fatal(err)
					}
					return result["thread"].(map[string]any)["id"].(string)
				}
				id, other := newChat(), newChat()
				send := func(id, account, mode, text string) map[string]any {
					r, err := call("web.send", map[string]any{"thread_id": id, "account": account, "mode": mode, "text": text})
					if err != nil {
						t.Fatal(err)
					}
					return r
				}
				watch := send(other, "alpha", "immediate", "GUI_FIXTURE_ACTIVE_NEIGHBOR")
				firstMessage := "ALPHA_SAVED_HISTORY"
				if active {
					firstMessage = "GUI_FIXTURE_ACTIVE_ALPHA"
				}
				first := send(id, "alpha", "immediate", firstMessage)
				orig := w.connections["alpha"]
				wantFirst := "completed"
				if active {
					wantFirst = "inProgress"
				}
				waitWebNativeTurn(t, f, orig, id, first["turn_id"].(string), wantFirst)
				if active {
					same := send(id, "alpha", "immediate", "STEER_THIS_ALPHA_TURN")
					if same["mode"] != "steer" || same["turn_id"] != first["turn_id"] {
						t.Fatal("same account did not steer its own turn", same)
					}
				}
				next := send(id, "beta", mode, "BETA_BOUND_INPUT")
				if next["thread_id"] != id {
					t.Fatal("account change altered thread ID", next)
				}
				assertFixtureModelLogin(t, f, "BETA_BOUND_INPUT", "beta")
				chosen := w.connections["beta"]
				if err := w.verifyOwner(f.ctx, chosen, id, true); err != nil {
					t.Fatal("selected login did not gain the writer", err)
				}
				if err := w.verifyLogin(f.ctx, chosen); err != nil {
					t.Fatal("selected identity did not match the new writer", err)
				}
				if active {
					wantFirst = "interrupted"
				}
				waitWebNativeTurn(t, f, chosen, id, first["turn_id"].(string), wantFirst)
				waitWebNativeTurn(t, f, orig, other, watch["turn_id"].(string), "inProgress")
				if err := w.verifyOwner(f.ctx, orig, other, true); err != nil {
					t.Fatal("the neighboring chat lost its owner", err)
				}
				history, err := call("web.history", map[string]any{"thread_id": id, "account": "alpha"})
				if err != nil || history["executionAccount"] != "beta" || history["thread"].(map[string]any)["id"] != id {
					t.Fatal("browser history did not report the actual writer and ID", history, err)
				}
			})
		}
	}
}

func TestWebOnlyOfficialQueueSurvivesBridgeRestart(t *testing.T) {
	f := webOnlyFixture(t)
	w := NewWebChat(f.ctx, f.c)
	call := webTestCall(t, w, f.ctx)
	beta := map[string]any{"account": "beta", "cwd": f.cmd.Dir}
	created, err := call("web.create", beta)
	if err != nil {
		t.Fatal(err)
	}
	id := created["thread"].(map[string]any)["id"].(string)
	_, err = call("web.send", map[string]any{"thread_id": id, "account": "beta", "mode": "immediate", "text": "GUI_FIXTURE_ACTIVE_RESTART"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.active:
	case <-time.After(10 * time.Second):
		t.Fatal("fixture did not start")
	}
	input := map[string]any{"thread_id": id, "account": "beta", "mode": "queue", "text": "EDIT_BOUND_INPUT"}
	q, err := call("web.send", input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := call("web.queueUpdate", map[string]any{"thread_id": id, "account": "alpha", "queued_id": q["queued_id"], "expected_text": input["text"], "text": "DURABLE_BETA_INPUT"}); err != nil {
		t.Fatal(err)
	}
	w.Close()
	f.once.Do(func() { close(f.release) })
	reopened := NewWebChat(f.ctx, f.c)
	defer reopened.Close()
	if err := reopened.dispatchQueued(f.ctx); err != nil {
		t.Fatal(err)
	}
	assertFixtureModelLogin(t, f, "DURABLE_BETA_INPUT", "beta")
	if q, err := reopened.readOwnedQueue(id); err != nil || len(q) != 0 {
		t.Fatal("delivered request was retained or replayed", err)
	}
	if err := reopened.dispatchQueued(f.ctx); err != nil {
		t.Fatal(err)
	}
	assertFixtureModelLogin(t, f, "DURABLE_BETA_INPUT", "beta")
}

func TestWebOnlyExhaustedAccountHandoffPreservesPendingInput(t *testing.T) {
	f := webOnlyFixture(t)
	f.mu.Lock()
	quota := make(chan struct{})
	f.quotaFailure = quota
	f.quotaAccount = "alpha"
	f.mu.Unlock()
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	call := webTestCall(t, w, f.ctx)
	created, err := call("web.create", map[string]any{"account": "alpha", "cwd": f.cmd.Dir})
	if err != nil {
		t.Fatal(err)
	}
	id := created["thread"].(map[string]any)["id"].(string)
	send := func(account, mode, content string, change bool) map[string]any {
		t.Helper()
		result, err := call("web.send", map[string]any{"thread_id": id, "account": account,
			"mode": mode, "text": content, "switch_account": change})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := send("alpha", "immediate", "WEB_FIXTURE_QUOTA_429", false)
	assertFixtureModelLogin(t, f, "WEB_FIXTURE_QUOTA_429", "alpha")
	queued := send("alpha", "queue", "FIXTURE_PRESERVED_OLD_INPUT", false)
	close(quota)
	source := w.connections["alpha"]
	waitWebNativeTurn(t, f, source, id, first["turn_id"].(string), "failed")
	if err := w.verifyOwner(f.ctx, source, id, true); err != nil {
		t.Fatal("the failing login must still own the chat for this test", err)
	}
	next := send("beta", "queue", "FIXTURE_NEW_ACCOUNT_AFTER_LIMIT", true)
	if next["thread_id"] != id || next["handoffError"] != "" {
		t.Fatal("queue did not release the limited session on the same thread", next)
	}
	assertFixtureModelLogin(t, f, "FIXTURE_NEW_ACCOUNT_AFTER_LIMIT", "beta")
	if err := w.dispatchQueued(f.ctx); err != nil {
		t.Fatal(err)
	}
	pending, err := w.readOwnedQueue(id)
	if err != nil || len(pending) != 1 || pending[0].ID != queued["queued_id"] || pending[0].State != "held" ||
		pending[0].Account != "alpha" || pending[0].Text != "FIXTURE_PRESERVED_OLD_INPUT" {
		t.Fatal("the original account's queued input was lost, rebound or replayed", pending, err)
	}
	waitWebNativeTurn(t, f, w.connections["beta"], id, first["turn_id"].(string), "failed")
}

func TestWebOnlyExplicitHandoffFromOfficialSocket(t *testing.T) {
	f := webOnlyFixtureTransport(t, "unix")
	id, neighbor := f.thread(), f.thread()
	oldTurn := f.start(id, "GUI_FIXTURE_ACTIVE_OTHER_CLIENT")
	neighborTurn := f.start(neighbor, "GUI_FIXTURE_ACTIVE_EXTERNAL_NEIGHBOR")
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	call := webTestCall(t, w, f.ctx)
	result, err := call("web.send", map[string]any{"thread_id": id, "account": "beta", "mode": "immediate",
		"text": "EXPLICIT_WEB_HANDOFF_INPUT", "switch_account": true})
	if err != nil || result["thread_id"] != id {
		t.Fatal("verified external owner did not release the requested thread", result, err)
	}
	assertFixtureModelLogin(t, f, "EXPLICIT_WEB_HANDOFF_INPUT", "beta")
	waitWebNativeTurn(t, f, w.connections["beta"], id, oldTurn, "interrupted")
	if entry, err := catalog.Lookup(f.ctx, f.c.Runtime.SharedCodexHome, id); err != nil || entry.Archived {
		t.Fatal("transferred chat was left archived", err)
	}
	current, _, err := f.client.Recent(f.ctx, neighbor, 1, "")
	if err != nil || len(current.Turns) != 1 || current.Turns[0].ID != neighborTurn || current.Turns[0].Status != "inProgress" {
		t.Fatal("external server or neighboring turn was changed", err)
	}
}

func TestWebOnlyExplicitHandoffFromTrackedCLI(t *testing.T) {
	product := os.Getenv("CODEXM_HANDOFF_TEST_BINARY")
	if !filepath.IsAbs(product) {
		t.Skip("requires an isolated CodexM binary and tmux")
	}
	if _, err := tmux(context.Background(), CodexTmuxServer, "has-session", "-t", "="+WorkspaceSession); err != nil {
		t.Skip("run the tracked CLI fixture from the default codexm workspace")
	}
	f := webOnlyFixture(t)
	id := f.thread()
	f.start(id, "INITIAL_TRUSTED_CLI_HISTORY")
	f.completed(id, 1)
	for _, method := range []string{"thread/archive", "thread/unarchive"} {
		if err := f.client.Call(f.ctx, method, map[string]any{"threadId": id}, nil); err != nil {
			t.Fatal(err)
		}
	}
	f.c.Path = filepath.Join(f.cmd.Dir, "fixture.toml")
	if err := f.c.Save(); err != nil {
		t.Fatal(err)
	}
	launch, err := OpenContinuation(f.ctx, f.c, product, id, "alpha", true, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = removeTerminal(f.ctx, launch) })
	for until := time.Now().Add(15 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		source, err := readLast(f.c, id)
		if err == nil {
			err = verifyOwnedCLI(f.ctx, f.c, source)
		}
		if err == nil {
			break
		}
		if time.Now().After(until) {
			screen, _ := Screen(f.ctx, f.c, launch.Session)
			t.Fatalf("launch did not record an owned CLI writer: %v\n%s", err, screen.Text)
		}
	}
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	call := webTestCall(t, w, f.ctx)
	result, err := call("web.send", map[string]any{"thread_id": id, "account": "beta", "mode": "immediate",
		"text": "WEB_FROM_TRACKED_CLI_FIXTURE", "switch_account": true})
	if err != nil || result["thread_id"] != id {
		t.Fatal("verified CLI did not release the selected chat", result, err)
	}
	assertFixtureModelLogin(t, f, "WEB_FROM_TRACKED_CLI_FIXTURE", "beta")
	if err := w.verifyOwner(f.ctx, w.connections["beta"], id, true); err != nil {
		t.Fatal("the selected account is not the new writer", err)
	}
}

func TestWebOnlyUnsentDraftUsesSelectedAccount(t *testing.T) {
	f := webOnlyFixture(t)
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	call := webTestCall(t, w, f.ctx)
	created, err := call("web.create", map[string]any{"account": "alpha", "cwd": f.cmd.Dir})
	if err != nil {
		t.Fatal(err)
	}
	id := created["thread"].(map[string]any)["id"].(string)
	if _, err := call("web.history", map[string]any{"thread_id": id, "account": "beta"}); err != nil {
		t.Fatal("changing the selected account hid the empty draft", err)
	}
	result, err := call("web.send", map[string]any{"thread_id": id, "account": "beta", "mode": "immediate", "text": "BETA_FIRST_INPUT"})
	if err != nil {
		t.Fatal(err)
	}
	newID, _ := result["thread_id"].(string)
	if newID == "" {
		t.Fatal("first input did not return its official address")
	}
	assertFixtureModelLogin(t, f, "BETA_FIRST_INPUT", "beta")
}

func TestWebOnlyWaitsForForeignOwner(t *testing.T) {
	f := webOnlyFixture(t)
	id := f.thread()
	turn := f.start(id, "GUI_FIXTURE_ACTIVE_FOREIGN")
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	call := webTestCall(t, w, f.ctx)
	if _, err := call("web.send", map[string]any{"thread_id": id, "account": "beta", "mode": "immediate", "text": "IMMEDIATE_BLOCKED",
		"switch_account": true}); !errors.Is(err, errWebOtherOwner) {
		t.Fatal("foreign writer received direct input or was interrupted", err)
	}
	if _, err := call("web.send", map[string]any{"thread_id": id, "account": "beta", "mode": "queue", "text": "BETA_AFTER_RELEASE"}); err != nil {
		t.Fatal(err)
	}
	if q, err := readWebQueue(f.ctx, f.client, id); err != nil || len(q.Data) != 0 {
		t.Fatal("another app could consume the web-bound queue", err)
	}
	current, _, err := f.client.Recent(f.ctx, id, 1, "")
	if err != nil || len(current.Turns) != 1 || current.Turns[0].ID != turn || current.Turns[0].Status != "inProgress" {
		t.Fatal("foreign turn was altered", err)
	}
	f.once.Do(func() { close(f.release) })
	f.completed(id, 1)
	// Simulate the original window closing, without a GUI or process takeover.
	f.client.Close()
	select {
	case <-f.client.ProcessDone:
	case <-f.ctx.Done():
		t.Fatal("fixture did not release its writer")
	}
	if err := w.dispatchQueued(f.ctx); err != nil {
		t.Fatal(err)
	}
	assertFixtureModelLogin(t, f, "BETA_AFTER_RELEASE", "beta")
}

func webOnlyFixture(t *testing.T) *guiControlFixture {
	t.Helper()
	return webOnlyFixtureTransport(t, "stdio")
}

func webOnlyFixtureTransport(t *testing.T, transport string) *guiControlFixture {
	t.Helper()
	t.Setenv("CODEXM_WEB_ONLY", "true")
	// The first fixture process is used as a passive reader or foreign writer.
	// Only the later web-owned connections need distinct synthetic logins.
	t.Setenv("CODEXM_GUI_FIXTURE_KEY", "fixture-original")
	f := newGUIControlFixtureDuration(t, transport, 60*time.Second, func(f *guiControlFixture) {
		setupWebFixtureLogins(t, f, "alpha", "beta")
		if transport == "unix" {
			if _, err := accounts.SetNativeDefault(f.c, "alpha"); err != nil {
				t.Fatal(err)
			}
		}
	})
	path := filepath.Join(f.c.Runtime.SharedCodexHome, "config.toml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The local fake model records which file-backed account made each request.
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(b), "env_key='CODEXM_GUI_FIXTURE_KEY'\n", "")), 0600); err != nil {
		t.Fatal(err)
	}
	return f
}

func waitWebNativeTurn(t *testing.T, f *guiControlFixture, c *webConnection, id, turn, state string) {
	t.Helper()
	for until := time.Now().Add(12 * time.Second); time.Now().Before(until); {
		chat, _, err := c.client.Recent(f.ctx, id, 10, "")
		if err == nil {
			for _, got := range chat.Turns {
				if got.ID == turn && got.Status == state {
					return
				}
			}
		}
		time.Sleep(60 * time.Millisecond)
	}
	t.Fatalf("turn did not reach %s", state)
}

func assertFixtureModelLogin(t *testing.T, f *guiControlFixture, marker, login string) {
	t.Helper()
	for until := time.Now().Add(12 * time.Second); time.Now().Before(until); {
		f.mu.Lock()
		matches := 0
		for i, body := range f.bodies {
			if !strings.Contains(body, marker) {
				continue
			}
			matches++
			if actual := fixtureLoginName(f.keys[i]); actual != login {
				f.mu.Unlock()
				t.Fatalf("model input %s was sent under %q, expected %q", marker, actual, login)
			}
		}
		f.mu.Unlock()
		if matches != 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no model request received %s", marker)
}

func fixtureLoginName(header string) string {
	parts := strings.Split(strings.TrimPrefix(header, "Bearer "), ".")
	var claims struct{ Sub string }
	if len(parts) == 3 {
		b, _ := base64.RawURLEncoding.DecodeString(parts[1])
		_ = json.Unmarshal(b, &claims)
	}
	return claims.Sub
}
