package nativecli

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
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
	blank := call("web.history", map[string]any{"thread_id": id})
	if turns, ok := blank["turns"].([]any); !ok || len(turns) != 0 {
		t.Fatal("new draft history is not empty")
	}
	call("web.send", map[string]any{"thread_id": id, "text": "WEB_READABLE_RESPONSE", "mode": "immediate"})
	for until := time.Now().Add(5 * time.Second); ; {
		r := call("web.history", map[string]any{"thread_id": id})
		turns := r["turns"].([]any)
		if len(turns) > 0 && turns[len(turns)-1].(map[string]any)["status"] == "completed" {
			b, _ := json.Marshal(turns)
			t.Log("summary turn payload", string(b))
			break
		}
		if time.Now().After(until) {
			t.Fatal("web turn did not complete")
		}
		time.Sleep(50 * time.Millisecond)
	}
	call("web.pin", map[string]any{"thread_id": id, "pinned": true})
	catalog := call("web.catalog", map[string]any{})
	if len(catalog["pinned"].([]any)) != 1 {
		t.Fatal("pin not persisted")
	}
	call("web.move", map[string]any{"thread_id": id, "project_id": ""})
	call("web.archive", map[string]any{"thread_id": id, "archived": true})
	archived := call("web.history", map[string]any{"thread_id": id})
	if archived["thread"].(map[string]any)["archived"] != true || len(archived["turns"].([]any)) != 1 || len(archived["queue"].([]any)) != 0 {
		t.Fatal("archived history was not readable without changing its state")
	}
	call("web.archive", map[string]any{"thread_id": id, "archived": false})
	call("web.delete", map[string]any{"thread_id": id})
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
