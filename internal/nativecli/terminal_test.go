package nativecli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
)

func TestStalePaneDoesNotKillOrReplaceAnotherSession(t *testing.T) {
	for _, display := range []string{"exit 1", "printf 'different-session|0\\n'"} {
		t.Run(display, func(t *testing.T) {
			c := fixture(t)
			h := os.Getenv("HOME")
			script := "#!/bin/sh\n[ \"$1\" = \"-L\" ] && shift 2\ncase \"$1\" in\ndisplay-message) " + display + " ;;\nhas-session) exit 0 ;;\n*) printf '%s\\n' \"$1\" >> \"$HOME/unexpected-tmux\" ;;\nesac\n"
			if err := os.WriteFile(filepath.Join(h, "tmux"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", h+string(os.PathListSeparator)+os.Getenv("PATH"))
			hash := sha256.Sum256([]byte(c.Runtime.SharedCodexHome + "\x00" + fixtureID + "\x00default"))
			l := Launch{Session: "codexm-cli-" + hex.EncodeToString(hash[:8]), Thread: fixtureID, Account: "default", Pane: "%123", Ready: true}
			if err := saveSession(c, l); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if err := Close(ctx, c, l.Session); err == nil {
				t.Fatal("stale pane ownership was accepted for close")
			}
			if _, err := Open(ctx, c, "/unused", fixtureID, "default"); err == nil {
				t.Fatal("stale pane ownership was accepted for relaunch")
			}
			if _, err := os.Stat(filepath.Join(h, "unexpected-tmux")); !os.IsNotExist(err) {
				t.Fatal("an unowned terminal was changed")
			}
		})
	}
}

func reconnectFixture(t *testing.T, mode string) (config.Config, Launch) {
	t.Helper()
	c := fixture(t)
	h := os.Getenv("HOME")
	l := Launch{Session: terminalName(c, fixtureID, "default"), Thread: fixtureID, Account: "default", Pane: "%123", Ready: true, Binary: "/official/codex", Cwd: h}
	if err := os.MkdirAll(filepath.Join(c.Runtime.SharedCodexHome, "thread-writer-locks"), 0700); err != nil {
		t.Fatal(err)
	}
	lockDir, err := filepath.EvalSymlinks(filepath.Join(c.Runtime.SharedCodexHome, "thread-writer-locks"))
	if err != nil {
		t.Fatal(err)
	}
	if err := saveSession(c, l); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
[ "$1" = "-L" ] && shift 2
case "$1" in
has-session) ` + map[bool]string{true: "exit 1", false: "exit 0"}[mode == "closed"] + ` ;;
display-message)
  case "$*" in
    *socket_path*) printf 'fixture-tmux\n' ;;
    *pane_current_command*) printf '` + l.Session + `|734567|codex\n' ;;
    *) printf '` + l.Session + `|0|\n' ;;
  esac ;;
capture-pane) printf 'existing Codex\n' ;;
attach-session|switch-client) printf '%s\n' "$@" > "$HOME/attached" ;;
*) printf '%s\n' "$@" > "$HOME/unexpected-tmux"; exit 8 ;;
esac
`
	lockPID := "734567"
	if mode == "different-owner" {
		lockPID = "999999"
	}
	lsof := "#!/bin/sh\nprintf 'p" + lockPID + "\\nn" + filepath.Join(lockDir, fixtureID+".lock") + "\\n'\n"
	if mode == "multiple-chats" {
		lsof += "printf 'n" + filepath.Join(lockDir, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee.lock") + "\\n'\n"
	}
	if mode == "closed" {
		lsof = "#!/bin/sh\nexit 9\n"
	}
	for name, body := range map[string]string{"tmux": script, "lsof": lsof} {
		if err := os.WriteFile(filepath.Join(h, name), []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", h+string(os.PathListSeparator)+os.Getenv("PATH"))
	return c, l
}

func TestReusableTerminalRequiresCurrentThreadOwnerAndAccount(t *testing.T) {
	for _, mode := range []string{"live", "closed", "different-owner", "different-account", "multiple-chats"} {
		t.Run(mode, func(t *testing.T) {
			c, l := reconnectFixture(t, mode)
			selected := "default"
			if mode == "different-account" {
				selected = "other"
			}
			got, err := ReusableTerminal(context.Background(), c, l.Thread, selected)
			if mode == "different-account" {
				if err != nil || got == nil || got.Account != "default" {
					t.Fatalf("running authentication changed: %+v %v", got, err)
				}
			} else if mode == "multiple-chats" {
				if err == nil || !strings.Contains(err.Error(), "multiple loaded chats") {
					t.Fatalf("ambiguous chat was attached: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if (got != nil) != (mode == "live" || mode == "different-account") {
				t.Fatalf("wrong reconnect decision: %+v %v", got, err)
			}
			if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), "argv")); !os.IsNotExist(err) {
				t.Fatal("reconnect attempted archive/resume")
			}
			if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), "unexpected-tmux")); !os.IsNotExist(err) {
				t.Fatal("reconnect changed a terminal")
			}
		})
	}
}

func TestForegroundResumeReconnectsWithoutArchive(t *testing.T) {
	c, l := reconnectFixture(t, "live")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	for _, nested := range []bool{false, true} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestResumeReconnectionHelper$")
		tmuxEnv := ""
		if nested {
			tmuxEnv = "fixture-tmux"
		}
		cmd.Env = append(os.Environ(), "CODEXM_RECONNECT_HELPER="+c.Path, "TMUX="+tmuxEnv)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("reconnect: %v %s", err, out)
		}
		want := "attach-session"
		if nested {
			want = "switch-client"
		}
		data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), "attached"))
		if err != nil || string(data) != want+"\n-t\n="+l.Session+"\n" {
			t.Fatalf("wrong attach args: %q %v", data, err)
		}
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), "argv")); !os.IsNotExist(err) {
		t.Fatal("resume attempted an archive")
	}
}

func TestResumeReconnectionHelper(t *testing.T) {
	file := os.Getenv("CODEXM_RECONNECT_HELPER")
	if file == "" {
		return
	}
	c, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	if err = Resume(context.Background(), c, Launch{Thread: fixtureID, Account: "default"}); err != nil {
		t.Fatal(err)
	}
	t.Fatal("reconnect did not exec")
}

func TestClosedSessionReceiptDoesNotUseTmuxCurrentPane(t *testing.T) {
	c := fixture(t)
	h := os.Getenv("HOME")
	script := "#!/bin/sh\n[ \"$1\" = \"-L\" ] && shift 2\ncase \"$1\" in\nhas-session) exit 1 ;;\n*) printf 'unrelated-session|0\\n' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(h, "tmux"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", h+string(os.PathListSeparator)+os.Getenv("PATH"))
	l := Launch{Session: "codexm-cli-0123456789abcdef", Thread: fixtureID, Account: "default", Pane: "%123", Ready: true}
	if err := saveSession(c, l); err != nil {
		t.Fatal(err)
	}
	screen, err := Screen(context.Background(), c, l.Session)
	if err != nil || screen.Alive || screen.Text != "" {
		t.Fatalf("closed terminal resolved to another tmux pane: %+v %v", screen, err)
	}
}

func TestDeadOwnedPaneExposesExitCodeAndHandoffError(t *testing.T) {
	c := fixture(t)
	h := os.Getenv("HOME")
	l := Launch{Session: "codexm-cli-0123456789abcdef", Thread: fixtureID, Account: "default", Pane: "%123", Error: "official archive failed; resume was not started"}
	if err := saveSession(c, l); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n[ \"$1\" = \"-L\" ] && shift 2\ncase \"$1\" in\nhas-session) exit 0 ;;\ndisplay-message) printf 'codexm-cli-0123456789abcdef|1|1\\n' ;;\ncapture-pane) printf 'Pane is dead (status 1)\\n\\n\\n' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(h, "tmux"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", h+string(os.PathListSeparator)+os.Getenv("PATH"))
	got, err := Screen(context.Background(), c, l.Session)
	if err != nil || got.Alive || got.ExitCode == nil || *got.ExitCode != 1 || got.Launch.Error != l.Error || got.Text != "Pane is dead (status 1)" {
		t.Fatalf("lost native failure metadata: %+v %v", got, err)
	}
}

func TestTmuxNamespaceRoutesNewAndLegacyReceipts(t *testing.T) {
	for _, server := range []string{"", "codex", CodexTmuxServer} {
		t.Run("server="+server, func(t *testing.T) {
			c := fixture(t)
			h := os.Getenv("HOME")
			l := Launch{Session: terminalName(c, fixtureID, "default"), Thread: fixtureID, Account: "default", Pane: "%7", TmuxServer: server, Ready: true}
			if err := saveSession(c, l); err != nil {
				t.Fatal(err)
			}
			expected := server
			if expected == "" {
				expected = "default"
			}
			script := `#!/bin/sh
[ "$1" = "-L" ] && [ "$2" = "` + expected + `" ] || exit 97
shift 2
printf '%s\n' "$1" >> "$HOME/tmux-routes"
case "$1" in
has-session) exit 0;;
display-message) printf '` + l.Session + `|0|\n';;
capture-pane) printf 'correct server\n';;
load-buffer) cat > "$HOME/input-text";;
paste-buffer|send-keys|delete-buffer|kill-pane) exit 0;;
*) exit 98;;
esac
`
			os.WriteFile(filepath.Join(h, "tmux"), []byte(script), 0700)
			t.Setenv("PATH", h+string(os.PathListSeparator)+os.Getenv("PATH"))
			screen, err := Screen(context.Background(), c, l.Session)
			if err != nil || screen.Text != "correct server" || screen.Attach != "tmux -L "+expected+" attach -t ="+l.Session {
				t.Fatalf("wrong server: %+v %v", screen, err)
			}
			if err = Input(context.Background(), c, l.Session, "literal $(do-not-run)", ""); err != nil {
				t.Fatal(err)
			}
			if err = Input(context.Background(), c, l.Session, "", "C-c"); err != nil {
				t.Fatal(err)
			}
			if err = Close(context.Background(), c, l.Session); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(filepath.Join(h, "input-text"))
			if string(data) != "literal $(do-not-run)" {
				t.Fatal("literal input changed")
			}
		})
	}
}
func TestTmuxAttachSwitchesOnlyWithinSameServer(t *testing.T) {
	for _, tt := range []struct{ current, socket, want string }{
		{"", "/tmp/tmux-501/codex", "attach-session"},
		{"/tmp/tmux-501/default,123,0", "/tmp/tmux-501/codex", "attach-session"},
		{"/tmp/tmux-501/codex,456,2", "/tmp/tmux-501/codex", "switch-client"},
	} {
		mode, env := attachMode(tt.current, tt.socket, []string{"TMUX=" + tt.current, "HOME=/safe"})
		if mode != tt.want {
			t.Fatal("wrong attach mode", mode)
		}
		if mode == "attach-session" && strings.Contains(strings.Join(env, "\n"), "TMUX=") {
			t.Fatal("cross-server nesting guard not cleared")
		}
	}
}

// Real tmux, synthetic history/login, fake official CLI. No real chat is opened.
func TestNativeTerminalUsesSharedWorkspace(t *testing.T) {
	self := os.Getenv("CODEXM_TUI_TEST_BINARY")
	if self == "" {
		t.Skip("opt-in built native launcher")
	}
	c := fixture(t)
	script := `#!/bin/sh
printf 'NAMESPACE_TEST_READY\n'
IFS= read -r line
printf '%s' "$line" > "$HOME/native-input"
printf 'NAMESPACE_TEST_FINISHED\n'
`
	os.WriteFile(c.Runtime.CodexBinary, []byte(script), 0700)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	l, err := Open(context.Background(), c, self, fixtureID, "default")
	if err != nil {
		t.Fatal(err)
	}
	defer Close(context.Background(), c, l.Session)
	if l.TmuxServer != CodexTmuxServer {
		t.Fatal("new terminal used the wrong server")
	}
	if l.TmuxSession != WorkspaceSession || l.Window == "" {
		t.Fatalf("missing workspace identity: %+v", l)
	}
	other, err := tmux(context.Background(), "default", "new-window", "-d", "-P", "-F", "#{pane_id}", "-t", WorkspaceSession+":", "-n", "verify-neighbor", "/bin/sleep", "60")
	if err != nil {
		t.Fatal(err)
	}
	other = strings.TrimSpace(other)
	defer tmux(context.Background(), "default", "kill-pane", "-t", other)
	defer func() {
		_ = Close(context.Background(), c, l.Session)
		if _, err := tmux(context.Background(), "default", "display-message", "-p", "-t", other, "#{pane_pid}"); err != nil {
			t.Fatal("closing one chat killed another workspace window")
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		screen, e := Screen(context.Background(), c, l.Session)
		if e != nil {
			t.Fatal(e)
		}
		if screen.Launch.Ready && strings.Contains(screen.Text, "NAMESPACE_TEST_READY") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("native terminal never ready: %+v", screen)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err = Input(context.Background(), c, l.Session, "literal $value; data", ""); err != nil {
		t.Fatal(err)
	}
	for {
		data, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), "native-input"))
		if string(data) == "literal $value; data" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("native input not received: %q", data)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
