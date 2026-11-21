package nativecli

import (
	"context"
	"errors"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

var errWebExitOwner = errors.New("이 채팅을 열어 둔 Codex 앱이나 CLI에서 종료해 주세요.")

func (w *WebChat) exitWebThread(ctx context.Context, operation string, p webParams) (any, error) {
	if !w.webOnly {
		return nil, errors.New("채팅 종료는 웹 전용 실행에서 사용할 수 있습니다.")
	}
	unlock, err := lockChatMutation(ctx, w.c, p.Thread)
	if err != nil {
		return nil, err
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	pids, err := webHandoffWriters(ctx, home, p.Thread)
	if err != nil || len(pids) > 1 {
		return nil, errWebExitOwner
	}
	var owner *webInputOwner
	if len(pids) == 1 {
		owner, err = w.openInputOwnerFor(ctx, p.Thread, pids[0], false)
		if err != nil {
			return nil, errWebExitOwner
		}
		defer owner.release()
	}
	queue, err := w.readOwnedQueue(p.Thread)
	if err != nil {
		return nil, err
	}
	pause, err := w.readQueuePause(p.Thread)
	if err != nil {
		return nil, err
	}
	holdQueue := func() error {
		if pause == operation {
			return nil
		}
		if err := config.AtomicWrite(w.queuePausePath(p.Thread), []byte(operation), 0600); err != nil {
			return err
		}
		pause = operation
		return nil
	}
	if owner != nil {
		var interrupted string
		var goal *jobs.Goal
		beforeStop := func(ctx context.Context, turn string) error {
			if turn != p.Turn {
				return errors.New("현재 작업이 바뀌었습니다. 실행 상태를 확인한 뒤 종료해 주세요.")
			}
			if err := holdQueue(); err != nil {
				return err
			}
			var err error
			goal, err = pauseWebGoalForStop(ctx, owner.client, p.Thread)
			interrupted = turn
			return err
		}
		if err := w.settleWebTurn(ctx, owner, p.Thread, p.Turn != "", beforeStop); err != nil {
			return nil, err
		}
		if interrupted != "" {
			if err := w.holdStoppedContinuation(p.Thread, interrupted, goal); err != nil {
				return nil, err
			}
		}
	}
	if pause != "" || nextWebQueuedInput(queue) >= 0 {
		if err := holdQueue(); err != nil {
			return nil, err
		}
	}
	if cr.WriterBusy(home, p.Thread) {
		if owner == nil {
			return nil, errWebExitOwner
		}
		if err := w.verifyInputOwner(ctx, owner, p.Thread, false); err != nil {
			return nil, err
		}
		if err := callWebInput(ctx, owner.client, "thread/unsubscribe", map[string]any{"threadId": p.Thread}, nil); err != nil {
			return nil, err
		}
		if err := w.waitWebRelease(ctx, owner, p.Thread); err != nil {
			return nil, err
		}
	}
	return map[string]any{"thread_id": p.Thread, "status": "released"}, nil
}
