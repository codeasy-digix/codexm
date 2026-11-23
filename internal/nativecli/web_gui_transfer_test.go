package nativecli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func TestWebGUIHandoffRetainsQueueAndExistingPause(t *testing.T) {
	c := config.Defaults()
	c.Storage.StateDir = t.TempDir()
	w := NewWebChat(t.Context(), c)
	defer w.Close()
	const thread = "00000000-0000-4000-8000-000000000002"
	const first, second = "handoff-first-attempt", "handoff-second-attempt"
	q := []webQueuedInput{
		{ID: "web-first", Text: "First pending", Identity: "gui-token", State: "pending", GUIWriter: &processIdentity{PID: 11}},
		{ID: "web-second", Text: "Second pending", Identity: "web-login", State: "pending"},
	}
	if err := w.saveOwnedQueue(thread, q); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(w.queuePath(thread))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.pauseGUITransferQueue(thread, first); err != nil {
		t.Fatal(err)
	}
	if err := w.pauseGUITransferQueue(thread, second); err != nil {
		t.Fatal(err)
	}
	if err := w.finishGUITransferQueue(thread, second, nil); err != nil {
		t.Fatal(err)
	}
	if err := w.finishGUITransferQueue(thread, first, func(input webQueuedInput) bool { return input.Identity == "web-login" }); err != nil {
		t.Fatal(err)
	}
	if pause, err := w.readQueuePause(thread); err != nil || pause != guiHandoffPause(first) {
		t.Fatalf("mismatched login must keep the queue paused: %q %v", pause, err)
	}
	if err := w.finishGUITransferQueue(thread, second, func(webQueuedInput) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if pause, err := w.readQueuePause(thread); err != nil || pause != "" {
		t.Fatalf("compatible destination should clear its temporary pause: %q %v", pause, err)
	}
	if after, err := os.ReadFile(w.queuePath(thread)); err != nil || !bytes.Equal(before, after) {
		t.Fatal("handoff changed the queue content or order", err)
	}
	const manual = "manual-pause"
	if err := os.WriteFile(w.queuePausePath(thread), []byte(manual), 0600); err != nil {
		t.Fatal(err)
	}
	if err := w.finishGUITransferQueue(thread, second, func(webQueuedInput) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if pause, err := w.readQueuePause(thread); err != nil || pause != manual {
		t.Fatal("cancelled a user's pause", pause, err)
	}
}

func TestWebGUIHandoffOfficialReleaseStartsSameChatWithQueueIntact(t *testing.T) {
	f := webOnlyFixture(t)
	id, relay := f.thread(), f.thread()
	f.start(relay, "RELAY_READY_FOR_HANDOFF")
	f.completed(relay, 1)
	turn := f.start(id, "GUI_FIXTURE_ACTIVE_FOREIGN_BROWSER")
	select {
	case <-f.active:
	case <-f.ctx.Done():
		t.Fatal("source did not start the target chat")
	}
	f.c.Path = filepath.Join(f.cmd.Dir, "fixture.toml")
	f.c.Runtime.WebGUIRelay = relay
	w := webWithGUIFixture(t, f)
	defer w.Close()
	original, err := inspectProcess(f.ctx, f.cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	q := []webQueuedInput{{ID: "web-fixture", Text: "Keep original queued GUI input", Account: "alpha",
		State: "pending", Identity: "original-login", GUIWriter: &original}}
	if err := w.saveOwnedQueue(id, q); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(w.queuePath(id))
	if err != nil {
		t.Fatal(err)
	}
	call := webTestCall(t, w, f.ctx)
	if _, err := call("web.send", map[string]any{"thread_id": id, "account": "beta", "mode": "immediate", "gui_handoff": true,
		"text": "stale input", "turn_id": "not-the-active-turn"}); err == nil {
		t.Fatal("a stale browser interfered with the running source")
	}
	const message = "GUI_TO_WEB_EXPLICIT_HANDOFF"
	result, err := call("web.send", map[string]any{"thread_id": id, "account": "beta", "mode": "immediate", "gui_handoff": true,
		"text": message, "turn_id": turn})
	request, _ := result["relay_id"].(string)
	if err != nil || !webRelayID.MatchString(request) || result["handoff"] != "web" {
		t.Fatal("no GUI-side release requested", err)
	}
	if err := w.advanceOwnedQueue(f.ctx, id); err != nil {
		t.Fatal(err)
	}
	var payload bytes.Buffer
	if err := claimGUIRelay(f.ctx, f.c, request, verifyGUIRelayClaim, &payload); err != nil {
		t.Fatal(err)
	}
	if err := f.client.Call(f.ctx, "thread/archive", map[string]any{"threadId": id}, nil); err != nil {
		t.Fatal(err)
	}
	if err := cr.WaitWriterRelease(f.ctx, f.c.Runtime.SharedCodexHome, id); err != nil {
		t.Fatal(err)
	}
	if err := completeGUIRelay(f.c, relay, request, "accepted"); err != nil {
		t.Fatal(err)
	}
	assertFixtureModelLogin(t, f, message, "beta")
	chosen, err := w.connection(f.ctx, "beta")
	if err != nil {
		t.Fatal(err)
	}
	defer w.releaseConnection(chosen)
	if err := w.verifyOwner(f.ctx, chosen, id, true); err != nil {
		t.Fatal(err)
	}
	waitWebNativeTurn(t, f, chosen, id, turn, "interrupted")
	wait, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	for w.guiTransferActive(id) && wait.Err() == nil {
		time.Sleep(30 * time.Millisecond)
	}
	if receipt, err := readGUIRelay(f.c, request); err != nil || receipt.State != "accepted" {
		t.Fatal("destination send wasn't confirmed", receipt.State, err)
	}
	if after, err := os.ReadFile(w.queuePath(id)); err != nil || !bytes.Equal(before, after) {
		t.Fatal("handoff changed an earlier queued message", err)
	}
	if queue, err := readWebQueue(f.ctx, chosen.client, id); err != nil || len(queue.Data) != 0 {
		t.Fatal("handoff sent its prompt into the target queue", err)
	}
}

func TestWebGUIArchiveClaimCarriesOnlyTheExpectedChatAndTurn(t *testing.T) {
	c := config.Defaults()
	c.Storage.StateDir = t.TempDir()
	c.Runtime.WebGUIRelay = "00000000-0000-4000-8000-000000000001"
	const request = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const thread = "00000000-0000-4000-8000-000000000002"
	receipt := webGUIRelayReceipt{Thread: thread, Relay: c.Runtime.WebGUIRelay, ObservedTurn: "expected", Archive: true,
		State: "pending", ExpiresAt: time.Now().Add(time.Minute)}
	if err := saveGUIRelay(c, request, receipt); err != nil {
		t.Fatal(err)
	}
	var claim bytes.Buffer
	check := func(_ context.Context, _ config.Config, saved webGUIRelayReceipt) error {
		if !saved.Archive || saved.Thread != thread || saved.ObservedTurn != "expected" {
			t.Fatal("archive claim lost its scope")
		}
		return nil
	}
	if err := claimGUIRelay(t.Context(), c, request, check, &claim); err != nil {
		t.Fatal(err)
	}
	if got, want := claim.String(), `{"expected_turn":"expected","thread_id":"`+thread+`"}`+"\n"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if err := completeGUIRelay(c, receipt.Relay, request, "accepted"); err != nil {
		t.Fatal(err)
	}
	if result, err := waitGUITransfer(t.Context(), c, request); err != nil || result.State != "released" || len(webRelayedInputs(c, thread)) != 0 {
		t.Fatal("source release must not appear as an accepted user message", result.State, err)
	}
}

func TestWebGUIAcquireKeepsInputPendingUntilWriterVerification(t *testing.T) {
	c := config.Defaults()
	c.Storage.StateDir = t.TempDir()
	c.Runtime.WebGUIRelay = "00000000-0000-4000-8000-000000000001"
	const request = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const thread = "00000000-0000-4000-8000-000000000002"
	receipt := webGUIRelayReceipt{Thread: thread, Relay: c.Runtime.WebGUIRelay, Acquire: true, Text: "transfer input",
		State: "pending", ExpiresAt: time.Now().Add(time.Minute)}
	if err := saveGUIRelay(c, request, receipt); err != nil {
		t.Fatal(err)
	}
	var payload bytes.Buffer
	if err := claimGUIRelay(t.Context(), c, request, func(context.Context, config.Config, webGUIRelayReceipt) error { return nil }, &payload); err != nil {
		t.Fatal(err)
	}
	if err := completeGUIRelay(c, receipt.Relay, request, "accepted"); err != nil {
		t.Fatal(err)
	}
	state, err := readGUIRelay(c, request)
	if err != nil || state.State != "acquired" || !guiRelayPending(state.State) || len(webRelayedInputs(c, thread)) != 0 {
		t.Fatal("tool accepted the input but writer verification did not finish", state.State, err)
	}
}
