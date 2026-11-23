package nativecli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func webWithGUIFixture(t *testing.T, f *guiControlFixture) *WebChat {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("desktop GUI routing is macOS only")
	}
	w := NewWebChat(f.ctx, f.c)
	w.webOnly = true
	// Replace only the signed desktop ancestry check. Writer locks, process
	// incarnations, rollout activity and both official servers are real.
	w.identifyGUI = func(ctx context.Context, pid int) (guiOwner, error) {
		if pid != f.cmd.Process.Pid {
			return guiOwner{}, errWebForeignInput
		}
		writer, err := inspectProcess(ctx, pid)
		return guiOwner{Writer: writer}, err
	}
	return w
}

func TestWebChatOfficialIdleGUIQueuePreservesNativeUserInput(t *testing.T) {
	f := newGUIControlFixture(t, "stdio")
	t.Setenv("CODEXM_GUI_FIXTURE_KEY", "fixture-web")
	id, other := f.thread(), f.thread()
	first := f.start(id, "IDLE_GUI_FIRST_TURN")
	f.completed(id, 1)
	f.start(other, "IDLE_GUI_NEIGHBOR")
	f.completed(other, 1)
	pid := f.cmd.Process.Pid
	key, err := cr.NativeProcessIdentity(pid)
	if err != nil {
		t.Fatal(err)
	}
	w := webWithGUIFixture(t, f)
	defer w.Close()
	connection, err := w.connection(f.ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	defer w.releaseConnection(connection)
	call := webTestCall(t, w, f.ctx)
	history, err := call("web.history", map[string]any{"thread_id": id, "limit": 1})
	delivery, _ := history["delivery"].(map[string]any)
	if err != nil || delivery["gui"] != true || delivery["state"] != "unavailable" || delivery["turnId"] != nil {
		t.Fatal("the idle GUI must expose its owner without an active turn", delivery, err)
	}
	const input = "GUI가 작업을 마친 뒤의 원본 입력\n`echo exact`"
	if _, err := call("web.send", map[string]any{"thread_id": id, "mode": "immediate", "text": "Direct must not use the GUI queue"}); !errors.Is(err, errWebGUIRelayOnly) {
		t.Fatal("immediate input was not rejected for the idle GUI", err)
	}
	result, err := call("web.send", map[string]any{"thread_id": id, "mode": "queue", "text": input})
	if err != nil || result["mode"] != "queue" || result["queued_id"] == "" {
		t.Fatal("idle GUI did not use the official queue", result, err)
	}
	thread := f.completed(id, 2)
	if thread.Turns[0].ID != first || thread.Turns[1].ID == first {
		t.Fatal("idle submission did not start a new turn in the same chat")
	}
	messages, err := webTurnMessages(f.ctx, connection, id, thread.Turns[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, raw := range messages {
		var item struct {
			Type    string
			Content []struct{ Type, Text string }
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			t.Fatal(err)
		}
		if item.Type == "userMessage" && len(item.Content) == 1 && item.Content[0].Type == "text" && item.Content[0].Text == input {
			count++
		}
	}
	if count != 1 || f.requests() != 3 || len(webRelayedInputs(f.c, id)) != 0 {
		t.Fatal("official queue did not produce exactly one literal user message without a relay")
	}
	f.assertOwner(key, id, other)
}

func TestWebChatOfficialActiveGUIUsesDedicatedRelay(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("desktop relay is macOS only")
	}
	f := newGUIControlFixture(t, "stdio")
	t.Setenv("CODEXM_GUI_FIXTURE_KEY", "fixture-web")
	target, relay := f.thread(), f.thread()
	f.start(relay, "RELAY_READY")
	f.completed(relay, 1)
	turn := f.start(target, "GUI_FIXTURE_ACTIVE_FOREIGN_BROWSER")
	select {
	case <-f.active:
	case <-f.ctx.Done():
		t.Fatal("target did not start")
	}
	f.c.Runtime.WebGUIRelay = relay
	f.c.Path = filepath.Join(f.cmd.Dir, "fixture.toml")
	w := webWithGUIFixture(t, f)
	defer w.Close()
	const input = "Only the relay tool may submit this exact synthetic input"
	call := webTestCall(t, w, f.ctx)
	history, err := call("web.history", map[string]any{"thread_id": target, "limit": 1})
	delivery, _ := history["delivery"].(map[string]any)
	if err != nil || delivery["gui"] != true || delivery["state"] != "relay" || delivery["turnId"] != turn {
		t.Fatal("the selected GUI chat must expose its active turn", delivery, err)
	}
	if _, err := call("web.exit", map[string]any{"thread_id": target, "turn_id": turn}); !errors.Is(err, errWebExitOwner) {
		t.Fatal("web exit did not protect the GUI owner", err)
	}
	for _, change := range []bool{false, true} {
		if _, err := call("web.send", map[string]any{"thread_id": target, "mode": "immediate", "text": "WRONG_GUI_ROUTE", "switch_account": change}); !errors.Is(err, errWebGUIRelayOnly) {
			t.Fatal("immediate input tried to use or take over the active GUI", err)
		}
	}
	result, err := call("web.send", map[string]any{"thread_id": target, "mode": "relay", "text": input})
	relayID, _ := result["relay_id"].(string)
	if err != nil || result["mode"] != "relay" || !webRelayID.MatchString(relayID) {
		t.Fatal("active GUI did not use the dedicated relay", err)
	}
	saved, err := readGUIRelay(f.c, relayID)
	if err != nil || saved.Thread != target || saved.Relay != relay || saved.State != "pending" ||
		saved.ObservedTurn != turn || len(webRelayedInputs(f.c, target)) != 0 {
		t.Fatal("relay was not bound to the active target or appeared before acknowledgement", err)
	}
	relayed := f.completed(relay, 2)
	b, _ := json.Marshal(relayed.Turns[1].Items)
	if !strings.Contains(string(b), relayID) || strings.Contains(string(b), input) {
		t.Fatal("only the one-time relay command should reach the dedicated queue")
	}
	close(f.releaseForeign)
	f.completed(target, 1)
	idle, err := call("web.send", map[string]any{"thread_id": target, "mode": "relay", "text": "Relay to the same idle GUI"})
	idleID, _ := idle["relay_id"].(string)
	if err != nil || idle["mode"] != "relay" || idleID == relayID || !webRelayID.MatchString(idleID) {
		t.Fatal("idle GUI did not use the explicit relay", idle, err)
	}
	saved, err = readGUIRelay(f.c, idleID)
	if err != nil || saved.Thread != target || saved.Relay != relay || saved.ObservedTurn != "" {
		t.Fatal("idle relay was not bound to the selected chat", saved, err)
	}
	f.completed(relay, 3)
	if queue, err := readWebQueue(f.ctx, f.client, target); err != nil || len(queue.Data) != 0 {
		t.Fatal("explicit relay added input to the target queue", queue, err)
	}
}

func TestWebOnlyGUIQueueWaitsForIdleAndKeepsEdits(t *testing.T) {
	f := newGUIControlFixture(t, "stdio")
	t.Setenv("CODEXM_GUI_FIXTURE_KEY", "fixture-web")
	id := f.thread()
	turn := f.start(id, "GUI_FIXTURE_ACTIVE_FOREIGN_BROWSER")
	select {
	case <-f.active:
	case <-f.ctx.Done():
		t.Fatal("GUI did not start its turn")
	}
	w := webWithGUIFixture(t, f)
	defer w.Close()
	call := webTestCall(t, w, f.ctx)
	enqueue := func(message string) string {
		t.Helper()
		r, err := call("web.send", map[string]any{"thread_id": id, "mode": "queue", "text": message})
		key, _ := r["queued_id"].(string)
		if err != nil || !strings.HasPrefix(key, webQueueID) || r["gui"] != true || r["handoffError"] != "" {
			t.Fatal("GUI input did not enter the web queue", r, err)
		}
		return key
	}
	first, second := enqueue("GUI_QUEUE_OLD"), enqueue("GUI_QUEUE_REMOVE")
	if _, err := call("web.queueUpdate", map[string]any{"thread_id": id, "queued_id": first, "expected_text": "GUI_QUEUE_OLD", "text": "GUI_QUEUE_EDITED"}); err != nil {
		t.Fatal(err)
	}
	if _, err := call("web.queueCancel", map[string]any{"thread_id": id, "queued_id": second, "expected_text": "GUI_QUEUE_REMOVE"}); err != nil {
		t.Fatal(err)
	}
	if err := w.advanceOwnedQueue(f.ctx, id); err != nil {
		t.Fatal(err)
	}
	if native, err := readWebQueue(f.ctx, f.client, id); err != nil || len(native.Data) != 0 || f.requests() != 1 {
		t.Fatal("queue or edit was delivered before the active turn ended", err)
	}
	if err := os.WriteFile(w.queuePausePath(id), []byte("fixture-pause"), 0600); err != nil {
		t.Fatal(err)
	}
	close(f.releaseForeign)
	original := f.completed(id, 1)
	if original.Turns[0].ID != turn {
		t.Fatal("registering a queue entry replaced the GUI turn")
	}
	if err := w.advanceOwnedQueue(f.ctx, id); err != nil {
		t.Fatal(err)
	}
	if q, err := w.readOwnedQueue(id); err != nil || len(q) != 1 || q[0].Text != "GUI_QUEUE_EDITED" || q[0].State != "pending" {
		t.Fatal("paused GUI queue lost its edited text", err)
	}
	if _, err := call("web.queueResume", map[string]any{"thread_id": id, "queue_pause": "fixture-pause"}); err != nil {
		t.Fatal(err)
	}
	identify := w.identifyGUI
	w.identifyGUI = func(context.Context, int) (guiOwner, error) { return guiOwner{}, errWebForeignInput }
	if err := w.advanceOwnedQueue(f.ctx, id); err != nil {
		t.Fatal(err)
	}
	if q, err := w.readOwnedQueue(id); err != nil || len(q) != 1 || q[0].State != "pending" || f.requests() != 1 {
		t.Fatal("queued text escaped its verified GUI owner", err)
	}
	w.identifyGUI = identify
	if err := w.advanceOwnedQueue(f.ctx, id); err != nil {
		t.Fatal(err)
	}
	completed := f.completed(id, 2)
	b, _ := json.Marshal(completed.Turns[1].Items)
	if !strings.Contains(string(b), "GUI_QUEUE_EDITED") || strings.Contains(string(b), "GUI_QUEUE_REMOVE") || strings.Contains(string(b), "GUI_QUEUE_OLD") {
		t.Fatal("GUI received unexpected queue contents")
	}
	enqueue("GUI_QUEUE_ALREADY_IDLE")
	f.completed(id, 3)
	if q, err := w.readOwnedQueue(id); err != nil || len(q) != 0 || f.requests() != 3 || len(webRelayedInputs(f.c, id)) != 0 {
		t.Fatal("idle GUI queue did not run once without a relay", err)
	}
}

func TestWebChatOfficialUnixWriterDelivery(t *testing.T) {
	f := newGUIControlFixture(t, "unix")
	t.Setenv("CODEXM_GUI_FIXTURE_KEY", "fixture-web")
	id, unrelated := f.thread(), f.thread()
	f.start(unrelated, "NEIGHBOR_WRITER_REMAINS")
	f.completed(unrelated, 1)
	turn := f.start(id, "GUI_FIXTURE_ACTIVE_FOREIGN_BROWSER")
	select {
	case <-f.active:
	case <-f.ctx.Done():
		t.Fatal("external active fixture did not start")
	}
	key, err := cr.NativeProcessIdentity(f.cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	call := webTestCall(t, w, f.ctx)
	webConfig := f.c
	webConfig.Storage.StateDir = t.TempDir()
	web := NewWebChat(f.ctx, webConfig)
	web.webOnly = true
	defer web.Close()
	webCall := webTestCall(t, web, f.ctx)
	history, err := webCall("web.history", map[string]any{"thread_id": id})
	if err != nil {
		t.Fatal(err)
	}
	control, ok := history["delivery"].(map[string]any)
	if !ok || control["state"] != "ready" || control["turnId"] != turn {
		t.Fatal("external writer did not expose the verified active turn", control)
	}
	send := func(text, expected, mode string) (map[string]any, error) {
		params := map[string]any{"thread_id": id, "text": text, "mode": mode, "turn_id": expected}
		if mode == "queue" {
			return call("web.send", params)
		}
		return webCall("web.send", params)
	}
	if _, err := send("RELAY_MUST_NOT_REACH_CLI", "", "relay"); err == nil {
		t.Fatal("GUI relay accepted an ordinary external CLI owner")
	}
	if _, err := send("STALE_MUST_NOT_EXECUTE", "stale-fixture-turn", "immediate"); err == nil {
		t.Fatal("stale direct input was accepted")
	}
	steer, err := send("WEB_SAME_ACTIVE_TURN", "", "immediate")
	if err != nil || steer["mode"] != "steer" || steer["turn_id"] != turn {
		t.Fatal("same-turn intervention not acknowledged", steer, err)
	}
	queued, err := send("WEB_QUEUE_AFTER_ACTIVE_TURN", "", "queue")
	if err != nil || queued["mode"] != "queue" || queued["queued_id"] == "" {
		t.Fatal("queue submission not acknowledged", queued, err)
	}
	f.assertOwner(key, id, unrelated)
	close(f.releaseForeign)
	finished := f.completed(id, 2)
	for i, byTurn := range []string{"WEB_SAME_ACTIVE_TURN", "WEB_QUEUE_AFTER_ACTIVE_TURN"} {
		b, _ := json.Marshal(finished.Turns[i])
		if !strings.Contains(string(b), byTurn) || strings.Contains(string(b), "STALE_MUST_NOT_EXECUTE") {
			t.Fatal("input did not reach its intended turn", i+1)
		}
	}
	history, err = call("web.history", map[string]any{"thread_id": id})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(history["turns"]); !strings.Contains(string(b), "WEB_SAME_ACTIVE_TURN") {
		t.Fatal("steered input missing in the web transcript")
	}
	f.assertOwner(key, id, unrelated)
	if _, err := send("FINISHED_MUST_NOT_EXECUTE", "", "steer"); err == nil {
		t.Fatal("intervention after completion was accepted")
	}
	started, err := send("WEB_IDLE_STARTS_NOW", "", "immediate")
	if err != nil || started["mode"] != "start" || started["turn_id"] == "" || started["turn_id"] == turn {
		t.Fatal("new turn on the same idle writer not acknowledged", started, err)
	}
	newTurn := f.completed(id, 3).Turns[2]
	b, _ := json.Marshal(newTurn)
	if newTurn.ID != started["turn_id"] || !strings.Contains(string(b), "WEB_IDLE_STARTS_NOW") || strings.Contains(string(b), "FINISHED_MUST_NOT_EXECUTE") {
		t.Fatal("immediate input did not start the requested turn in the same chat")
	}
	queued, err = send("WEB_IDLE_QUEUE", "", "queue")
	if err != nil || queued["mode"] != "queue" || queued["queued_id"] == "" {
		t.Fatal("queue submission not acknowledged while idle", queued, err)
	}
	b, _ = json.Marshal(f.completed(id, 4).Turns[3])
	if !strings.Contains(string(b), "WEB_IDLE_QUEUE") {
		t.Fatal("native queue input was not executed")
	}
	f.assertOwner(key, id, unrelated)
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, key := range f.keys {
		if key != "Bearer fixture-owner-a" {
			t.Fatal("input changed the original writer's account")
		}
	}
}

func setupWebFixtureLogins(t *testing.T, f *guiControlFixture, names ...string) {
	t.Helper()
	const workspace = "fixture-workspace"
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/api/codex/usage") && f.quotaWindow != "" {
			user := fixtureLoginName(r.Header.Get("Authorization"))
			fiveHour, week := 20, 30
			if user == f.quotaAccount {
				if f.quotaWindow == "5h" {
					fiveHour = 100
				} else {
					week = 100
				}
			}
			window := func(used, seconds int) map[string]any {
				return map[string]any{"used_percent": used, "limit_window_seconds": seconds,
					"reset_after_seconds": 1800, "reset_at": time.Now().Add(30 * time.Minute).Unix()}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"account_id": workspace, "user_id": user, "plan_type": "team",
				"rate_limit": map[string]any{"allowed": fiveHour < 100 && week < 100, "limit_reached": fiveHour == 100 || week == 100,
					"primary_window": window(fiveHour, 300*60), "secondary_window": window(week, 10080*60)}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/accounts/check") {
			_ = json.NewEncoder(w).Encode(map[string]any{"accounts": []any{map[string]any{
				"id": workspace, "name": "Fixture", "plan_type": "team",
				"workspace_backend_origin": "https://fixture.invalid", "account_routing_override": "NO_CONSTRAINT",
			}}})
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(auth.Close)
	t.Setenv("CODEX_REFRESH_TOKEN_URL_OVERRIDE", auth.URL+"/refresh")
	p := filepath.Join(f.c.Runtime.SharedCodexHome, "config.toml")
	config, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	settings := fmt.Sprintf("chatgpt_base_url=%q\n", auth.URL) +
		strings.ReplaceAll(string(config), "requires_openai_auth=false", "requires_openai_auth=true")
	if err := os.WriteFile(p, []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := cr.EnsureAccountHome(f.c, name); err != nil {
			t.Fatal(err)
		}
		claims, _ := json.Marshal(map[string]any{
			"sub": name, "exp": time.Now().Add(time.Hour).Unix(), "email": name + "@example.invalid",
			"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": workspace, "chatgpt_user_id": name, "chatgpt_plan_type": "team"},
		})
		token := "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".fixture" // gitleaks:allow -- unsigned, synthetic test JWT
		data, _ := json.Marshal(map[string]any{
			"auth_mode": "chatgpt", "last_refresh": time.Now().UTC().Format(time.RFC3339),
			"tokens": map[string]string{"id_token": token, "access_token": token, "refresh_token": "fixture-refresh-" + name, "account_id": workspace},
		})
		if err := os.WriteFile(filepath.Join(f.c.Runtime.AccountsDir, name, "auth.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWebChatOfficialRefreshDefaultAfterQueuedWork(t *testing.T) {
	f := newGUIControlFixture(t, "stdio")
	t.Setenv("CODEXM_GUI_FIXTURE_KEY", "fixture-web")
	setupWebFixtureLogins(t, f, "first-login", "second-login", "separate-login")
	if _, err := accounts.SetNativeDefault(f.c, "first-login"); err != nil {
		t.Fatal(err)
	}
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	call := webTestCall(t, w, f.ctx)
	if _, err := w.Dispatch(f.ctx, Request{Version: 1, Method: "web.models", Params: json.RawMessage(`{"account":"separate-login"}`)}); err != nil {
		t.Fatal("separate connection not opened for the fixture", err)
	}
	unrelated := w.connections["separate-login"]
	created, err := call("web.create", map[string]any{"cwd": f.cmd.Dir})
	if err != nil {
		t.Fatal(err)
	}
	id := created["thread"].(map[string]any)["id"].(string)
	old := w.connections["default"]
	started, err := call("web.send", map[string]any{"thread_id": id, "mode": "immediate", "text": "GUI_FIXTURE_ACTIVE_ROTATION"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-f.active:
	case <-f.ctx.Done():
		t.Fatal("first account did not start its fixture turn")
	}
	if _, err := call("web.send", map[string]any{"thread_id": id, "mode": "queue", "text": "FINISH_BEFORE_ROTATION"}); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.SetNativeDefault(f.c, "second-login"); err != nil {
		t.Fatal(err)
	}
	history, err := call("web.history", map[string]any{"thread_id": id})
	if err != nil || history["loginDraining"] != true || w.connections["default"] != old {
		t.Fatal("existing web work was interrupted or became unreadable", err)
	}
	if _, err := call("web.create", map[string]any{"cwd": f.cmd.Dir}); !errors.Is(err, errWebLoginDraining) {
		t.Fatal("new work was admitted under the previous login", err)
	}
	if _, err := call("web.send", map[string]any{
		"thread_id": id, "mode": "immediate", "turn_id": started["turn_id"], "text": "STEER_BEFORE_ROTATION",
	}); err != nil {
		t.Fatal("same-turn input did not retain the original account", err)
	}
	f.once.Do(func() { close(f.release) })
	for until := time.Now().Add(12 * time.Second); w.connections["default"] == old || w.connections["default"] == nil; {
		if time.Now().After(until) {
			t.Fatal("default connection did not rotate", err)
		}
		_, err = call("web.history", map[string]any{"thread_id": id})
		time.Sleep(75 * time.Millisecond)
	}
	select {
	case <-old.client.ProcessDone:
	default:
		t.Fatal("old web connection was not reaped before its replacement")
	}
	now := w.connections["default"]
	if now.login.Key == old.login.Key || now.login.Revision != accounts.InspectIdentity(f.c, "default").Revision ||
		now.generation == old.generation || w.connections["separate-login"] != unrelated {
		t.Fatal("rotation affected an unrelated connection or selected the wrong login")
	}
	if err := w.verifyLogin(f.ctx, now); err != nil {
		t.Fatal("new default identity did not match the live app-server", err)
	}
	finished := f.completed(id, 2)
	for i, want := range []string{"STEER_BEFORE_ROTATION", "FINISH_BEFORE_ROTATION"} {
		data, _ := json.Marshal(finished.Turns[i])
		if !strings.Contains(string(data), want) {
			t.Fatal("old account did not finish both the active and pending work", i)
		}
	}
}
