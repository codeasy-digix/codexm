package nativecli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func TestOfficialNativeDefaultRestartKeepsQueueAndTranscript(t *testing.T) {
	f := newGUIControlFixtureDuration(t, "stdio", 90*time.Second, func(f *guiControlFixture) {
		setupWebFixtureLogins(t, f, "first-login", "second-login")
		if _, err := accounts.SetNativeDefault(f.c, "first-login"); err != nil {
			t.Fatal(err)
		}
	})
	closeClient := func(client *cr.Client) {
		t.Helper()
		client.Close()
		select {
		case <-client.ProcessDone:
		case <-time.After(5 * time.Second):
			t.Fatal("isolated official process did not shut down")
		}
	}
	openWithoutSQLiteEnv := func(account string) *cr.Client {
		t.Helper()
		cmd := exec.CommandContext(f.ctx, f.binary, "--no-daemon", "app-server", "--listen", "stdio://")
		cmd.Dir = f.cmd.Dir
		cmd.Env = slices.DeleteFunc(cr.Env(f.c, account, f.binary), func(entry string) bool {
			return strings.HasPrefix(entry, "CODEX_SQLITE_HOME=")
		})
		client, err := cr.StartStdio(f.ctx, cmd)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { closeClient(client) })
		return client
	}

	id := f.thread()
	const prior = "계정 전환 전 기록한 원문"
	const pending = "재실행 후에도 대기할 원문"
	turn := f.start(id, prior)
	f.completed(id, 1)
	for _, method := range []string{"thread/archive", "thread/unarchive"} {
		if err := f.client.Call(f.ctx, method, map[string]any{"threadId": id}, nil); err != nil {
			t.Fatal(err)
		}
	}
	var submitted struct {
		Item struct{ ID string } `json:"queuedSubmission"`
	}
	if err := f.client.Call(f.ctx, "thread/queue/add", map[string]any{
		"threadId": id, "clientUserMessageId": "00000000-0000-4000-8000-000000000073",
		"input": cr.TextInput(pending),
	}, &submitted); err != nil || submitted.Item.ID == "" {
		t.Fatal("could not put a message in the unloaded chat's native queue", err)
	}
	closeClient(f.client)

	// A normal official launch that receives only the named CODEX_HOME
	// opens separate indexes and cannot see this chat's pending message.
	legacy := openWithoutSQLiteEnv("second-login")
	legacyQueue, legacyErr := readWebQueue(f.ctx, legacy, id)
	if legacyErr == nil && len(legacyQueue.Data) != 0 {
		t.Fatal("unconfigured named home unexpectedly saw the shared queue")
	}
	private := filepath.Join(f.c.Runtime.AccountsDir, "second-login", "queue_1.sqlite")
	privateInfo, err := os.Stat(private)
	if err != nil {
		t.Fatal("did not reproduce an independent native queue database", err)
	}
	closeClient(legacy)

	if _, err := accounts.PinNativeStorage(f.c); err != nil {
		t.Fatal(err)
	}
	assertHistory := func(client *cr.Client) {
		t.Helper()
		page, err := readWebQueue(f.ctx, client, id)
		if err != nil || len(page.Data) != 1 {
			t.Fatal("native queue did not retain the pending message", err)
		}
		var queued struct {
			ID    string
			Input []struct{ Text string }
		}
		if err := json.Unmarshal(page.Data[0], &queued); err != nil || queued.ID != submitted.Item.ID ||
			len(queued.Input) != 1 || queued.Input[0].Text != pending {
			t.Fatal("queue ID or original text changed", err)
		}
		var history struct{ Data []WebTurn }
		if err := client.Call(f.ctx, "thread/turns/list", map[string]any{"threadId": id, "itemsView": "full"}, &history); err != nil ||
			len(history.Data) != 1 || history.Data[0].ID != turn {
			t.Fatal("same native transcript was not available", err)
		}
		items, _ := json.Marshal(history.Data[0].Items)
		original, _ := json.Marshal(prior)
		if !bytes.Contains(items, original) {
			t.Fatal("original history message lost across account change")
		}
	}
	named := openWithoutSQLiteEnv("second-login")
	assertHistory(named)
	closeClient(named)

	filename := filepath.Join(f.c.Runtime.SharedCodexHome, "config.toml")
	pinned, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.SetNativeDefault(f.c, "second-login"); err != nil {
		t.Fatal(err)
	}
	reopened := openWithoutSQLiteEnv("")
	var login struct{ Account struct{ Email string } }
	if err := reopened.Call(f.ctx, "account/read", map[string]bool{"refreshToken": false}, &login); err != nil ||
		login.Account.Email != "second-login@example.invalid" {
		t.Fatal("native restart did not load the changed default login", err)
	}
	assertHistory(reopened)
	if after, err := os.ReadFile(filename); err != nil || !bytes.Equal(after, pinned) {
		t.Fatal("setting the default rewrote the shared configuration", err)
	}
	if after, err := os.Stat(private); err != nil || !os.SameFile(after, privateInfo) {
		t.Fatal("legacy private queue did not remain in place", err)
	}
	if f.requests() != 1 {
		t.Fatal("reading the persisted queue or transcript started a new model request")
	}
}
