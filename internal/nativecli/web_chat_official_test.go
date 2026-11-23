package nativecli

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/catalog"
)

func TestWebChatOfficialCore(t *testing.T) {
	f := newGUIControlFixture(t, "stdio")
	t.Setenv("CODEXM_GUI_FIXTURE_KEY", "fixture-web")
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	count := 0
	call := func(method string, p any) map[string]any {
		t.Helper()
		count++
		b, _ := json.Marshal(p)
		r, err := w.Dispatch(f.ctx, Request{Version: 1, Owner: "fixture-browser", Operation: fmt.Sprintf("00000000-0000-4000-8000-%012d", count), Method: method, Params: b})
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		b, _ = json.Marshal(r)
		var result map[string]any
		if err := json.Unmarshal(b, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	project := call("web.projectCreate", map[string]any{"name": "Web fixture", "cwd": f.cmd.Dir})["project"].(map[string]any)["id"].(string)
	id := call("web.create", map[string]any{"project_id": project, "cwd": f.cmd.Dir})["thread"].(map[string]any)["id"].(string)
	if draft := call("web.thread", map[string]any{"thread_id": id}); draft["id"] != id || draft["account"] != "default" {
		t.Fatal("an open unsent draft cannot be restored by its address", draft)
	}
	blank := call("web.history", map[string]any{"thread_id": id})
	if turns, ok := blank["turns"].([]any); !ok || len(turns) != 0 {
		t.Fatal("new draft history is not empty")
	}
	call("web.send", map[string]any{"thread_id": id, "text": "WEB_READABLE_RESPONSE", "mode": "immediate"})
	var revision string
	for until := time.Now().Add(5 * time.Second); ; {
		r := call("web.history", map[string]any{"thread_id": id})
		turns := r["turns"].([]any)
		if len(turns) > 0 && turns[len(turns)-1].(map[string]any)["status"] == "completed" {
			if revision, _ = r["historyRevision"].(string); revision != "" {
				cached := call("web.history", map[string]any{"thread_id": id, "history_revision": revision})
				if cached["historyUnchanged"] == true && cached["turns"] == nil && cached["queue"] != nil {
					break
				}
			}
		}
		if time.Now().After(until) {
			t.Fatal("web turn did not complete and validate its final revision")
		}
		time.Sleep(50 * time.Millisecond)
	}
	call("web.pin", map[string]any{"thread_id": id, "pinned": true})
	list := call("web.catalog", map[string]any{})
	if len(list["pinned"].([]any)) != 1 {
		t.Fatal("pin not persisted")
	}
	call("web.move", map[string]any{"thread_id": id, "project_id": ""})
	call("web.archive", map[string]any{"thread_id": id, "archived": true})
	archived := call("web.history", map[string]any{"thread_id": id, "history_revision": revision})
	if archived["historyUnchanged"] == true || archived["thread"].(map[string]any)["archived"] != true || len(archived["turns"].([]any)) != 1 || len(archived["queue"].([]any)) != 0 {
		t.Fatal("archived history was not readable without changing its state")
	}
	entry, err := catalog.Lookup(f.ctx, f.c.Runtime.SharedCodexHome, id)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce an older projection-only thread using this fixture's own file.
	original := entry.RolloutPath
	hidden := original + ".fixture-held"
	if err := os.Rename(original, hidden); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := os.Stat(hidden); err == nil {
			if err := os.Rename(hidden, original); err != nil {
				t.Error(err)
			}
		}
	})
	projectedVersion, err := w.historyVersion(f.ctx, id)
	if err != nil || projectedVersion == "" || projectedVersion == archived["historyRevision"] {
		t.Fatal("the projection-only source could not be discovered independently of the rollout", err)
	}
	sources, err := w.historySources(f.ctx)
	if err != nil || !slices.Contains(sources, webHistorySource{id, projectedVersion}) {
		t.Fatal("the projection-only source was not eligible for background reconciliation", err)
	}
	if err := os.Rename(hidden, original); err != nil {
		t.Fatal(err)
	}
	call("web.archive", map[string]any{"thread_id": id, "archived": false})
	call("web.delete", map[string]any{"thread_id": id})
	if gone, err := w.historyVersion(f.ctx, id); err != nil || gone != "" {
		t.Fatal("a deleted chat could still validate its cache", err)
	}
	t.Log("official web create/send/read/pin/project-move/archive/restore/delete completed")
	if c := w.connections["default"]; c != nil {
		c.client.Close()
		select {
		case <-c.client.ProcessDone:
		case <-time.After(3 * time.Second):
			t.Fatal("owned web server did not exit on EOF")
		}
	}
}

func TestWebChatOfficialRequestUserInput(t *testing.T) {
	f := newGUIControlFixture(t, "stdio")
	t.Setenv("CODEXM_GUI_FIXTURE_KEY", "fixture-web-questions")
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	call := webTestCall(t, w, f.ctx)
	created, err := call("web.create", map[string]any{"cwd": f.cmd.Dir})
	if err != nil {
		t.Fatal(err)
	}
	id := created["thread"].(map[string]any)["id"].(string)
	if _, err := call("web.send", map[string]any{"thread_id": id, "mode": "immediate", "text": "WEB_QUESTION_FIXTURE"}); err != nil {
		t.Fatal(err)
	}
	var request webPendingRequest
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if pending := w.activity(id, "default").Pending; len(pending) == 1 {
			request = pending[0]
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if request.Method != "item/tool/requestUserInput" || request.Account != "default" || !strings.Contains(string(request.Params), `"isSecret":true`) {
		t.Fatal("the official connection did not surface the question with its secret-input flag", request.Method)
	}
	reply := map[string]any{"thread_id": id, "account": request.Account, "request_id": request.ID,
		"response": map[string]any{"answers": map[string]any{"approach": map[string]any{"answers": []string{"기본 방식"}}}},
	}
	if _, err := call("web.respond", reply); err == nil {
		t.Fatal("an incomplete answer reached the native thread")
	}
	reply["response"].(map[string]any)["answers"].(map[string]any)["confirmation"] = map[string]any{"answers": []string{"fixture-confirmed-answer"}}
	result, err := call("web.respond", reply)
	if err != nil || result["responded"] != true {
		t.Fatal("answer was not delivered through the waiting official connection", err)
	}
	if _, err := call("web.respond", reply); err == nil {
		t.Fatal("the completed request accepted another answer")
	}
	for deadline := time.Now().Add(6 * time.Second); time.Now().Before(deadline); {
		history, err := call("web.history", map[string]any{"thread_id": id})
		b, _ := json.Marshal(history["turns"])
		if err == nil && strings.Contains(string(b), "웹에서 보낸 답변을 받았습니다.") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the same turn did not receive the answer")
}

func TestWebChatOfficialModelSelection(t *testing.T) {
	f := newGUIControlFixture(t, "stdio")
	t.Setenv("CODEXM_GUI_FIXTURE_KEY", "fixture-web-model-selection")
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	listed, err := w.Dispatch(f.ctx, Request{Version: 1, Method: "web.models", Params: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	var chosen webModel
	for _, model := range listed.([]webModel) {
		if slices.Contains(model.Efforts, "xhigh") {
			chosen = model
			break
		}
	}
	if chosen.Model == "" {
		t.Fatal("official model list has no model with Extra High support")
	}
	call := webTestCall(t, w, f.ctx)
	created, err := call("web.create", map[string]any{"cwd": f.cmd.Dir})
	if err != nil {
		t.Fatal(err)
	}
	thread := created["thread"].(map[string]any)["id"].(string)
	parameters := map[string]any{"thread_id": thread, "mode": "immediate", "text": "WEB_MODEL_SELECTION_FIXTURE", "model": chosen.Model, "effort": "unsupported-effort"}
	if _, err := call("web.send", parameters); err == nil || !strings.Contains(err.Error(), "effort") {
		t.Fatal("accepted an unsupported effort", err)
	}
	// The synthetic Responses provider uses the standard API effort range.
	// Prove a non-default selection reaches that provider without normalization.
	parameters["effort"] = "xhigh"
	parameters["mode"] = "queue"
	if _, err := call("web.send", parameters); err == nil {
		t.Fatal("accepted an override on a queue request")
	}
	parameters["mode"] = "immediate"
	if _, err := call("web.send", parameters); err != nil {
		t.Fatal(err)
	}
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); {
		f.mu.Lock()
		bodies := append([]string{}, f.bodies...)
		f.mu.Unlock()
		for _, body := range bodies {
			if !strings.Contains(body, "WEB_MODEL_SELECTION_FIXTURE") {
				continue
			}
			var request struct {
				Model     string `json:"model"`
				Reasoning struct {
					Effort string `json:"effort"`
				} `json:"reasoning"`
			}
			if err := json.Unmarshal([]byte(body), &request); err != nil {
				t.Fatal(err)
			}
			if request.Model != chosen.Model || request.Reasoning.Effort != parameters["effort"] {
				t.Fatalf("model input got %s/%s, want %s/%s", request.Model, request.Reasoning.Effort, chosen.Model, parameters["effort"])
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("the isolated model provider did not receive the requested turn")
}

func TestWebChatOfficialLiveStream(t *testing.T) {
	f := newGUIControlFixture(t, "stdio")
	t.Setenv("CODEXM_GUI_FIXTURE_KEY", "fixture-web-live")
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	call := webTestCall(t, w, f.ctx)
	created, err := call("web.create", map[string]any{"cwd": f.cmd.Dir})
	if err != nil {
		t.Fatal(err)
	}
	id := created["thread"].(map[string]any)["id"].(string)
	if _, err := call("web.send", map[string]any{"thread_id": id, "mode": "immediate", "text": "GUI_FIXTURE_ACTIVE_BROWSER"}); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(8 * time.Second)
	for {
		activity := w.activity(id, "default")
		if activity.Turn != nil && activity.Threads[id].State == "responding" &&
			slices.ContainsFunc(activity.Turn.Items, func(item webLiveItem) bool {
				return item.Streaming && item.Text == "완료 전에도 보이는 답변"
			}) {
			if !activity.AccountTurn || w.activity(id, "different-login").AccountTurn {
				t.Fatal("observation must preserve the executing account for input")
			}
			return
		}
		if time.Now().After(until) {
			t.Fatalf("missing partial response before model completion: state=%+v turn=%+v", activity.Threads[id], activity.Turn)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
