package nativecli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"golang.org/x/sys/unix"
)

func TestExecTakeoverAlwaysRequiresFreshTerminalConsent(t *testing.T) {
	l := Launch{Thread: fixtureID, Account: "chosen-account"}
	owner := execOwner{Process: processIdentity{PID: 1234}}
	for _, tc := range []struct {
		text      string
		tty, want bool
	}{
		{"y\n", true, true}, {"yes\n", true, true}, {"예\n", true, true},
		{"\n", true, false}, {"n\n", true, false}, {"yes", true, false},
		{"yes\n", false, false}, {"always\n", true, false},
	} {
		var out bytes.Buffer
		if got := confirmExecHandoff(strings.NewReader(tc.text), &out, tc.tty, l, owner); got != tc.want {
			t.Fatalf("consent tty=%v input=%q: %v", tc.tty, tc.text, got)
		}
		if tc.tty && (!strings.Contains(out.String(), l.Thread) || !strings.Contains(out.String(), l.Account) || !strings.Contains(out.String(), "1234") || !strings.Contains(out.String(), "[y/N]")) {
			t.Fatal("consent omitted the exact chat, process, account or default")
		}
		if !tc.tty && out.Len() != 0 {
			t.Fatal("noninteractive operation prompted for consent")
		}
	}
	if confirmExecHandoff(strings.NewReader("\n"), &bytes.Buffer{}, true, l, owner) {
		t.Fatal("previous consent persisted")
	}
}

func TestExecCannotAuthorizeItsOwnTermination(t *testing.T) {
	for _, ancestor := range []int{os.Getpid(), os.Getppid()} {
		if err := verifyIndependentCaller(context.Background(), ancestor); err == nil {
			t.Fatal("requesting process or its parent could be stopped")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyIndependentCaller(ctx, -1); err == nil {
		t.Fatal("an unreadable process tree was considered independent")
	}
}

func startExternalExecFixture(t *testing.T, binary, mode string, args ...string) (config.Config, Launch, *exec.Cmd) {
	t.Helper()
	c := fixture(t)
	home := os.Getenv("HOME")
	c.Runtime.CodexBinary = binary
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(), "CODEXM_EXEC_FIXTURE_ROOT="+home, "CODEXM_EXEC_FIXTURE_MODE="+mode)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill() // This test's synthetic child only.
		<-done
	})
	for until := time.Now().Add(5 * time.Second); ; {
		if _, err := os.Stat(filepath.Join(home, "ready")); err == nil {
			break
		}
		if time.Now().After(until) {
			t.Fatal("synthetic writer did not become ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
	l, err := Plan(context.Background(), c, fixtureID, "default")
	if err != nil {
		t.Fatal(err)
	}
	return c, l, cmd
}

func TestExternalExecOwnershipAndBoundedStop(t *testing.T) {
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof is required to inspect native ownership")
	}
	binary := filepath.Join(t.TempDir(), "codex-fixture")
	build := exec.Command("go", "build", "-o", binary, "testdata/exec_writer.go")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build native fixture: %v %s", err, output)
	}
	for _, mode := range []string{"cooperative", "ignore", "release-only", "multiple", "not-exec", "wrong-executable", "stale-identity", "changed-arguments", "unavailable-target"} {
		t.Run(mode, func(t *testing.T) {
			args := []string{"--no-daemon", "exec", "resume", fixtureID, "synthetic prompt"}
			if mode == "not-exec" {
				args = []string{"resume", fixtureID, "exec"}
			}
			c, l, cmd := startExternalExecFixture(t, binary, mode, args...)
			if mode == "wrong-executable" {
				l.Binary = os.Args[0]
			}
			ctx := context.Background()
			owner, err := findExecOwner(ctx, c, l)
			if mode == "multiple" || mode == "not-exec" || mode == "wrong-executable" {
				if err == nil {
					t.Fatal("unverified writer was accepted")
				}
			} else {
				if err != nil || owner.Process.PID != cmd.Process.Pid {
					t.Fatalf("exact exec owner not identified: %v", err)
				}
				if mode == "stale-identity" {
					owner.Key = "stale-incarnation"
				}
				if mode == "changed-arguments" {
					owner.Args[0] ^= 0xff
				}
				if mode == "unavailable-target" {
					c.Runtime.CodexBinary = filepath.Join(os.Getenv("HOME"), "missing-runtime")
				}
				wait, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
				defer cancel()
				err = stopConfirmedExec(wait, c, l, owner)
				if (err == nil) != (mode == "cooperative") {
					t.Fatalf("unexpected stop outcome for %s: %v", mode, err)
				}
			}
			signals, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), "signals"))
			wantSignal := mode == "cooperative" || mode == "ignore" || mode == "release-only"
			if (string(signals) == "SIGTERM\n") != wantSignal || strings.Count(string(signals), "SIGTERM") > 1 {
				t.Fatalf("wrong/repeated signal for %s: %q", mode, signals)
			}
			if mode == "cooperative" {
				if cr.WriterBusy(c.Runtime.SharedCodexHome, fixtureID) {
					t.Fatal("stopped process kept its writer")
				}
			} else if _, err := cr.NativeProcessIdentity(cmd.Process.Pid); err != nil {
				t.Fatal("a refused/timed-out takeover killed its writer", err)
			}
		})
	}
	t.Run("released-while-confirming", func(t *testing.T) {
		c, l, cmd := startExternalExecFixture(t, binary, "release-only", "exec", "resume", fixtureID)
		owner, err := findExecOwner(context.Background(), c, l)
		if err != nil {
			t.Fatal(err)
		}
		if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		for until := time.Now().Add(3 * time.Second); cr.WriterBusy(c.Runtime.SharedCodexHome, fixtureID); {
			if time.Now().After(until) {
				t.Fatal("fixture did not release its writer")
			}
			time.Sleep(20 * time.Millisecond)
		}
		wait, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		if err = stopConfirmedExec(wait, c, l, owner); err == nil {
			t.Fatal("a released lock was mistaken for a terminated exec")
		}
		signals, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), "signals"))
		if string(signals) != "SIGTERM\n" {
			t.Fatal("unverified process received another signal")
		}
	})
}

func TestPreparedResumeDoesNotArchiveARacingWriter(t *testing.T) {
	c := fixture(t)
	dir := filepath.Join(c.Runtime.SharedCodexHome, "thread-writer-locks")
	os.MkdirAll(dir, 0700)
	f, err := os.OpenFile(filepath.Join(dir, fixtureID+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	l, err := Plan(context.Background(), c, fixtureID, "default")
	if err != nil {
		t.Fatal(err)
	}
	l.Session, l.Pane, l.RequireFreeWriter = terminalName(c, fixtureID, "default"), "%123", true
	if err = saveSession(c, l); err != nil {
		t.Fatal(err)
	}
	err = RunSession(context.Background(), c, sessionPath(c, l.Session))
	var foreign *ForeignWriterError
	if !errors.As(err, &foreign) {
		t.Fatalf("racing writer was not rejected: %v", err)
	}
	if _, err = os.Stat(filepath.Join(os.Getenv("HOME"), "argv")); !os.IsNotExist(err) {
		t.Fatal("prepared launcher attempted archive/unarchive/resume against a new owner")
	}
}
