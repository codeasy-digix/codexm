package nativecli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"golang.org/x/sys/unix"
)

// This helper is only the test executable, guarded by the private fixture root.
// It simulates official archive/unarchive; production code never edits the DB.
func TestChatManagementNativeHelper(t *testing.T) {
	root := os.Getenv("CODEXM_CHAT_TEST_ROOT")
	if root == "" {
		return
	}
	var args []string
	for i, a := range os.Args {
		if a == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	log, e := os.OpenFile(filepath.Join(root, "native-calls.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		os.Exit(8)
	}
	_ = json.NewEncoder(log).Encode(args)
	log.Close()
	if len(args) != 3 || args[0] != "--no-daemon" || (args[1] != "archive" && args[1] != "unarchive") {
		os.Exit(7)
	}
	if _, e = os.Stat(filepath.Join(root, "fail-command")); e == nil {
		fmt.Fprintln(os.Stderr, "fixture native operation failed")
		os.Exit(1)
	}
	db, e := sql.Open("sqlite", filepath.Join(os.Getenv("CODEX_SQLITE_HOME"), "state_5.sqlite"))
	if e != nil {
		os.Exit(8)
	}
	_, e = db.Exec("UPDATE threads SET archived=? WHERE id=?", args[1] == "archive", args[2])
	db.Close()
	if e != nil {
		os.Exit(8)
	}
	os.Exit(0)
}

func chatManagementFixture(t *testing.T) config.Config {
	t.Helper()
	c := fixture(t)
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("CODEXM_CHAT_TEST_ROOT", os.Getenv("HOME"))
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(exe, "'", "'\"'\"'") + "' -test.run=^TestChatManagementNativeHelper$ -- \"$@\"\n"
	if e = os.WriteFile(c.Runtime.CodexBinary, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	return c
}

func TestChatManagementKeepsIdentityAndCachesAnExplicitOperation(t *testing.T) {
	c := chatManagementFixture(t)
	ctx := context.Background()
	db, e := sql.Open("sqlite", filepath.Join(c.Runtime.SharedCodexHome, "state_5.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	_, e = db.Exec("UPDATE threads SET cwd=? WHERE id=?", filepath.Join(os.Getenv("HOME"), "missing-project"), fixtureID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec("INSERT INTO threads VALUES('aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee','untouched',?,9,0)", os.Getenv("HOME"))
	if e != nil {
		t.Fatal(e)
	}
	r := Request{Version: 1, Owner: "test-owner", Operation: "chat-archive-00000001", Method: "chats.archive", Params: json.RawMessage(`{"thread_id":"` + fixtureID + `"}`)}
	if _, e = Dispatch(ctx, c, "", r); e != nil {
		t.Fatal(e)
	}
	if _, e = Dispatch(ctx, c, "", r); e != nil {
		t.Fatal(e)
	}
	result, e := SetChatArchived(ctx, c, fixtureID, true)
	if e != nil || result.Changed {
		t.Fatal("already archived was replayed", result, e)
	}
	result, e = SetChatArchived(ctx, c, fixtureID, false)
	if e != nil || !result.Changed || result.Archived {
		t.Fatal(result, e)
	}
	log, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), "native-calls.jsonl"))
	if strings.Count(string(log), "\n") != 2 {
		t.Fatalf("mutation replayed: %s", log)
	}
	var title, cwd string
	var archived int
	if e = db.QueryRow("SELECT title,cwd,archived FROM threads WHERE id=?", fixtureID).Scan(&title, &cwd, &archived); e != nil {
		t.Fatal(e)
	}
	if title != "fixture chat" || !strings.HasSuffix(cwd, "missing-project") || archived != 0 {
		t.Fatal("chat identity or project changed")
	}
	if e = db.QueryRow("SELECT archived FROM threads WHERE id='aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee'").Scan(&archived); e != nil || archived != 0 {
		t.Fatal("another chat changed", e)
	}
}

func TestChatManagementRejectsInvalidTargetFailureAndForeignWriter(t *testing.T) {
	c := chatManagementFixture(t)
	ctx := context.Background()
	for _, id := range []string{"--all", "../invalid", "00000000-0000-4000-8000-000000000000"} {
		if _, e := SetChatArchived(ctx, c, id, true); e == nil {
			t.Fatal("unknown/invalid target accepted", id)
		}
	}
	if _, e := os.Stat(filepath.Join(os.Getenv("HOME"), "native-calls.jsonl")); !os.IsNotExist(e) {
		t.Fatal("invalid target invoked CLI")
	}
	os.WriteFile(filepath.Join(os.Getenv("HOME"), "fail-command"), []byte("fixture"), 0600)
	if _, e := SetChatArchived(ctx, c, fixtureID, true); e == nil {
		t.Fatal("failed command reported success")
	}
	if archived, e := archivedState(ctx, c, fixtureID); e != nil || archived {
		t.Fatal("failure changed state", e)
	}
	os.Remove(filepath.Join(os.Getenv("HOME"), "fail-command"))
	os.Remove(filepath.Join(os.Getenv("HOME"), "native-calls.jsonl"))
	lockDir := filepath.Join(c.Runtime.SharedCodexHome, "thread-writer-locks")
	os.MkdirAll(lockDir, 0700)
	f, e := os.OpenFile(filepath.Join(lockDir, fixtureID+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		t.Fatal(e)
	}
	if _, e = SetChatArchived(ctx, c, fixtureID, true); e == nil || !strings.Contains(e.Error(), "owning window") {
		t.Fatal("foreign writer not refused", e)
	}
	if !cr.WriterBusy(c.Runtime.SharedCodexHome, fixtureID) {
		t.Fatal("writer was released")
	}
	log, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), "native-calls.jsonl"))
	if strings.Contains(string(log), `"archive"`) || strings.Contains(string(log), `"unarchive"`) {
		t.Fatal("busy writer received a CLI mutation")
	}
}

func TestChatManagementThroughTUI(t *testing.T) {
	binary := os.Getenv("CODEXM_TUI_TEST_BINARY")
	if binary == "" {
		t.Skip("set built local Go binary with sibling codexm-tui")
	}
	c := chatManagementFixture(t)
	h := os.Getenv("HOME")
	if e := c.Save(); e != nil {
		t.Fatal(e)
	}
	session := fmt.Sprintf("codexm-chat-actions-test-%d", os.Getpid())
	ctx := context.Background()
	if e := ensureWorkspace(ctx); e != nil {
		t.Fatal(e)
	}
	pane, e := tmux(ctx, CodexTmuxServer, "new-window", "-d", "-P", "-F", "#{pane_id}", "-t", WorkspaceSession+":", "-n", session, "-e", "HOME="+h, "-e", "PATH="+os.Getenv("PATH"), "-e", "CODEXM_CHAT_TEST_ROOT="+h, "/bin/sleep", "60")
	if e != nil {
		t.Fatal(e)
	}
	pane = strings.TrimSpace(pane)
	t.Cleanup(func() { _, _ = tmux(context.Background(), CodexTmuxServer, "kill-pane", "-t", pane) })
	_, _ = tmux(ctx, CodexTmuxServer, "resize-window", "-t", pane, "-x", "120", "-y", "38")
	if _, e = tmux(ctx, CodexTmuxServer, "respawn-pane", "-k", "-t", pane, "-e", "HOME="+h, "-e", "PATH="+os.Getenv("PATH"), "-e", "CODEXM_CHAT_TEST_ROOT="+h, binary, "--config", c.Path); e != nil {
		t.Fatal(e)
	}
	waitText := func(text string) {
		t.Helper()
		until := time.Now().Add(12 * time.Second)
		for time.Now().Before(until) {
			screen, _ := tmux(ctx, CodexTmuxServer, "capture-pane", "-p", "-t", pane)
			if strings.Contains(screen, text) {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		screen, _ := tmux(ctx, CodexTmuxServer, "capture-pane", "-p", "-t", pane)
		t.Fatalf("missing %q: %s", text, screen)
	}
	send := func(keys ...string) {
		t.Helper()
		args := append([]string{"send-keys", "-t", pane}, keys...)
		if _, e = tmux(ctx, CodexTmuxServer, args...); e != nil {
			t.Fatal(e)
		}
		time.Sleep(150 * time.Millisecond)
	}
	waitText("fixture chat")
	send("Right")
	waitText("Enter 적용 후 목록")
	send("Right")
	waitText("채팅 관리")
	send("Enter")
	waitText("보관 확인")
	send("Enter")
	waitText("채팅 관리")
	if state, _ := archivedState(ctx, c, fixtureID); state {
		t.Fatal("default confirmation archived chat")
	}
	send("Enter", "Right", "Enter")
	waitText("채팅을 보관했습니다")
	if state, e := archivedState(ctx, c, fixtureID); e != nil || !state {
		t.Fatal("selected row was not archived", e)
	}
	send("a")
	waitText("[보관]")
	send("Right", "Right")
	waitText("일반 목록으로 복원")
	send("Enter")
	waitText("보관 해제 확인")
	send("Right", "Enter")
	waitText("보관을 해제했습니다")
	if state, e := archivedState(ctx, c, fixtureID); e != nil || state {
		t.Fatal("selected row was not restored", e)
	}
	os.WriteFile(filepath.Join(h, "fail-command"), []byte("fixture"), 0600)
	send("Right", "Right", "Enter", "Right", "Enter")
	waitText("official archive did not complete")
	waitText("채팅 관리")
	if state, _ := archivedState(ctx, c, fixtureID); state {
		t.Fatal("failed native archive changed state")
	}
	send("Left", "Left")
	waitText("Enter 연결/재개")
	send("q")
}
