package nativecli

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestWebCompletionEventsOnlyCompleteRootTurns(t *testing.T) {
	now := time.Now().UTC()
	const thread = "00000000-0000-4000-8000-000000000001"
	const turn = "00000000-0000-4000-8000-000000000002"
	const child = "00000000-0000-4000-8000-000000000003"
	event := func(kind, id, root, final string, at time.Time) []byte {
		data, _ := json.Marshal(map[string]any{
			"type": "event_msg", "timestamp": at,
			"payload": map[string]string{"type": kind, "turn_id": id, "root_turn_id": root, "last_agent_message": final},
		})
		return data
	}
	var records []byte
	for _, row := range [][]byte{
		event("task_complete", turn, turn, "old", now.Add(-time.Minute)),
		event("task_complete", child, turn, "subturn", now),
		event("turn_aborted", turn, turn, "canceled", now),
		event("task_complete", turn, turn, "", now),
		event("task_complete", turn, turn, "complete", now),
	} {
		records = append(append(records, row...), '\n')
	}
	// A just-written partial line should be picked up on a subsequent read.
	records = append(records, event("task_complete", turn, turn, "incomplete line", now)...)
	got := completionEvents(records, false, thread, now.Add(-time.Second).UnixMilli())
	if len(got) != 1 || got[0] != (webCompletion{webCompletionID{thread, turn}, now.UnixMilli()}) {
		t.Fatalf("completion events: %+v", got)
	}
}

func TestWebChatOfficialCompletionReadNeedsNoWebWriter(t *testing.T) {
	f := newGUIControlFixture(t, "stdio")
	chat := f.thread()
	after := time.Now().Add(-time.Second).UnixMilli()
	turn := f.start(chat, "WEB_COMPLETION_FIXTURE")
	f.completed(chat, 1)
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); {
		found, err := w.readCompletions(f.ctx, after)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range found {
			if event.Thread == chat && event.Turn == turn {
				if len(w.connections) != 0 {
					t.Fatal("read created a web app-server")
				}
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("actual official runtime completion was not observed from disk")
}

func TestWebChatOfficialExecCompletionSurvivesArchive(t *testing.T) {
	f := newGUIControlFixture(t, "stdio")
	after := time.Now().Add(-time.Second).UnixMilli()
	cmd := exec.CommandContext(f.ctx, f.binary, "--no-daemon", "exec", "--json", "--skip-git-repo-check", "WEB_EXEC_COMPLETION_FIXTURE")
	cmd.Dir, cmd.Env = f.cmd.Dir, f.cmd.Env
	output, err := cmd.Output()
	if err != nil {
		t.Fatal("isolated Codex CLI exec failed", err)
	}
	id := ""
	for _, line := range strings.Split(string(output), "\n") {
		var row struct {
			Type string
			ID   string `json:"thread_id"`
		}
		if json.Unmarshal([]byte(line), &row) == nil && row.Type == "thread.started" {
			id = row.ID
			break
		}
	}
	if !threadID.MatchString(id) {
		t.Fatal("official CLI did not report its chat ID")
	}
	if err := f.client.Call(f.ctx, "thread/archive", map[string]any{"threadId": id}, nil); err != nil {
		t.Fatal("isolated CLI chat archive failed", err)
	}
	w := NewWebChat(f.ctx, f.c)
	defer w.Close()
	rows, err := w.readCompletions(f.ctx, after)
	if err != nil || len(rows) != 1 || rows[0].Thread != id || !threadID.MatchString(rows[0].Turn) || len(w.connections) != 0 {
		t.Fatalf("archived exec completion was not read passively: %+v (%v)", rows, err)
	}
}
