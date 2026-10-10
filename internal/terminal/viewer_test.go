package terminal

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

func TestNativeCommandUsesSelectedAccountAndExactSession(t *testing.T) {
	t.Setenv("CODEX_ACCOUNT", "other")
	t.Setenv("CODEX_HOME", "/other/home")
	t.Setenv("OPENAI_API_KEY", "test-must-not-reach-child")
	c := config.Defaults()
	c.Runtime.AccountsDir = t.TempDir()
	c.Runtime.SharedCodexHome = t.TempDir()
	v := supervisor.ViewState{Binary: "/pinned/codex", Socket: "/tmp/session socket", ThreadID: "exact-thread", Account: "account-secondary", Cwd: "/workspace"}
	cmd := nativeCommand(c, v)
	env := strings.Join(cmd.Env, "\n")
	if !strings.Contains(env, "CODEX_ACCOUNT=account-secondary\n") || !strings.Contains(env, "CODEX_HOME="+c.Runtime.AccountsDir+"/account-secondary\n") || strings.Contains(env, "OPENAI_API_KEY=") || strings.Contains(env, "CODEX_ACCOUNT=other") {
		t.Fatal("child account was not isolated")
	}
	if cmd.Dir != v.Cwd || cmd.Args[len(cmd.Args)-1] != v.ThreadID || !strings.Contains(strings.Join(cmd.Args, "\n"), "unix://"+v.Socket) {
		t.Fatal("child did not attach to the chosen session")
	}
}

func TestViewerReconnectsOnAccountRotationAndExitsOnPause(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	first := supervisor.ViewState{Ready: true, RunID: "a", Socket: "a.sock", Account: "account-work", ThreadID: "same-thread"}
	var mu sync.Mutex
	next := first
	poll := func() (supervisor.ViewState, error) { mu.Lock(); defer mu.Unlock(); return next, nil }
	started := make(chan supervisor.ViewState, 4)
	result := make(chan error, 1)
	go func() {
		result <- follow(ctx, first, poll, func(v supervisor.ViewState) *exec.Cmd {
			started <- v
			return exec.Command("sleep", "10")
		}, 10*time.Millisecond)
	}()
	await := func(account string) {
		t.Helper()
		select {
		case v := <-started:
			if v.Account != account || v.ThreadID != "same-thread" {
				t.Fatal("wrong account or new conversation", v)
			}
		case <-ctx.Done():
			t.Fatal("UI did not reconnect")
		}
	}
	await("account-work")
	mu.Lock()
	next = supervisor.ViewState{State: "switching"}
	mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-started:
		t.Fatal("UI started before replacement worker was ready")
	default:
	}
	mu.Lock()
	next = first
	next.RunID, next.Socket, next.Account = "b", "b.sock", "account-secondary"
	mu.Unlock()
	await("account-secondary")
	mu.Lock()
	next = supervisor.ViewState{State: "paused", Paused: true}
	mu.Unlock()
	select {
	case e := <-result:
		if e != nil {
			t.Fatal(e)
		}
	case <-ctx.Done():
		t.Fatal("paused job reopened automatically")
	}
}

func TestNormalNativeQuitDoesNotReopenTheSession(t *testing.T) {
	v := supervisor.ViewState{Ready: true, RunID: "same", Account: "account-secondary"}
	starts := 0
	e := follow(context.Background(), v, func() (supervisor.ViewState, error) { return v, nil }, func(supervisor.ViewState) *exec.Cmd {
		starts++
		return exec.Command("true")
	}, time.Second)
	if e != nil || starts != 1 {
		t.Fatal("user quit was ignored", e, starts)
	}
}

func TestCleanBackendExitStillFollowsTheReplacementAccount(t *testing.T) {
	a := supervisor.ViewState{Ready: true, RunID: "old", Account: "account-work"}
	b := supervisor.ViewState{Ready: true, RunID: "new", Account: "account-secondary"}
	started := []string{}
	e := follow(context.Background(), a, func() (supervisor.ViewState, error) { return b, nil }, func(v supervisor.ViewState) *exec.Cmd {
		started = append(started, v.Account)
		return exec.Command("true")
	}, time.Hour)
	if e != nil || strings.Join(started, ",") != "account-work,account-secondary" {
		t.Fatal("a clean socket closure lost account recovery", e, started)
	}
}
