package runtime

import (
	"context"
	"encoding/json"
	"github.com/codeasy-digix/codexm/internal/config"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResponsesAndServerRequestsRemainSeparate(t *testing.T) {
	c := newClient()
	c.write = func(b []byte) error {
		var q Message
		json.Unmarshal(b, &q)
		c.receive([]byte(`{"method":"item/tool/requestUserInput","id":10,"params":{"threadId":"t"}}`))
		reply, _ := json.Marshal(map[string]any{"id": q.ID, "result": map[string]string{"ok": "yes"}})
		c.receive(reply)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var r map[string]string
	if e := c.Call(ctx, "probe", map[string]any{}, &r); e != nil || r["ok"] != "yes" {
		t.Fatal(r, e)
	}
	m := <-c.Events
	if len(m.ID) == 0 || m.Method != "item/tool/requestUserInput" {
		t.Fatal("request was lost")
	}
	c.Close()
}
func TestNativeThreadAndGoalRoundTripWithoutModelCall(t *testing.T) {
	if os.Getenv("CODEXM_NATIVE_TEST") != "1" {
		t.Skip("set CODEXM_NATIVE_TEST=1 for official CLI integration")
	}
	dir := t.TempDir()
	c := config.Defaults()
	c.Path = filepath.Join(dir, "manager.toml")
	c.Storage.StateDir = filepath.Join(dir, "state")
	c.Runtime.SharedCodexHome = filepath.Join(dir, "codex")
	c.Runtime.AccountsDir = filepath.Join(dir, "accounts")
	os.MkdirAll(c.Runtime.SharedCodexHome, 0700)
	os.MkdirAll(c.Runtime.AccountsDir, 0700)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	r, e := Resolve(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	p, e := r.Probe(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	var created ResumeResponse
	e = p.Call(ctx, "thread/start", map[string]any{"cwd": dir, "sandbox": "read-only", "approvalPolicy": "never", "model": "gpt-6.1-sol"}, &created)
	if e != nil {
		p.Close()
		t.Fatal(e)
	}
	id := created.Thread.ID
	if id == "" {
		t.Fatal("thread ID missing")
	}
	if _, _, e = p.Recent(ctx, id, 1, ""); e != nil {
		t.Fatal(e)
	}
	e = p.Call(ctx, "thread/goal/set", map[string]any{"threadId": id, "objective": "isolated smoke test", "status": "paused", "origin": "user"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	g, e := p.Goal(ctx, id)
	if e != nil || g == nil || g.Status != "paused" {
		t.Fatal(g, e)
	}
	p.Close()
	time.Sleep(300 * time.Millisecond)
	p, e = r.Probe(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	loaded, e := p.Resume(ctx, id)
	if e != nil || loaded.Thread.ID != id {
		t.Fatal(loaded, e)
	}
	g, e = p.Goal(ctx, id)
	if e != nil || g == nil || g.Objective != "isolated smoke test" || g.Status != "paused" {
		t.Fatal(g, e)
	}
}
