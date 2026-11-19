package nativecli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

// Serve real app-server RPC over a private Unix socket with a local Responses
// and rate-limit backend. No personal account or runtime store is used.
func TestWebQuotaBrowserFixture(t *testing.T) {
	output := os.Getenv("CODEXM_WEB_BROWSER_FIXTURE")
	if !filepath.IsAbs(output) {
		t.Skip("requires a private browser fixture directory")
	}
	window := os.Getenv("CODEXM_WEB_QUOTA_WINDOW")
	if window == "" {
		window = "5h"
	}
	if window != "5h" && window != "week" {
		t.Fatal("quota window must be 5h or week")
	}
	transport := os.Getenv("CODEXM_WEB_QUOTA_OWNER")
	if transport == "" {
		transport = "unix"
	}
	if transport != "unix" && transport != "stdio" {
		t.Fatal("quota owner transport must be unix or stdio")
	}
	const exhausted, healthy = "fixture-exhausted", "fixture-healthy"
	f := newGUIControlFixtureDuration(t, transport, 45*time.Minute, func(f *guiControlFixture) {
		f.quotaAccount, f.quotaWindow = exhausted, window
		f.quotaFailure = make(chan struct{})
		close(f.quotaFailure)
		setupWebFixtureLogins(t, f, exhausted, healthy)
		if _, err := accounts.SetNativeDefault(f.c, exhausted); err != nil {
			t.Fatal(err)
		}
		filename := filepath.Join(f.c.Runtime.SharedCodexHome, "config.toml")
		settings, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(strings.ReplaceAll(string(settings), "env_key='CODEXM_GUI_FIXTURE_KEY'\n", "")), 0600); err != nil {
			t.Fatal(err)
		}
	})
	name := func(text string) string {
		t.Helper()
		id := f.thread()
		if err := f.client.Call(f.ctx, "thread/name/set", map[string]any{"threadId": id, "name": text}, nil); err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := name("한도 0% 재현")
	f.start(id, "WEB_FIXTURE_QUOTA_429")
	var failed bool
	for until := time.Now().Add(12 * time.Second); time.Now().Before(until); time.Sleep(100 * time.Millisecond) {
		thread, _, err := f.client.Recent(f.ctx, id, 1, "")
		if err == nil && len(thread.Turns) == 1 && thread.Turns[0].Status == "failed" && thread.Turns[0].Error != nil &&
			strings.Contains(string(thread.Turns[0].Error.CodexErrorInfo), "usageLimitExceeded") {
			failed = true
			break
		}
	}
	if !failed {
		t.Fatal("the real Codex runtime did not classify the synthetic 429 as a usage limit")
	}
	neighbor := name("다른 실행 보존 검증")
	neighborTurn := f.start(neighbor, "GUI_FIXTURE_ACTIVE_QUOTA_NEIGHBOR")
	if !cr.WriterBusy(f.c.Runtime.SharedCodexHome, id) || !cr.WriterBusy(f.c.Runtime.SharedCodexHome, neighbor) {
		t.Fatal("the fixture must retain both original writer locks")
	}
	f.c.Path = filepath.Join(f.cmd.Dir, "web-quota.toml")
	if err := f.c.Save(); err != nil {
		t.Fatal(err)
	}
	write := func(name string, value any) {
		t.Helper()
		b, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := config.AtomicWrite(filepath.Join(output, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("fixture.json", map[string]any{"home": f.cmd.Dir, "config": f.c.Path, "state": filepath.Join(f.c.StateDir(), "web"),
		"thread": id, "target": healthy, "window": window, "ownerTransport": transport})
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-f.ctx.Done():
			t.Fatal(f.ctx.Err())
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(output, "stop-fixture")); err == nil {
				return
			}
			if _, err := os.Stat(filepath.Join(output, "verify-quota-handoff")); err != nil {
				continue
			}
			loaded, err := webLoadedThreads(f.ctx, f.client)
			if err != nil {
				t.Fatal(err)
			}
			_, stillOwned := loaded[id]
			other, _, err := f.client.Recent(f.ctx, neighbor, 1, "")
			neighborActive := err == nil && len(other.Turns) == 1 && other.Turns[0].ID == neighborTurn &&
				other.Turns[0].Status == "inProgress" && cr.WriterBusy(f.c.Runtime.SharedCodexHome, neighbor)
			requests := 0
			f.mu.Lock()
			for i, body := range f.bodies {
				if strings.Contains(body, "QUOTA_FIXTURE_HEALTHY_FOLLOWUP") && fixtureLoginName(f.keys[i]) == healthy {
					requests++
				}
			}
			f.mu.Unlock()
			write("quota-handoff.json", map[string]any{"oldOwnerKeepsThread": stillOwned, "neighborContinues": neighborActive,
				"healthyRequests": requests, "nativeUsageLimit": failed})
		}
	}
}
