package nativecli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func TestWebExitReleasesOnlyTheSelectedChat(t *testing.T) {
	for _, name := range []string{"idle", "running"} {
		t.Run(name, func(t *testing.T) {
			f := webOnlyFixture(t)
			w := NewWebChat(f.ctx, f.c)
			defer w.Close()
			call := webTestCall(t, w, f.ctx)
			start := func(text string) (string, string) {
				t.Helper()
				created, err := call("web.create", map[string]any{"account": "alpha", "cwd": f.cmd.Dir})
				if err != nil {
					t.Fatal(err)
				}
				id := created["thread"].(map[string]any)["id"].(string)
				result, err := call("web.send", map[string]any{"thread_id": id, "account": "alpha", "mode": "immediate", "text": text})
				if err != nil {
					t.Fatal(err)
				}
				return id, result["turn_id"].(string)
			}
			neighbor, neighborTurn := start("GUI_FIXTURE_ACTIVE_EXIT_NEIGHBOR")
			message, status := "EXIT_SAVED_HISTORY", "completed"
			if name == "running" {
				message, status = "GUI_FIXTURE_ACTIVE_EXIT_TARGET", "inProgress"
			}
			id, turn := start(message)
			owner := w.connections["alpha"]
			waitWebNativeTurn(t, f, owner, id, turn, status)
			home := f.c.Runtime.SharedCodexHome
			if !cr.WriterBusy(home, id) {
				t.Fatal("the web client did not own the chat before exit")
			}
			params := map[string]any{"thread_id": id, "account": "beta"}
			const queuedText = "EXIT_KEEP_THIS_INPUT_QUEUED"
			if name == "running" {
				if _, err := call("web.send", map[string]any{"thread_id": id, "account": "alpha", "mode": "queue", "text": queuedText}); err != nil {
					t.Fatal(err)
				}
				if _, err := call("web.exit", params); !errors.Is(err, errWebAccountBusy) {
					t.Fatal("an active chat exited without a confirmed turn", err)
				}
				params["turn_id"] = neighborTurn
				if _, err := call("web.exit", params); err == nil || !strings.Contains(err.Error(), "현재 작업이 바뀌었습니다") {
					t.Fatal("an unrelated turn was accepted", err)
				}
				if pause, err := w.readQueuePause(id); err != nil || pause != "" {
					t.Fatal("a rejected exit paused the queue", err)
				}
				params["turn_id"] = turn
				status = "interrupted"
			}
			result, err := call("web.exit", params)
			if err != nil || result["status"] != "released" || result["thread_id"] != id || cr.WriterBusy(home, id) {
				t.Fatal("exit did not release the selected writer", result, err)
			}
			if len(w.connections) != 1 || w.connections["alpha"] != owner {
				t.Fatal("exit replaced or opened a server")
			}
			if err := w.verifyOwner(f.ctx, owner, neighbor, true); err != nil {
				t.Fatal("exit affected the neighboring writer", err)
			}
			waitWebNativeTurn(t, f, owner, neighbor, neighborTurn, "inProgress")
			if err := w.dispatchQueued(f.ctx); err != nil || cr.WriterBusy(home, id) {
				t.Fatal("queue dispatch reclaimed an exited chat", err)
			}
			if name == "running" {
				queue, err := w.readOwnedQueue(id)
				if err != nil || len(queue) != 1 || queue[0].Text != queuedText || queue[0].State != "pending" {
					t.Fatal("exit did not preserve pending input", err)
				}
				if pause, err := w.readQueuePause(id); err != nil || pause == "" {
					t.Fatal("exit did not pause the queue", err)
				}
			}
			// A separate official runtime can now take the same thread and history.
			if err := f.client.Call(f.ctx, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true}, nil); err != nil {
				t.Fatal("the second client could not resume the released chat", err)
			}
			owners, err := webHandoffWriters(f.ctx, home, id)
			if err != nil || len(owners) != 1 || owners[0] != f.cmd.Process.Pid {
				t.Fatal("the second client did not acquire the same ID", owners, err)
			}
			waitWebNativeTurn(t, f, &webConnection{client: f.client}, id, turn, status)
			saved, _, err := f.client.Recent(f.ctx, id, 10, "")
			data, _ := json.Marshal(saved.Turns)
			if err != nil || !strings.Contains(string(data), message) || strings.Contains(string(data), queuedText) {
				t.Fatal("the resumed chat did not preserve its saved history", err)
			}
			if _, err := call("web.exit", params); !errors.Is(err, errWebExitOwner) {
				t.Fatal("exit accepted another client's writer", err)
			}
			if !cr.WriterBusy(home, id) {
				t.Fatal("the other client's writer was closed")
			}
		})
	}
}
