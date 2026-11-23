package nativecli

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func TestWebGUIQuitRequiresFreshConfirmationForTheSameApp(t *testing.T) {
	ctx := context.Background()
	var action webGUIQuit
	desktop := webDesktop{main: desktopProcess{pid: 1234, key: "first-instance"}, app: "/fixture/Codex.app"}
	find := func(context.Context) (webDesktop, error) { return desktop, nil }
	stopped := 0
	stop := func(context.Context, webDesktop) error { stopped++; return nil }
	preview := func() string {
		t.Helper()
		result, err := action.preview(ctx, "browser", find)
		if err != nil {
			t.Fatal(err)
		}
		return result.(map[string]string)["confirmation"]
	}
	for _, fixture := range []struct {
		name, owner, token string
	}{
		{"no preview", "browser", "forged"},
		{"other browser", "another-browser", preview()},
	} {
		if err := action.quit(ctx, fixture.owner, fixture.token, find, stop); err == nil {
			t.Fatalf("%s authorized a shutdown", fixture.name)
		}
	}
	token := preview()
	ticket := action.tickets["browser"]
	ticket.expires = time.Now().Add(-time.Second)
	action.tickets["browser"] = ticket
	if err := action.quit(ctx, "browser", token, find, stop); err == nil {
		t.Fatal("expired confirmation authorized a shutdown")
	}
	token = preview()
	desktop.main.key = "replacement-instance"
	if err := action.quit(ctx, "browser", token, find, stop); err == nil || stopped != 0 {
		t.Fatal("replacement app received a shutdown", err)
	}
	token = preview()
	if err := action.quit(ctx, "browser", token, find, stop); err != nil || stopped != 1 {
		t.Fatal("confirmed app did not receive one shutdown", err)
	}
	if err := action.quit(ctx, "browser", token, find, stop); err == nil || stopped != 1 {
		t.Fatal("confirmation was used again", err)
	}
}

func TestWebChatOfficialQuitAndResumeWithAnotherAccount(t *testing.T) {
	// The registered PID below belongs to this test's official app-server
	// child. The production desktop finder and the real Mac GUI are untouched.
	gui := newGUIControlFixture(t, "stdio")
	other := newGUIControlFixture(t, "stdio")
	setupWebFixtureLogins(t, gui, "primary", "secondary")
	if _, err := accounts.SetNativeDefault(gui.c, "primary"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEXM_GUI_FIXTURE_KEY", "fixture-web-continue")
	t.Setenv("CODEXM_WEB_ONLY", "true")
	w := NewWebChat(gui.ctx, gui.c)
	defer w.Close()
	call := webTestCall(t, w, gui.ctx)
	catalog, err := call("web.catalog", map[string]any{})
	if err != nil || catalog["defaultAccount"] != "primary" {
		t.Fatal("default account label did not report the saved identity", err)
	}
	if catalog["guiQuitAvailable"] != (runtime.GOOS == "darwin") {
		t.Fatal("web-only mode did not report its platform's GUI quit capability")
	}
	if _, err := call("web.guiQuit", map[string]any{"confirmation": "no-preview"}); err == nil || !strings.Contains(err.Error(), "종료 확인이 만료") {
		t.Fatal("web-only quit must still require a fresh confirmation", err)
	}

	id := gui.thread()
	gui.start(id, "GUI_FIXTURE_ACTIVE_QUIT")
	select {
	case <-gui.active:
	case <-gui.ctx.Done():
		t.Fatal("test app-server did not start a turn")
	}
	otherID := other.thread()
	other.start(otherID, "GUI_FIXTURE_ACTIVE_UNRELATED")
	select {
	case <-other.active:
	case <-other.ctx.Done():
		t.Fatal("unrelated test server did not start a turn")
	}
	otherKey, err := cr.NativeProcessIdentity(other.cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	pid := gui.cmd.Process.Pid
	key, err := cr.NativeProcessIdentity(pid)
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"thread_id": id, "account": "secondary", "mode": "immediate", "text": "WEB_CONTINUED_UNDER_SECONDARY"}
	if _, err := call("web.send", params); err == nil {
		t.Fatal("the external writer did not block starting a new web turn")
	}

	find := func(context.Context) (webDesktop, error) {
		return webDesktop{main: desktopProcess{pid: pid, key: key}, app: "/fixture/Codex.app"}, nil
	}
	confirmation, err := w.guiQuit.preview(gui.ctx, "fixture-browser", find)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.guiQuit.quit(gui.ctx, "fixture-browser", confirmation.(map[string]string)["confirmation"], find, stopWebDesktop); err != nil {
		t.Fatal(err)
	}
	if cr.WriterBusy(gui.c.Runtime.SharedCodexHome, id) {
		t.Fatal("test GUI writer did not release the original chat")
	}
	if current, err := cr.NativeProcessIdentity(other.cmd.Process.Pid); err != nil || current != otherKey ||
		!cr.WriterBusy(other.c.Runtime.SharedCodexHome, otherID) {
		t.Fatal("unrelated app-server was disturbed", err)
	}
	result, err := call("web.send", params)
	if err != nil || result["thread_id"] != id || result["mode"] != "start" {
		t.Fatal("web did not continue the same stored chat", result, err)
	}
	history, err := call("web.history", map[string]any{"thread_id": id, "account": "secondary"})
	if err != nil || history["thread"].(map[string]any)["externalOwner"] == true {
		t.Fatal("the selected web account does not own the chat", err)
	}
}
