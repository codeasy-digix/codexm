package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/supervisor"
	"golang.org/x/term"
)

type pollResult struct {
	view supervisor.ViewState
	err  error
}

func nativeCommand(c config.Config, v supervisor.ViewState) *exec.Cmd {
	shared, _ := json.Marshal(config.Expand(c.Runtime.SharedCodexHome))
	cmd := exec.Command(v.Binary, "resume", "-c", "sqlite_home="+string(shared), "-c", "check_for_update_on_startup=false", "--remote", "unix://"+v.Socket, v.ThreadID)
	cmd.Env = cr.Env(c, v.Account, v.Binary)
	cmd.Dir = v.Cwd
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd
}

func sameRun(a, b supervisor.ViewState) bool {
	return a.Ready && b.Ready && a.RunID == b.RunID && a.Socket == b.Socket && a.Account == b.Account && a.ThreadID == b.ThreadID && a.Binary == b.Binary
}

func stopped(v supervisor.ViewState) bool {
	return !v.Ready && (v.Paused || v.State == "stopped" || v.State == "complete" || v.State == "archived")
}

// Run attaches only a UI to the supervisor-owned engine. Heartbeats and account
// reconnections never submit turn/start or replay a user instruction.
func Run(ctx context.Context, c config.Config, id, clientID string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("공식 Codex 화면은 대화형 터미널에서 열어야 합니다")
	}
	if clientID == "" {
		return errors.New("missing terminal client ID")
	}
	state, _ := term.GetState(int(os.Stdin.Fd()))
	defer func() {
		if state != nil {
			_ = term.Restore(int(os.Stdin.Fd()), state)
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = supervisor.Request(cleanup, c, "detach", supervisor.JobArgs{ClientID: clientID}, nil)
	}()
	var v supervisor.ViewState
	start, cancel := context.WithTimeout(ctx, 5*time.Second)
	e := supervisor.Request(start, c, "view", supervisor.JobArgs{ID: id, ClientID: clientID}, &v)
	cancel()
	if e != nil {
		return e
	}
	poll := func() (supervisor.ViewState, error) {
		var next supervisor.ViewState
		req, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		e := supervisor.Request(req, c, "client", supervisor.JobArgs{ID: id, ClientID: clientID}, &next)
		return next, e
	}
	return follow(ctx, v, poll, func(v supervisor.ViewState) *exec.Cmd { return nativeCommand(c, v) }, 2*time.Second)
}

// follow isolates process replacement from terminal creation, making a quota
// rotation reconnect the existing window instead of opening another window.
func follow(ctx context.Context, current supervisor.ViewState, poll func() (supervisor.ViewState, error), command func(supervisor.ViewState) *exec.Cmd, interval time.Duration) error {
	updates := make(chan pollResult, 1)
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-t.C:
				v, e := poll()
				select {
				case updates <- pollResult{v, e}:
				case <-watchCtx.Done():
					return
				}
			}
		}
	}()
	var child *exec.Cmd
	var done chan error
	stopChild := func() {
		if child == nil {
			return
		}
		_ = child.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = child.Process.Kill()
			<-done
		}
		child, done = nil, nil
	}
	defer stopChild()
	lastGood := time.Now()
	waiting := time.Now()
	hadUI := false
	for {
		if child == nil && current.Ready {
			child = command(current)
			if e := child.Start(); e != nil {
				return e
			}
			done = make(chan error, 1)
			hadUI = true
			cmd, completed := child, done
			go func() { completed <- cmd.Wait() }()
		}
		select {
		case <-ctx.Done():
			return nil
		case e := <-done:
			child, done = nil, nil
			// A normal /quit closes this viewer. A lost backend is checked once
			// before exiting, because its replacement may not yet be ready.
			next, checkErr := poll()
			if checkErr == nil && !sameRun(current, next) {
				current = next
				waiting = time.Now()
				continue
			}
			if e == nil {
				return nil
			}
			return fmt.Errorf("Codex 화면 종료: %w", e)
		case update := <-updates:
			if update.err != nil {
				if time.Since(lastGood) > 60*time.Second {
					return fmt.Errorf("감시 서비스 연결 실패: %w", update.err)
				}
				continue
			}
			lastGood = time.Now()
			if child != nil && !sameRun(current, update.view) {
				stopChild()
				waiting = time.Now()
			}
			current = update.view
			if child == nil && hadUI && stopped(current) {
				fmt.Fprintln(os.Stdout, "codexm: 작업 화면을 닫았습니다. 상태:", current.State)
				return nil
			}
			if child == nil && !current.Ready && current.Error != "" && current.State != "quota_wait" && current.State != "switching" && current.State != "retry_wait" {
				return errors.New(current.Error)
			}
			if child == nil && !current.Ready && time.Since(waiting) > 90*time.Second && current.State != "quota_wait" && current.State != "switching" {
				return errors.New("공식 Codex 화면을 준비하지 못했습니다. codexm의 /status 또는 /events를 확인하세요")
			}
		}
	}
}
