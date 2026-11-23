package nativecli

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
)

type webGoalRef struct {
	Objective string `json:"objective"`
	CreatedAt int64  `json:"createdAt"`
}

func (ref *webGoalRef) matches(goal *jobs.Goal) bool {
	return goal != nil && goal.Objective == ref.Objective && goal.CreatedAt == ref.CreatedAt
}

func (w *WebChat) changeGoal(ctx context.Context, target *webConnection, p webParams) (any, error) {
	if !w.webOnly {
		return nil, errors.New("목표 제어는 웹 전용 실행에서 사용할 수 있습니다.")
	}
	ref := p.ExpectedGoal
	if ref == nil || ref.Objective == "" || ref.CreatedAt <= 0 || utf8.RuneCountInString(ref.Objective) > 4000 {
		return nil, errors.New("현재 목표를 새로 확인해 주세요.")
	}
	const changed = "채팅의 목표가 바뀌었습니다. 현재 목표를 다시 확인해 주세요."
	current, err := target.client.Goal(ctx, p.Thread)
	if err != nil {
		return nil, err
	}
	if !ref.matches(current) {
		return nil, errors.New(changed)
	}
	client := target.client
	var owner *webInputOwner
	switch p.Mode {
	case "resume":
		switch current.Status {
		case "paused", "blocked", "usageLimited", "active":
		default:
			return nil, errors.New("이 목표 상태에서는 재개할 수 없습니다.")
		}
		if p.SwitchAccount {
			if err := w.verifyWebTransferTarget(ctx, target); err != nil {
				return nil, err
			}
		}
		owner, err = w.acquireWebChat(ctx, target, p.Thread, webAcquire{
			prepare: true, interrupt: p.SwitchAccount, external: p.SwitchAccount, objective: ref.Objective,
		})
	case "pause", "clear":
		home := config.Expand(w.c.Runtime.SharedCodexHome)
		pids, readErr := webHandoffWriters(ctx, home, p.Thread)
		if readErr != nil || len(pids) > 1 {
			return nil, errWebOtherOwner
		}
		if len(pids) == 1 {
			owner, err = w.openInputOwnerFor(ctx, p.Thread, pids[0], true)
		}
	default:
		return nil, errors.New("지원하는 목표 동작이 아닙니다.")
	}
	if err != nil {
		return nil, err
	}
	if owner != nil {
		defer owner.release()
		if err := w.verifyInputOwner(ctx, owner, p.Thread, owner.web == target); err != nil {
			return nil, err
		}
		client = owner.client
	}
	current, err = client.Goal(ctx, p.Thread)
	if err != nil {
		return nil, err
	}
	if !ref.matches(current) {
		return nil, errors.New(changed)
	}
	if p.Mode == "resume" && p.SwitchAccount && owner != nil {
		q, err := w.readOwnedQueue(p.Thread)
		if err != nil {
			return nil, err
		}
		if err := w.pinQueueToAccount(p.Thread, q, owner.web); err != nil {
			return nil, err
		}
	}
	args := map[string]any{"threadId": p.Thread, "origin": "user"}
	if p.Mode == "clear" {
		var response struct{ Cleared bool }
		if err := callWebInput(ctx, client, "thread/goal/clear", args, &response); err != nil {
			return nil, err
		}
		if !response.Cleared {
			return nil, errors.New(changed)
		}
		return map[string]any{"goal": nil}, nil
	}
	status := "paused"
	if p.Mode == "resume" {
		status = "active"
	}
	if current.Status == status {
		return map[string]any{"goal": current}, nil
	}
	if current.Status == "complete" || current.Status == "budgetLimited" {
		return nil, errors.New("목표의 완료 상태 또는 예산 한도를 확인해 주세요.")
	}
	args["status"] = status
	var response struct{ Goal *jobs.Goal }
	if err := callWebInput(ctx, client, "thread/goal/set", args, &response); err != nil {
		return nil, err
	}
	if !ref.matches(response.Goal) || response.Goal.Status != status {
		return nil, errWebInputUnconfirmed
	}
	if p.Mode == "resume" && owner != nil && owner.web != nil {
		w.rememberAccount(p.Thread, owner.web.account)
	}
	return map[string]any{"goal": response.Goal}, nil
}
