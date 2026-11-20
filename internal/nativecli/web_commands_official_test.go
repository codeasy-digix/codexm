package nativecli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWebOnlyOfficialPlanStartsAfterActiveTurn(t *testing.T) {
	f := webOnlyFixture(t)
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	call := webTestCall(t, w, f.ctx)
	created, err := call("web.create", map[string]any{"account": "alpha", "cwd": f.cmd.Dir})
	if err != nil {
		t.Fatal(err)
	}
	id := created["thread"].(map[string]any)["id"].(string)
	connection := w.connections["alpha"]
	send := func(text string) map[string]any {
		t.Helper()
		result, err := call("web.send", map[string]any{"thread_id": id, "account": "alpha", "mode": "immediate", "text": text, "switch_account": true})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := send("GUI_FIXTURE_ACTIVE_BEFORE_PLAN")["turn_id"].(string)
	assertFixtureModelLogin(t, f, "GUI_FIXTURE_ACTIVE_BEFORE_PLAN", "alpha")
	queued := send("/plan WEB_FIXTURE_PLAN_NEXT_TURN")
	if queued["mode"] != "queue" || queued["plan"] != true {
		t.Fatal("plan was not queued for a new turn", queued)
	}
	if pending, err := w.readOwnedQueue(id); err != nil || len(pending) != 1 || !pending[0].Plan {
		t.Fatal("plan metadata was not preserved across queue dispatch", err)
	}
	waitWebNativeTurn(t, f, connection, id, first, "inProgress")
	f.mu.Lock()
	f.responseText = "<proposed_plan>\n## 샘플 계획\n\n검증 절차입니다.\n</proposed_plan>"
	f.mu.Unlock()
	f.once.Do(func() { close(f.release) })
	waitWebNativeTurn(t, f, connection, id, first, "completed")
	var planned string
	for until := time.Now().Add(8 * time.Second); time.Now().Before(until); {
		if err := w.advanceOwnedQueue(f.ctx, id); err != nil {
			t.Fatal(err)
		}
		turns, err := recentWebTurns(f.ctx, connection.client, id, 2)
		if err == nil && len(turns) == 2 && turns[0].ID != first {
			planned = turns[0].ID
			break
		}
		time.Sleep(80 * time.Millisecond)
	}
	if planned == "" {
		pending, _ := w.readOwnedQueue(id)
		t.Fatalf("queued planning turn did not start: %+v", pending)
	}
	waitWebNativeTurn(t, f, connection, id, planned, "completed")
	if mode := fixtureTurnMode(t, f, connection, id, planned); mode != "plan" {
		t.Fatal("the native turn did not enter Plan mode", mode)
	}
	if history, err := call("web.history", map[string]any{"thread_id": id, "account": "alpha"}); err != nil || !hasWebCommandItem(history, "plan") {
		t.Fatal("the proposed plan was not returned in web history", err)
	}
	f.mu.Lock()
	f.responseText = "ordinary follow-up"
	f.mu.Unlock()
	following := send("WEB_FIXTURE_PLAN_RETURN_TO_DEFAULT")["turn_id"].(string)
	waitWebNativeTurn(t, f, connection, id, following, "completed")
	if mode := fixtureTurnMode(t, f, connection, id, following); mode != "default" {
		t.Fatal("a normal follow-up inherited the planning override", mode)
	}
}

func fixtureTurnMode(t *testing.T, f *guiControlFixture, c *webConnection, id, turn string) string {
	t.Helper()
	thread, err := c.client.Read(f.ctx, id, false)
	if err != nil || thread.Path == "" {
		t.Fatal("synthetic rollout was not saved", err)
	}
	b, err := os.ReadFile(thread.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		var event struct {
			Type    string
			Payload struct {
				Type   string
				TurnID string `json:"turn_id"`
				Mode   string `json:"collaboration_mode_kind"`
			}
		}
		if json.Unmarshal([]byte(line), &event) == nil && event.Type == "event_msg" &&
			event.Payload.Type == "task_started" && event.Payload.TurnID == turn {
			return event.Payload.Mode
		}
	}
	return ""
}

func hasWebCommandItem(history map[string]any, kind string) bool {
	turns, _ := history["turns"].([]any)
	for _, value := range turns {
		turn, _ := value.(map[string]any)
		items, _ := turn["items"].([]any)
		for _, value := range items {
			if item, ok := value.(map[string]any); ok && item["type"] == kind {
				return true
			}
		}
	}
	return false
}

func TestWebOnlyOfficialRenameReviewAndCompact(t *testing.T) {
	f := webOnlyFixture(t)
	for _, args := range [][]string{{"init", "-q", "-b", "main"},
		{"-c", "user.name=Fixture", "-c", "user.email=fixture@example.com", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "synthetic base"}} {
		git := exec.CommandContext(f.ctx, "git", args...)
		git.Dir = f.cmd.Dir
		if output, err := git.CombinedOutput(); err != nil {
			t.Fatalf("seed isolated Git history: %v %s", err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(f.cmd.Dir, "fixture-source.txt"), []byte("synthetic worktree change\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	call := webTestCall(t, w, f.ctx)
	created, err := call("web.create", map[string]any{"account": "alpha", "cwd": f.cmd.Dir})
	if err != nil {
		t.Fatal(err)
	}
	id := created["thread"].(map[string]any)["id"].(string)
	connection := w.connections["alpha"]
	first, err := call("web.send", map[string]any{"thread_id": id, "account": "alpha", "mode": "immediate", "text": "GUI_FIXTURE_ACTIVE_BLOCKS_COMMANDS"})
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureModelLogin(t, f, "GUI_FIXTURE_ACTIVE_BLOCKS_COMMANDS", "alpha")
	for _, method := range []string{"web.review", "web.compact"} {
		if result, err := call(method, map[string]any{"thread_id": id, "account": "alpha"}); err == nil {
			t.Fatal("command accepted while a native turn was active", method, result)
		}
	}
	const name = "공식 이름 변경 검증"
	if result, err := call("web.rename", map[string]any{"thread_id": id, "account": "alpha", "name": name}); err != nil || result["name"] != name {
		t.Fatal("native rename was not confirmed", result, err)
	}
	waitWebNativeTurn(t, f, connection, id, first["turn_id"].(string), "inProgress")
	f.once.Do(func() { close(f.release) })
	waitWebNativeTurn(t, f, connection, id, first["turn_id"].(string), "completed")
	if saved, err := w.thread(f.ctx, id); err != nil || saved == nil || saved.Name != name {
		t.Fatal("the original metadata did not reflect the new title", err)
	}
	modelCalls := f.requests()
	forked, err := call("web.fork", map[string]any{"thread_id": id, "account": "alpha"})
	if err != nil || forked["status"] != "accepted" {
		t.Fatal("native fork was not confirmed", forked, err)
	}
	forkID := forked["thread"].(map[string]any)["id"].(string)
	if forkID == id || !threadID.MatchString(forkID) {
		t.Fatal("fork reused the original chat ID")
	}
	copyHistory, err := call("web.history", map[string]any{"thread_id": forkID, "account": "alpha"})
	if err != nil || !hasWebCommandItem(copyHistory, "userMessage") || f.requests() != modelCalls {
		t.Fatal("fork did not retain the source conversation without a new model request", err)
	}
	if saved, err := w.thread(f.ctx, id); err != nil || saved == nil || saved.Name != name || saved.Archived {
		t.Fatal("the original changed after fork", err)
	}

	f.mu.Lock()
	f.responseText = `{"findings":[],"overall_correctness":"patch is correct","overall_explanation":"Synthetic review completed","overall_confidence_score":0.95}`
	f.mu.Unlock()
	args := map[string]any{"thread_id": id, "account": "alpha", "branch": "main"}
	review, err := call("web.review", args)
	if err != nil || review["thread_id"] != id || review["status"] != "accepted" {
		t.Fatal("inline official review was not started", review, err)
	}
	waitWebNativeTurn(t, f, connection, id, review["turn_id"].(string), "completed")
	if history, err := call("web.history", args); err != nil || !hasWebCommandItem(history, "exitedReviewMode") {
		t.Fatal("native review result is missing from web history", err)
	}
	f.mu.Lock()
	f.responseText = "Synthetic context summary"
	f.mu.Unlock()
	if result, err := call("web.compact", args); err != nil || result["status"] != "accepted" {
		t.Fatal("official compaction was not started", result, err)
	}
	for until := time.Now().Add(8 * time.Second); time.Now().Before(until); {
		history, err := call("web.history", args)
		if err == nil && hasWebCommandItem(history, "contextCompaction") {
			return
		}
		time.Sleep(80 * time.Millisecond)
	}
	t.Fatal("native compaction result is missing from web history")
}
