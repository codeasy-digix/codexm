package nativecli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func TestWebChatOfficialQueueEditsPreservePendingOrder(t *testing.T) {
	for _, transport := range []string{"unix", "stdio"} {
		t.Run(transport, func(t *testing.T) {
			f := newGUIControlFixture(t, transport)
			t.Setenv("CODEXM_GUI_FIXTURE_KEY", "fixture-web")
			id, adjacent := f.thread(), f.thread()
			f.start(adjacent, "OTHER_CHAT_REMAINS")
			f.completed(adjacent, 1)
			active := f.start(id, "GUI_FIXTURE_ACTIVE")
			select {
			case <-f.active:
			case <-f.ctx.Done():
				t.Fatal("fixture owner did not start")
			}
			key, err := cr.NativeProcessIdentity(f.cmd.Process.Pid)
			if err != nil {
				t.Fatal(err)
			}
			const firstText = "NATIVE_QUEUE_ORIGINAL"
			const clientID = "00000000-0000-4000-8000-000000000071"
			var old struct {
				Item struct{ ID string } `json:"queuedSubmission"`
			}
			// Include a submission made by the original native owner before
			// the web client connected.
			if err := f.client.Call(f.ctx, "thread/queue/add", map[string]any{
				"threadId": id, "clientUserMessageId": clientID, "input": cr.TextInput(firstText),
			}, &old); err != nil || old.Item.ID == "" {
				t.Fatal("original queue item unavailable", err)
			}
			w := NewWebChat(f.ctx, f.c)
			defer w.Close()
			call := webTestCall(t, w, f.ctx)
			enqueue := func(text string) string {
				t.Helper()
				reply, err := call("web.send", map[string]any{"thread_id": id, "mode": "queue", "text": text})
				queued, _ := reply["queued_id"].(string)
				if err != nil || queued == "" {
					t.Fatal("queue registration failed", err)
				}
				return queued
			}
			const withdrawn = "QUEUE_WITHDRAWN"
			const followerText = "QUEUE_FOLLOWER"
			cancelID := enqueue(withdrawn)
			follower := enqueue(followerText)
			const edited = "대기 순서를 유지하는 수정\n  두 번째 줄"
			update := map[string]any{
				"thread_id": id, "queued_id": old.Item.ID, "expected_text": "stale text", "text": edited,
			}
			if _, err := call("web.queueUpdate", update); !errors.Is(err, errWebQueueChanged) {
				t.Fatal("stale edit was not rejected", err)
			}
			update["expected_text"] = firstText
			reply, err := call("web.queueUpdate", update)
			if err != nil || reply["updated"] != true || reply["queued_id"] != old.Item.ID {
				t.Fatal("in-place edit did not confirm the original submission", err)
			}
			cancel := map[string]any{"thread_id": adjacent, "queued_id": cancelID, "expected_text": withdrawn}
			if _, err := call("web.queueCancel", cancel); !errors.Is(err, errWebQueueGone) {
				t.Fatal("cancel accepted a queue item from another chat", err)
			}
			cancel["thread_id"] = id
			reply, err = call("web.queueCancel", cancel)
			if err != nil || reply["deleted"] != true || reply["check_history"] != (transport == "stdio") {
				t.Fatal("cancelling the original queue item was not confirmed", err)
			}
			const revised = "QUEUE_WITHDRAWN_REQUEUED_EDIT"
			readded := enqueue(revised)
			if readded == cancelID {
				t.Fatal("explicitly requeued draft should be a new submission")
			}
			queue, err := readWebQueue(f.ctx, f.client, id)
			want := []string{old.Item.ID, follower, readded}
			if err != nil || len(queue.Data) != len(want) {
				t.Fatal("native owner sees a different queue", err)
			}
			for index, raw := range queue.Data {
				var item struct{ ID, ClientUserMessageID string }
				if err := json.Unmarshal(raw, &item); err != nil || item.ID != want[index] ||
					(index == 0 && item.ClientUserMessageID != clientID) {
					t.Fatal("queue order or item identity changed", index, err)
				}
			}
			if f.requests() != 2 {
				t.Fatal("editing or cancelling launched work during the active turn")
			}
			f.once.Do(func() { close(f.release) })
			finished := f.completed(id, 4)
			if finished.Turns[0].ID != active {
				t.Fatal("active turn was replaced")
			}
			for index, expected := range []string{edited, followerText, revised} {
				raw, _ := json.Marshal(finished.Turns[index+1].Items)
				literal, _ := json.Marshal(expected)
				if !strings.Contains(string(raw), string(literal)) || strings.Contains(string(raw), firstText) {
					t.Fatal("executed an unexpected queued message", index+1)
				}
			}
			cancel["queued_id"], cancel["expected_text"] = readded, revised
			if _, err := call("web.queueCancel", cancel); !errors.Is(err, errWebQueueGone) {
				t.Fatal("already consumed item was cancelled or put back", err)
			}
			if f.requests() != 5 {
				t.Fatal("a cancelled item ran or a requeue was duplicated")
			}
			f.assertOwner(key, id, adjacent)
		})
	}
}
