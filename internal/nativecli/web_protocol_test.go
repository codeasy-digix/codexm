package nativecli

import (
	"encoding/json"
	"os/exec"
	"testing"
	"time"

	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func TestWebCoreOfficialProtocol(t *testing.T) {
	f := newGUIControlFixture(t, "stdio")
	var project struct{ Project struct{ ID string } }
	err := f.client.Call(f.ctx, "project/create", map[string]any{"idempotencyKey": "00000000-0000-4000-8000-000000000041", "name": "Fixture project", "roots": []map[string]string{{"path": f.cmd.Dir}}}, &project)
	if err != nil {
		t.Fatal("project/create", err)
	}
	var created struct{ Thread cr.Thread }
	if err := f.client.Call(f.ctx, "thread/start", map[string]any{"cwd": f.cmd.Dir, "projectId": project.Project.ID, "historyMode": "paginated", "approvalPolicy": "never", "sandbox": "read-only"}, &created); err != nil {
		t.Fatal(err)
	}
	id := created.Thread.ID
	f.start(id, "WEB_FIXTURE_FIRST")
	f.completed(id, 1)
	var raw json.RawMessage
	var second struct{ Project struct{ ID string } }
	if err := f.client.Call(f.ctx, "project/create", map[string]any{"idempotencyKey": "00000000-0000-4000-8000-000000000044", "name": "Moved project", "roots": []map[string]string{{"path": f.cmd.Dir}}}, &second); err != nil {
		t.Fatal(err)
	}
	if err := f.client.Call(f.ctx, "thread/metadata/update", map[string]any{"threadId": id, "projectId": second.Project.ID}, &raw); err != nil {
		t.Fatal("project move", err)
	}
	if err := f.client.Call(f.ctx, "thread/section/move", map[string]any{"threadId": id, "sectionId": "pinned"}, &raw); err == nil {
		t.Fatal("expected no built-in pinned section in this isolated store")
	}
	t.Log("native project assignment works; pinned needs web-owned presentation state")
	turn := f.start(id, "GUI_FIXTURE_ACTIVE")
	select {
	case <-f.active:
	case <-time.After(5 * time.Second):
		t.Fatal("fixture did not become active")
	}
	if err := f.client.Call(f.ctx, "turn/steer", map[string]any{"threadId": id, "expectedTurnId": turn, "input": cr.TextInput("WEB_FIXTURE_STEER"), "clientUserMessageId": "00000000-0000-4000-8000-000000000042"}, &raw); err != nil {
		t.Fatal("turn/steer", err)
	}
	cmd := exec.CommandContext(f.ctx, f.binary, "--no-daemon", "--disable", "plugins", "--disable", "apps", "--disable", "hooks", "--enable", "skip_host_skill_discovery", "app-server", "--listen", "stdio://")
	cmd.Env, cmd.Dir = environment(f.c, "default"), f.cmd.Dir
	peer, err := cr.StartStdio(f.ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { peer.Close(); <-peer.ProcessDone }()
	err = peer.Call(f.ctx, "turn/steer", map[string]any{"threadId": id, "expectedTurnId": turn, "input": cr.TextInput("WEB_FOREIGN_STEER")}, &raw)
	t.Log("foreign-owner steer result", err)
	if err := f.client.Call(f.ctx, "thread/queue/add", map[string]any{"threadId": id, "input": cr.TextInput("WEB_FIXTURE_QUEUED"), "clientUserMessageId": "00000000-0000-4000-8000-000000000043"}, &raw); err != nil {
		t.Fatal("thread/queue/add", err)
	}
	var queue struct{ Data []json.RawMessage }
	if err := f.client.Call(f.ctx, "thread/queue/list", map[string]any{"threadId": id}, &queue); err != nil || len(queue.Data) != 1 {
		t.Fatal("queue read", err, len(queue.Data))
	}
	if err := f.client.Call(f.ctx, "turn/interrupt", map[string]any{"threadId": id, "turnId": turn}, nil); err != nil {
		t.Fatal(err)
	}
	t.Log("native steer and durable queue accepted")
}

func TestWebArchivedOfficialReadProtocol(t *testing.T) {
	f := newGUIControlFixture(t, "stdio")
	id := f.thread()
	turn := f.start(id, "ARCHIVED_PASSIVE_READ")
	f.completed(id, 1)
	if err := f.client.Call(f.ctx, "thread/archive", map[string]any{"threadId": id}, nil); err != nil {
		t.Fatal(err)
	}
	for _, probe := range []struct {
		method string
		params any
	}{
		{"thread/read", map[string]any{"threadId": id, "includeTurns": false}},
		{"thread/turns/list", map[string]any{"threadId": id, "itemsView": "summary", "limit": 8}},
		{"thread/items/list", map[string]any{"threadId": id, "turnId": turn, "limit": 30}},
		{"thread/queue/list", map[string]any{"threadId": id, "limit": 50}},
	} {
		var raw json.RawMessage
		err := f.client.Call(f.ctx, probe.method, probe.params, &raw)
		t.Log(probe.method, "archived read result", err, "bytes", len(raw))
	}
}
