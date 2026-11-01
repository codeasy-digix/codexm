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

	"github.com/codeasy-digix/codexm/internal/config"
)

func TestStalePaneDoesNotKillOrReplaceAnotherSession(t *testing.T) {
	for _, display := range []string{"exit 1", "printf 'different-session|0\\n'"} {
		t.Run(display, func(t *testing.T) {
			c := fixture(t)
			h := os.Getenv("HOME")
			script := "#!/bin/sh\ncase \"$1\" in\ndisplay-message) " + display + " ;;\nhas-session) exit 0 ;;\n*) printf '%s\\n' \"$1\" >> \"$HOME/unexpected-tmux\" ;;\nesac\n"
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
case "$1" in
has-session) ` + map[bool]string{true: "exit 1", false: "exit 0"}[mode == "closed"] + ` ;;
display-message)
  case "$*" in
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
				if err == nil || !strings.Contains(err.Error(), "selected account other was not applied") {
					t.Fatalf("account switch was hidden: %v", err)
				}
			} else if mode == "multiple-chats" {
				if err == nil || !strings.Contains(err.Error(), "multiple loaded chats") {
					t.Fatalf("ambiguous chat was attached: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if (got != nil) != (mode == "live") {
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
	script := "#!/bin/sh\ncase \"$1\" in\nhas-session) exit 1 ;;\n*) printf 'unrelated-session|0\\n' ;;\nesac\n"
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
	script := "#!/bin/sh\ncase \"$1\" in\nhas-session) exit 0 ;;\ndisplay-message) printf 'codexm-cli-0123456789abcdef|1|1\\n' ;;\ncapture-pane) printf 'Pane is dead (status 1)\\n\\n\\n' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(h, "tmux"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", h+string(os.PathListSeparator)+os.Getenv("PATH"))
	got, err := Screen(context.Background(), c, l.Session)
	if err != nil || got.Alive || got.ExitCode == nil || *got.ExitCode != 1 || got.Launch.Error != l.Error || got.Text != "Pane is dead (status 1)" {
		t.Fatalf("lost native failure metadata: %+v %v", got, err)
	}
}
