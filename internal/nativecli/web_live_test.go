package nativecli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func TestWebLivePreviewTracksOnlyTheCurrentAgentMessage(t *testing.T) {
	id := "10000000-0000-4000-8000-000000000001"
	c := &webConnection{}
	event := func(method string, params map[string]any) {
		params["threadId"] = id
		data, _ := json.Marshal(params)
		c.observeLiveEvent(cr.Message{Method: method, Params: data})
	}
	event("turn/started", map[string]any{"turn": map[string]string{"id": "turn-a"}})
	if c.live[id].turn.Items == nil {
		t.Fatal("starting a turn must produce an empty JSON item array")
	}
	event("item/started", map[string]any{"turnId": "turn-a", "item": map[string]string{"id": "item-a", "type": "agentMessage"}})
	event("item/agentMessage/delta", map[string]any{"turnId": "turn-a", "itemId": "item-a", "delta": "First fragment"})
	event("item/agentMessage/delta", map[string]any{"turnId": "old-turn", "itemId": "item-a", "delta": "stale"})
	entry := c.live[id]
	if len(entry.turn.Items) != 1 || entry.turn.Items[0].Text != "First fragment" || !entry.turn.Items[0].Streaming {
		t.Fatal(entry.turn)
	}
	event("thread/status/changed", map[string]any{"status": map[string]any{"type": "active", "activeFlags": []string{"waitingOnApproval"}}})
	if entry.activity.State != "waiting" {
		t.Fatal(entry.activity)
	}
	event("item/completed", map[string]any{"turnId": "turn-a", "item": map[string]string{"id": "item-a", "type": "agentMessage", "text": "Final text"}})
	event("turn/completed", map[string]any{"turn": map[string]string{"id": "turn-a", "status": "completed"}})
	if entry.turn.Items[0].Text != "Final text" || entry.turn.Items[0].Streaming || entry.activity.State != "idle" {
		t.Fatal(entry)
	}
}

func TestWebRolloutActivityRequiresCurrentRootTurnBoundary(t *testing.T) {
	holder := time.Date(2026, 11, 18, 1, 0, 0, 0, time.UTC)
	open := `{"timestamp":"2026-11-18T01:01:00Z","type":"event_msg","payload":{"type":"task_started","turn_id":"root","root_turn_id":"root"}}` + "\n"
	nested := `{"type":"event_msg","payload":{"type":"task_complete","turn_id":"subturn","root_turn_id":"root"}}` + "\n"
	closed := `{"type":"event_msg","payload":{"type":"task_complete","turn_id":"root","root_turn_id":"root"}}`
	for _, tc := range []struct {
		log, state string
		started    time.Time
	}{
		{open + nested + closed, "running", holder}, // Partial write is not a completed record.
		{open + nested + closed + "\n", "idle", holder},
		{open, "unknown", holder.Add(2 * time.Minute)},
	} {
		got, ok := lastWebRolloutActivity([]byte(tc.log), false, tc.started, webActivity{})
		if !ok || got.State != tc.state || got.TurnID != "root" {
			t.Fatalf("activity %+v, want %q", got, tc.state)
		}
	}
}

func TestWebRolloutTracksTurnStartedByAnotherChat(t *testing.T) {
	holder := time.Date(2026, 11, 22, 22, 0, 0, 0, time.UTC)
	open := `{"timestamp":"2026-11-22T22:19:57.471Z","type":"event_msg","payload":{"type":"task_started","turn_id":"target","root_turn_id":"sender","turn_attribution":{"turn_trigger":"app_tool_send_message","turn_id":"target"}}}` + "\n"
	closed := `{"type":"event_msg","payload":{"type":"task_complete","turn_id":"target","root_turn_id":"sender"}}` + "\n"
	nested := `{"type":"event_msg","payload":{"type":"task_complete","turn_id":"nested","root_turn_id":"sender"}}` + "\n"
	running := webActivity{State: "running", Source: "external", TurnID: "target"}
	idle := webActivity{State: "idle", Source: "external", TurnID: "target"}
	for _, tc := range []struct {
		name, log   string
		prior, want webActivity
	}{
		{"running", open + nested, webActivity{}, running},
		{"full scan after completion", open + nested + closed, webActivity{}, idle},
		{"incremental scan after completion", nested + closed, running, idle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := lastWebRolloutActivity([]byte(tc.log), false, holder, tc.prior)
			if !ok || got != tc.want {
				t.Fatalf("activity %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestWebInputFindsGUIBoundaryBehindLargeToolOutput(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	id := "10000000-0000-4000-8000-000000000002"
	previous := webWriter{pid: 19, started: time.Unix(1, 0), path: "rollout-" + id + ".jsonl"}
	boundary := `{"timestamp":"2026-11-18T01:01:00Z","type":"event_msg","payload":{"type":"task_started","turn_id":"long-turn"}}`
	output := `{"type":"response_item","payload":{"output":"` + strings.Repeat("x", maxWebRolloutTail) + `"}}`
	if err := os.WriteFile(filepath.Join(dir, previous.path), []byte(boundary+"\n"+output+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	observed := previous
	readWebWriter(t.Context(), root, dir, id, &observed, previous, maxWebRolloutTail)
	if observed.activity.TurnID != "" {
		t.Fatal("a sidebar poll exceeded the regular tail budget")
	}
	readWebWriter(t.Context(), root, dir, id, &observed, previous, 64<<20)
	if observed.activity.State != "running" || observed.activity.TurnID != "long-turn" {
		t.Fatalf("input routing missed the active GUI turn: %+v", observed.activity)
	}
	poll := previous
	readWebWriter(t.Context(), root, dir, id, &poll, observed, maxWebRolloutTail)
	if poll.activity != observed.activity {
		t.Fatal("the activity badge lost the verified turn during a regular poll")
	}
}
