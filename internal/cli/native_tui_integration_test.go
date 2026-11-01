package cli

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
)

// Opt-in PTY exercise of the installed Rust/Go pair. All auth/history is synthetic.
// Nothing starts a model, reads a real login, or stops a user's tmux session.
func TestNativeTUIThroughOwnedTerminal(t *testing.T) {
	binary := os.Getenv("CODEXM_TUI_TEST_BINARY")
	if binary == "" {
		t.Skip("set built local Go binary with sibling codexm-tui")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	c := config.Defaults()
	c.Path = filepath.Join(root, "config.toml")
	c.Runtime.SharedCodexHome = filepath.Join(root, "shared")
	c.Runtime.AccountsDir = filepath.Join(root, "accounts")
	c.Storage.StateDir = filepath.Join(root, "state")
	c.Runtime.CodexBinary = filepath.Join(root, "codex-fixture")
	for _, dir := range []string{c.Runtime.SharedCodexHome, c.Runtime.AccountsDir, c.Storage.StateDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	fixture, err := os.ReadFile(filepath.Join("..", "..", "web", "tests", "fixtures", "codex.cjs"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(c.Runtime.CodexBinary, fixture, 0700)
	os.WriteFile(filepath.Join(root, ".fixture-only"), []byte("synthetic"), 0600)
	for _, name := range []string{"alpha", "beta"} {
		home := filepath.Join(c.Runtime.AccountsDir, name)
		os.MkdirAll(home, 0700)
		os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"auth_mode":"chatgpt","tokens":{"id_token":"fixture","access_token":"fixture-access","refresh_token":"fixture-refresh","account_id":"fixture-account"}}`), 0600)
	}
	os.WriteFile(filepath.Join(c.Runtime.SharedCodexHome, "config.toml"), []byte("model='preserved-model'\nmodel_reasoning_effort='high'\n"), 0600)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(c.Runtime.SharedCodexHome, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	id := "11111111-2222-4333-8444-555555555555"
	if _, err = db.Exec(`CREATE TABLE threads(id TEXT PRIMARY KEY,title TEXT,cwd TEXT,updated_at INTEGER,archived INTEGER);INSERT INTO threads VALUES(?,?,?,20,0);`, id, "한글 진행 작업", root); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO threads VALUES('aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee','보관 작업',?,10,1);`, root); err != nil {
		t.Fatal(err)
	}
	db.Close()
	last := filepath.Join(c.StateDir(), "native-cli", "last")
	os.MkdirAll(last, 0700)
	receipt, _ := json.Marshal(map[string]any{"thread_id": id, "account": "beta", "ready": true})
	os.WriteFile(filepath.Join(last, id+".json"), receipt, 0600)
	history, _ := os.ReadFile(filepath.Join(c.Runtime.SharedCodexHome, "state_5.sqlite"))
	settings, _ := os.ReadFile(filepath.Join(c.Runtime.SharedCodexHome, "config.toml"))
	session := fmt.Sprintf("codexm-tui-test-%d", os.Getpid())
	tmux := func(args ...string) string {
		t.Helper()
		out, e := exec.Command("tmux", args...).CombinedOutput()
		if e != nil {
			t.Fatalf("tmux %s: %v %s", args[0], e, out)
		}
		return string(out)
	}
	// Ensure startup begins with a terminal's normal modes; the fake CLI observes them after exec.
	pane := strings.TrimSpace(tmux("new-session", "-d", "-P", "-F", "#{pane_id}", "-s", session, "-x", "120", "-y", "38", "-e", "PATH="+os.Getenv("PATH"), "-e", "HOME="+root, "-e", "CODEX_HOME="+c.Runtime.SharedCodexHome, "-e", "CODEX_SQLITE_HOME="+c.Runtime.SharedCodexHome, "-e", "CODEX_ACCOUNT=default", "/bin/sleep", "60"))
	t.Cleanup(func() { exec.Command("tmux", "kill-session", "-t", "="+session).Run() })
	tmux("set-option", "-w", "-t", pane, "remain-on-exit", "on")
	terminalModes := func() string {
		t.Helper()
		tty := strings.TrimSpace(tmux("display-message", "-p", "-t", pane, "#{pane_tty}"))
		in, err := os.Open(tty)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		cmd := exec.Command("stty", "-g")
		cmd.Stdin = in
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("terminal modes: %v %s", err, out)
		}
		return string(out)
	}
	originalModes := terminalModes()
	tmux("respawn-pane", "-k", "-t", pane, binary, "--config", c.Path)
	waitText := func(text string) {
		t.Helper()
		until := time.Now().Add(12 * time.Second)
		for time.Now().Before(until) {
			out, e := exec.Command("tmux", "capture-pane", "-p", "-t", pane).CombinedOutput()
			if e == nil && strings.Contains(string(out), text) {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("screen missing %q: %s", text, tmux("capture-pane", "-p", "-t", pane))
	}
	send := func(keys ...string) {
		t.Helper()
		tmux(append([]string{"send-keys", "-t", pane}, keys...)...)
		time.Sleep(160 * time.Millisecond)
	}
	waitText("한글 진행 작업")
	pid := strings.TrimSpace(tmux("display-message", "-p", "-t", pane, "#{pane_pid}"))
	child, err := exec.Command("pgrep", "-P", pid).Output()
	if err != nil {
		t.Fatal("owned bridge missing", err)
	}
	bridgePID, err := strconv.Atoi(strings.TrimSpace(string(child)))
	if err != nil {
		t.Fatal("expected one owned bridge", err)
	}
	send("Right")
	waitText("Enter 적용 후 목록")
	send("Left")
	waitText("Enter 바로 재개")
	send("F2")
	waitText("전역 설정")
	send("Down", "Enter")
	waitText("켜짐")
	send("Up", "Enter")
	waitText("전체 계정 관리")
	send("d")
	waitText("기본 계정으로 지정")
	send("Right", "Enter")
	waitText("[기본]")
	send("x")
	waitText("계정 등록을 해제")
	send("Enter")
	waitText("전체 계정 관리")
	if _, err := os.Stat(filepath.Join(c.Runtime.AccountsDir, "alpha", "auth.json")); err != nil {
		t.Fatal("cancel removed auth")
	}
	send("a")
	waitText("계정 추가")
	send("gamma", "Enter")
	waitText("인증 방식")
	send("Enter")
	waitText("FIXTURE-CODE")
	if _, err := os.Stat(filepath.Join(c.Runtime.AccountsDir, "gamma", "auth.json")); !os.IsNotExist(err) {
		t.Fatal("pending auth was published")
	}
	os.WriteFile(filepath.Join(root, "complete-login"), []byte("synthetic"), 0600)
	waitText("전체 계정 관리")
	waitText("gamma")
	send("End") // Account navigation uses Down; End must not trigger an action.
	send("Down", "Down", "x")
	waitText("계정 등록을 해제")
	send("Right", "Enter")
	waitText("전체 계정 관리")
	if _, err := os.Stat(filepath.Join(c.Runtime.AccountsDir, "gamma", "auth.json")); err != nil {
		t.Fatal("registration removal erased auth")
	}
	send("Up", "l")
	waitText("인증 방식")
	send("Down", "Enter")
	waitText("oauth/authorize")
	send("Escape")
	waitText("전체 계정 관리")
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if _, err := os.Stat(filepath.Join(c.Runtime.AccountsDir, name, "auth.json")); err != nil {
			t.Fatal("saved auth missing", name)
		}
	}
	send("F2", "F2")
	waitText("한글 진행 작업")
	waitText("보관 작업")
	// Return to the original active row and resume with its last registered beta account.
	send("Home", "Enter")
	waitText("Selected account: beta")
	send("후속 작업 확인", "Enter")
	waitText("Received: 후속 작업 확인")
	if terminalModes() != originalModes {
		t.Fatal("terminal modes were not restored before handoff")
	}
	if syscall.Kill(bridgePID, 0) == nil {
		t.Fatal("owned bridge survived CLI handoff")
	}
	if out := tmux("display-message", "-p", "-t", pane, "#{pane_current_command}"); strings.Contains(out, "codexm") {
		t.Fatal("TUI did not hand off")
	}
	after, _ := os.ReadFile(filepath.Join(c.Runtime.SharedCodexHome, "state_5.sqlite"))
	if string(after) != string(history) {
		t.Fatal("TUI changed native history")
	}
	after, _ = os.ReadFile(filepath.Join(c.Runtime.SharedCodexHome, "config.toml"))
	if string(after) != string(settings) {
		t.Fatal("TUI changed model/effort")
	}
	if _, err := os.Stat(c.Path + ".tui.json"); err != nil {
		t.Fatal("view preference was not saved")
	}
	// The frontend and bridge have exited. Auth staging is empty; native CLI remains.
	entries, _ := os.ReadDir(c.Runtime.AccountsDir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".codexm-login-") {
			t.Fatal("owned login staging was leaked")
		}
	}
}
