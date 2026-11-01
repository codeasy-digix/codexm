package nativecli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
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
