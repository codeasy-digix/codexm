package cli

import (
	"context"
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
	"github.com/codeasy-digix/codexm/internal/nativecli"
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
	secondID := "99999999-8888-4777-8666-555555555555"
	if _, err = db.Exec(`INSERT INTO threads VALUES(?,?,?,15,0);`, secondID, "다른 진행 작업", root); err != nil {
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
		out, e := exec.Command("tmux", append([]string{"-L", "default"}, args...)...).CombinedOutput()
		if e != nil {
			t.Fatalf("tmux %s: %v %s", args[0], e, out)
		}
		return string(out)
	}
	// Ensure startup begins with a terminal's normal modes; the fake CLI observes them after exec.
	if exec.Command("tmux", "-L", "default", "has-session", "-t", "=codexm").Run() != nil {
		tmux("new-session", "-d", "-s", "codexm", "-n", "home")
	}
	pane := strings.TrimSpace(tmux("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "codexm:", "-n", session, "-e", "PATH="+os.Getenv("PATH"), "-e", "HOME="+root, "-e", "CODEX_HOME="+c.Runtime.SharedCodexHome, "-e", "CODEX_SQLITE_HOME="+c.Runtime.SharedCodexHome, "-e", "CODEX_ACCOUNT=default", "/bin/sleep", "60"))
	pickerPane := pane
	t.Cleanup(func() {
		exec.Command("tmux", "-L", "default", "kill-pane", "-t", pickerPane).Run()
		for _, launch := range nativecli.Sessions(c) {
			_ = nativecli.Close(context.Background(), c, launch.Session)
		}
	})
	tmux("resize-window", "-t", pane, "-x", "120", "-y", "38")
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
	tmux("respawn-pane", "-k", "-t", pane, "-e", "PATH="+os.Getenv("PATH"), "-e", "HOME="+root, "-e", "CODEX_HOME="+c.Runtime.SharedCodexHome, "-e", "CODEX_SQLITE_HOME="+c.Runtime.SharedCodexHome, "-e", "CODEX_ACCOUNT=default", binary, "--config", c.Path)
	waitText := func(text string) {
		t.Helper()
		until := time.Now().Add(12 * time.Second)
		for time.Now().Before(until) {
			out, e := exec.Command("tmux", "-L", "default", "capture-pane", "-p", "-t", pane).CombinedOutput()
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
	waitText(id)
	send("k")
	waitText(secondID)
	send("i")
	waitText(id)
	send("l")
	waitText("Enter 적용 후 목록")
	send("l")
	waitText("↑↓/ik 메뉴")
	send("j")
	waitText("Enter 적용 후 목록")
	send("j")
	waitText("Enter 연결/재개")
	send("/", "ijkl")
	waitText("/ijkl_")
	waitText("검색 입력")
	send("Enter", "Escape")
	waitText(id)
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
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		for _, launch := range nativecli.Sessions(c) {
			if launch.Thread == id && launch.Ready {
				pane = launch.Pane
			}
		}
		if pane != pickerPane {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pane == pickerPane {
		t.Fatal("TUI did not create a persistent CLI window")
	}
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
	// Closing a terminal while Crossterm is waiting must not leave the UI or
	// bridge spinning in the background. Only these synthetic children are used.
	pane = pickerPane
	for _, closePTY := range []bool{false, true} {
		tmux("respawn-pane", "-k", "-t", pane, "-e", "PATH="+os.Getenv("PATH"), "-e", "HOME="+root, "-e", "CODEX_HOME="+c.Runtime.SharedCodexHome, "-e", "CODEX_SQLITE_HOME="+c.Runtime.SharedCodexHome, "-e", "CODEX_ACCOUNT=default", binary, "--config", c.Path)
		waitText("한글 진행 작업")
		uiPIDText := strings.TrimSpace(tmux("display-message", "-p", "-t", pane, "#{pane_pid}"))
		uiPID, err := strconv.Atoi(uiPIDText)
		if err != nil {
			t.Fatal(err)
		}
		owned, err := exec.Command("pgrep", "-P", uiPIDText).Output()
		if err != nil {
			t.Fatal(err)
		}
		ownedPID, err := strconv.Atoi(strings.TrimSpace(string(owned)))
		if err != nil {
			t.Fatal(err)
		}
		cleanupNeeded := true
		t.Cleanup(func() {
			if !cleanupNeeded {
				return
			}
			for _, owned := range []int{uiPID, ownedPID} {
				args, err := exec.Command("ps", "-p", strconv.Itoa(owned), "-o", "args=").Output()
				if err == nil && strings.Contains(string(args), c.Path) {
					_ = syscall.Kill(owned, syscall.SIGKILL)
				}
			}
		})
		if closePTY {
			tmux("kill-pane", "-t", pane)
		} else if err := syscall.Kill(uiPID, syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		until := time.Now().Add(5 * time.Second)
		for time.Now().Before(until) && (syscall.Kill(uiPID, 0) == nil || syscall.Kill(ownedPID, 0) == nil) {
			time.Sleep(50 * time.Millisecond)
		}
		if syscall.Kill(uiPID, 0) == nil || syscall.Kill(ownedPID, 0) == nil {
			t.Fatalf("terminal shutdown left owned processes: closePTY=%v", closePTY)
		}
		cleanupNeeded = false
	}
}
