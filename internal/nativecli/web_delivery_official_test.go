package nativecli

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

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
	history, err := call("web.history", map[string]any{"thread_id": id})
	if err != nil {
		t.Fatal(err)
	}
	control, ok := history["delivery"].(map[string]any)
	if !ok || control["state"] != "ready" || control["turnId"] != turn {
		t.Fatal("external writer did not expose the verified active turn", control)
	}
	send := func(text, expected, mode string) (map[string]any, error) {
		return call("web.send", map[string]any{"thread_id": id, "text": text, "mode": mode, "turn_id": expected})
	}
	if _, err := send("STALE_MUST_NOT_EXECUTE", "stale-fixture-turn", "immediate"); err == nil {
		t.Fatal("stale direct input was accepted")
	}
	steer, err := send("WEB_SAME_ACTIVE_TURN", turn, "immediate")
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
	if _, err := send("FINISHED_MUST_NOT_EXECUTE", turn, "immediate"); err == nil {
		t.Fatal("intervention after completion was accepted")
	}
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
		strings.ReplaceAll(string(config), "requires_openai_auth=false", "requires_openai_auth=true") +
		"\n[features]\nplugins=false\napps=false\nhooks=false\nskip_host_skill_discovery=true\n"
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
