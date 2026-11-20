package nativecli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

var errWebAccountBusy = errors.New("이 채팅의 현재 작업이 끝나기를 기다리고 있습니다.")
var errWebOtherOwner = errors.New("다른 앱의 채팅 제어 연결을 확인할 수 없습니다. 원래 창에서 이 채팅을 닫거나 보관한 뒤 이어갈 수 있습니다.")
var errWebWriterStillHeld = errors.New("기존 실행에서 채팅 잠금이 풀리지 않았습니다.")

func sameWebLogin(a, b *webConnection) bool {
	return a == b || (a.login.Key != "" && a.login.Key == b.login.Key)
}

func (w *WebChat) interruptWebThread(ctx context.Context, operation string, p webParams) (any, error) {
	if p.Turn == "" {
		return nil, errWebNoActiveTurn
	}
	pids, err := webWriterPIDs(ctx, config.Expand(w.c.Runtime.SharedCodexHome), p.Thread)
	if err != nil || len(pids) != 1 {
		return nil, errWebNoActiveTurn
	}
	owner, err := w.openInputOwner(ctx, p.Thread, pids[0])
	if err != nil {
		return nil, errWebOtherOwner
	}
	defer owner.release()
	if err := w.verifyInputOwner(ctx, owner, p.Thread, false); err != nil {
		return nil, err
	}
	turns, err := recentWebTurns(ctx, owner.client, p.Thread, 1)
	if err != nil {
		return nil, err
	}
	if len(turns) != 1 || turns[0].ID != p.Turn {
		return nil, errWebNoActiveTurn
	}
	// Set the durable barrier under the same lock used for enqueue and the
	// worker, before the native turn can become idle.
	if err := config.AtomicWrite(w.queuePausePath(p.Thread), []byte(operation), 0600); err != nil {
		return nil, err
	}
	interrupted := turns[0].Status == "inProgress"
	if interrupted {
		goal, err := pauseWebGoalForStop(ctx, owner.client, p.Thread)
		if err != nil {
			return nil, err
		}
		if err := callWebInput(ctx, owner.client, "turn/interrupt", map[string]any{"threadId": p.Thread, "turnId": p.Turn}, nil); err != nil {
			return nil, err
		}
		if err := w.holdStoppedContinuation(p.Thread, p.Turn, goal); err != nil {
			return nil, err
		}
	}
	return map[string]any{"thread_id": p.Thread, "interrupted": interrupted, "queue_pause": operation}, nil
}

func pauseWebGoalForStop(ctx context.Context, client *cr.Client, id string) (*jobs.Goal, error) {
	goal, err := client.Goal(ctx, id)
	if err != nil || goal == nil || goal.Status != "active" {
		return goal, err
	}
	var reply struct{ Goal *jobs.Goal }
	args := map[string]any{"threadId": id, "status": "paused", "origin": "user"}
	if err := callWebInput(ctx, client, "thread/goal/set", args, &reply); err != nil {
		return nil, err
	}
	ref := webGoalRef{Objective: goal.Objective, CreatedAt: goal.CreatedAt}
	if !ref.matches(reply.Goal) || reply.Goal.Status != "paused" {
		return nil, errWebInputUnconfirmed
	}
	return reply.Goal, nil
}

func (w *WebChat) holdStoppedContinuation(id, turn string, goal *jobs.Goal) error {
	q, err := w.readOwnedQueue(id)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(q, func(input webQueuedInput) bool {
		return input.State == "running" && input.ResumeFrom != "" && (input.ResumedTurn == turn ||
			(input.ResumeGoal != nil && input.ResumeGoal.matches(goal)))
	})
	if index < 0 {
		return nil
	}
	q[index].State, q[index].Detail = "held", "사용자가 이전 작업을 중지했습니다."
	return w.saveOwnedQueue(id, q)
}

func (w *WebChat) sendWebOnly(ctx context.Context, target *webConnection, r Request, p webParams) (any, error) {
	if p.Model != "" || p.Effort != "" {
		if p.Model == "" {
			return nil, errors.New("effort를 적용할 모델을 선택해 주세요.")
		}
		if err := target.validateTurnModel(ctx, p.Model, p.Effort); err != nil {
			return nil, err
		}
	}
	automatic := p.Mode == "immediate" && p.Turn == ""
	if automatic || p.Mode == "queue" {
		var err error
		target, p.Thread, err = w.webDraftAccount(ctx, target, p.Thread)
		if err != nil {
			return nil, err
		}
	}
	if err := w.preparePrompt(ctx, target, &p); err != nil {
		return nil, err
	}
	if p.Mode == "queue" || p.Plan {
		return w.enqueueWebInput(ctx, target, r, p)
	}
	if paused, err := w.readQueuePause(p.Thread); err != nil {
		return nil, err
	} else if paused != "" {
		// Starting a regular turn would also release a native queue when it
		// finishes. Preserve those older submissions until explicit resume.
		queue, err := readWebQueue(ctx, target.client, p.Thread)
		if err != nil {
			return nil, err
		}
		if len(queue.Data) > 0 {
			return nil, errors.New("Codex 기본 대기열이 일시 중지돼 있습니다. 대기열을 재개한 뒤 전송해 주세요.")
		}
	}
	var owner *webInputOwner
	if !p.SwitchAccount {
		// Check the selected server first. A loaded chat still gets a fresh
		// writer check before start, steer, or a goal mutation.
		probe, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		owner, _ = w.openInputOwnerFor(probe, p.Thread, target.process.PID, false)
		cancel()
	}
	if owner == nil {
		var err error
		owner, err = w.acquireWebChat(ctx, target, p.Thread, webAcquire{
			prepare: automatic, interrupt: automatic, external: automatic && p.SwitchAccount,
			objective: goalObjective(p.Text, p.Goal),
		})
		if err != nil {
			return nil, err
		}
	}
	defer owner.release()
	if p.SwitchAccount {
		q, err := w.readOwnedQueue(p.Thread)
		if err != nil {
			return nil, err
		}
		if err := w.pinQueueToAccount(p.Thread, q, owner.web); err != nil {
			return nil, err
		}
	}
	if p.Goal {
		return w.deliverGoal(ctx, owner, target, r, p)
	}
	return w.deliverWebInput(ctx, owner, target, r, p)
}

// An empty native draft exists only in its creating server. If no input was
// ever saved, a different login needs its own empty draft before the first send.
func (w *WebChat) webDraftAccount(ctx context.Context, target *webConnection, id string) (*webConnection, string, error) {
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	if _, err := catalog.Lookup(ctx, home, id); !errors.Is(err, sql.ErrNoRows) {
		return target, id, nil
	}
	draft, err := w.thread(ctx, id)
	if err != nil {
		return target, id, err
	}
	if draft == nil || draft.Account == target.account {
		return target, id, nil
	}
	w.mu.Lock()
	source := w.connections[draft.Account]
	w.mu.Unlock()
	if source != nil && sameWebLogin(source, target) && !w.loginChanged(source) {
		return source, id, nil
	}
	if source == nil || draft.Status.Type != "idle" {
		return target, id, errWebAccountBusy
	}
	chat, _, err := source.client.Recent(ctx, id, 1, "")
	if err != nil || chat.ID != id || len(chat.Turns) != 0 {
		return target, id, errors.New("저장된 채팅이 확인될 때 다시 계정을 선택해 주세요.")
	}
	if chat.Path != "" {
		if _, err := os.Lstat(chat.Path); !os.IsNotExist(err) {
			return target, id, errors.New("저장된 채팅이 확인될 때 다시 계정을 선택해 주세요.")
		}
	}
	if q, err := w.readOwnedQueue(id); err != nil || len(q) != 0 {
		return target, id, errors.New("이 초안에 대기 중인 입력이 있습니다. 먼저 대기 입력을 확인해 주세요.")
	}
	if q, err := readWebQueue(ctx, source.client, id); err != nil || len(q.Data) != 0 {
		return target, id, errors.New("이 초안에 대기 중인 입력이 있는지 확인해 주세요.")
	}
	if err := w.verifyLogin(ctx, target); err != nil {
		return target, id, err
	}
	replacement, err := w.createWebThread(ctx, target, draft.Cwd, draft.Project)
	return target, replacement.ID, err
}

type webAcquire struct {
	prepare, interrupt, external bool
	objective                    string
	beforeInterrupt              func(context.Context, string) error
}

func webHandoffWriters(ctx context.Context, home, id string) ([]int, error) {
	if !cr.WriterBusy(home, id) {
		return nil, nil
	}
	pids, err := webWriterPIDs(ctx, home, id)
	if err != nil {
		return nil, err
	}
	// A lock can expire while lsof is running. Conversely, no observable
	// PID is not permission to resume a still-locked chat.
	if !cr.WriterBusy(home, id) {
		return nil, nil
	}
	if len(pids) == 0 {
		return nil, errWebOtherOwner
	}
	return pids, nil
}

func (w *WebChat) acquireWebChat(ctx context.Context, target *webConnection, id string, request webAcquire) (*webInputOwner, error) {
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	pids, err := webHandoffWriters(ctx, home, id)
	if err != nil {
		return nil, err
	}
	switch len(pids) {
	case 1:
		probe, cancel := context.WithTimeout(ctx, 5*time.Second)
		source, err := w.openInputOwnerFor(probe, id, pids[0], request.external)
		cancel()
		if err != nil {
			if !request.external || !request.interrupt || !request.prepare {
				return nil, errWebOtherOwner
			}
			if err := w.releaseTrackedCLI(ctx, target, id, pids[0], request); err != nil {
				return nil, err
			}
			break
		}
		same := source.web != nil && sameWebLogin(source.web, target)
		if same && (source.web == target || !w.loginChanged(source.web)) {
			return source, nil
		}
		defer source.release()
		if !request.prepare {
			return nil, errWebNoActiveTurn
		}
		if err := w.verifyWebTransferTarget(ctx, target); err != nil {
			return nil, err
		}
		if source.web == nil {
			probe, cancel := context.WithTimeout(ctx, 3*time.Second)
			key, _, err := liveWebLogin(probe, source.client)
			cancel()
			if err != nil {
				return nil, errWebOtherOwner
			}
			if key != "" && key == target.login.Key {
				return nil, errors.New("원래 창과 다른 로그인 계정을 선택해 주세요.")
			}
		}
		if request.objective != "" {
			if _, err := readGoalForChange(ctx, source.client, id, request.objective); err != nil {
				return nil, err
			}
		}
		restore, err := w.releaseWebChat(ctx, source, id, request.interrupt && !same, request.beforeInterrupt)
		if err != nil {
			return nil, err
		}
		if restore {
			if cr.WriterBusy(home, id) {
				return nil, errWebOtherOwner
			}
			if err := callWebInput(ctx, source.client, "thread/unarchive", map[string]any{"threadId": id}, nil); err != nil {
				return nil, err
			}
		}
	case 0:
		if !request.prepare {
			return nil, errWebNoActiveTurn
		}
	default:
		return nil, errWebOtherOwner
	}
	if err := w.verifyLogin(ctx, target); err != nil {
		return nil, err
	}
	if err := w.prepare(ctx, target, id); err != nil {
		return nil, err
	}
	return w.openInputOwner(ctx, id, target.process.PID)
}

// The browser can stop a CLI CodexM itself launched only for an explicit
// account change on its one verified chat. Other processes need owner RPC.
func (w *WebChat) releaseTrackedCLI(ctx context.Context, target *webConnection, id string, pid int, request webAcquire) error {
	source, err := readLast(w.c, id)
	if err != nil || source.Owner.PID != pid || source.AuthKey == "" {
		return errWebOtherOwner
	}
	if err := w.verifyWebTransferTarget(ctx, target); err != nil {
		return err
	}
	key, err := authKey(w.c, target.loginName)
	if err != nil || key == source.AuthKey {
		return errors.New("기존 터미널과 다른 로그인 계정을 선택해 주세요.")
	}
	if request.objective != "" {
		if _, err := readGoalForChange(ctx, target.client, id, request.objective); err != nil {
			return err
		}
	}
	if err := verifyIndependentCaller(ctx, pid); err != nil {
		return err
	}
	if err := verifyOwnedCLI(ctx, w.c, source); err != nil {
		return errWebOtherOwner
	}
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	if request.beforeInterrupt != nil {
		activity := readWebProcessActivity(ctx, home, id, *source.Owner)
		switch activity.State {
		case "running":
			if err := request.beforeInterrupt(ctx, activity.TurnID); err != nil {
				return err
			}
			if err := verifyOwnedCLI(ctx, w.c, source); err != nil {
				return errWebOtherOwner
			}
		case "idle":
		default:
			return errors.New("원래 터미널의 작업 상태를 확인할 수 없어 인계를 보류했습니다.")
		}
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return err
	}
	wait, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	owner := execOwner{Process: *source.Owner, Key: source.OwnerKey}
	return waitExecRelease(wait, home, id, owner)
}

func (w *WebChat) verifyWebTransferTarget(ctx context.Context, target *webConnection) error {
	if target.login.Key == "" {
		return errors.New("선택한 계정의 로그인과 워크스페이스를 확인해 주세요.")
	}
	if err := w.verifyLogin(ctx, target); err != nil {
		return err
	}
	w.usageCache.mu.Lock()
	usage := w.usageCache.entries[target.login.Revision]
	w.usageCache.mu.Unlock()
	if time.Since(usage.checkedAt) < time.Minute &&
		(usage.limits.FiveHour != nil && *usage.limits.FiveHour == 0 || usage.limits.Week != nil && *usage.limits.Week == 0) {
		return errors.New("선택한 계정도 사용 한도에 도달했습니다. 잔여 한도가 있는 계정을 선택해 주세요.")
	}
	return nil
}

func (w *WebChat) releaseWebChat(ctx context.Context, source *webInputOwner, id string, interrupt bool, beforeInterrupt func(context.Context, string) error) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	archive := source.web == nil
	if archive {
		// Unsubscribe only affects the client making the request. A separate
		// official socket can release this single thread via archive instead.
		if err := verifyWebReleaseArchive(ctx, source.client, id); err != nil {
			return false, err
		}
	}
	args := map[string]any{"threadId": id}
	if err := w.settleWebTurn(ctx, source, id, interrupt, beforeInterrupt); err != nil {
		return false, err
	}
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	if !cr.WriterBusy(home, id) {
		return false, nil
	}
	if err := w.verifyInputOwner(ctx, source, id, false); err != nil {
		return false, err
	}
	if !archive {
		if err := callWebInput(ctx, source.client, "thread/unsubscribe", args, nil); err != nil {
			return false, err
		}
		wait, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := w.waitWebRelease(wait, source, id)
		cancel()
		if err == nil || !errors.Is(err, errWebWriterStillHeld) || !interrupt {
			return false, err
		}
	}
	if err := verifyWebReleaseArchive(ctx, source.client, id); err != nil {
		return false, err
	}
	if err := w.verifyInputOwner(ctx, source, id, false); err != nil {
		return false, err
	}
	if err := callWebInput(ctx, source.client, "thread/archive", args, nil); err != nil {
		return false, err
	}
	return true, w.waitWebRelease(ctx, source, id)
}

func (w *WebChat) settleWebTurn(ctx context.Context, source *webInputOwner, id string, interrupt bool, beforeInterrupt func(context.Context, string) error) error {
	stopped, ended := "", ""
	for {
		thread, err := source.client.Read(ctx, id, false)
		if err != nil || thread.ID != id {
			return errors.New("계정을 바꾸기 전에 현재 채팅 상태를 확인할 수 없습니다.")
		}
		switch thread.Status.Type {
		case "idle", "notLoaded", "systemError":
			return nil
		case "active":
			if !interrupt {
				return errWebAccountBusy
			}
		default:
			return errWebAccountBusy
		}
		turns, err := recentWebTurns(ctx, source.client, id, 1)
		if err != nil {
			return err
		}
		if stopped != "" && len(turns) == 1 && turns[0].ID != stopped {
			return errors.New("인계 도중 새 턴이 시작됐습니다. 현재 작업 상태를 확인해 주세요.")
		}
		if len(turns) == 1 && turns[0].Status == "inProgress" && turns[0].ID != "" {
			turn := turns[0].ID
			if stopped == "" {
				if err := w.verifyInputOwner(ctx, source, id, false); err != nil {
					return err
				}
				if beforeInterrupt != nil {
					if err := beforeInterrupt(ctx, turn); err != nil {
						return err
					}
				}
				if err := callWebInput(ctx, source.client, "turn/interrupt", map[string]any{"threadId": id, "turnId": turn}, nil); err != nil {
					return err
				}
				stopped = turn
			}
			ended = ""
		} else if len(turns) == 1 {
			switch turns[0].Status {
			case "completed", "failed", "interrupted":
				// Turn completion and the thread status notification can land
				// separately, including a quota error. Don't interrupt a
				// finished turn while waiting for the thread to catch up.
				if ended != "" && ended == turns[0].ID {
					return nil
				}
				ended = turns[0].ID
			}
		}
		select {
		case <-ctx.Done():
			return errWebAccountBusy
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func verifyWebReleaseArchive(ctx context.Context, client *cr.Client, id string) error {
	for _, archived := range []bool{false, true} {
		var descendants struct {
			Data       []json.RawMessage
			NextCursor *string
		}
		args := map[string]any{"ancestorThreadId": id, "archived": archived, "limit": 1,
			"sourceKinds": []string{}, "useStateDbOnly": true}
		if err := client.Call(ctx, "thread/list", args, &descendants); err != nil {
			return err
		}
		if len(descendants.Data) != 0 || descendants.NextCursor != nil && *descendants.NextCursor != "" {
			return errors.New("하위 채팅이 있는 대화는 다른 실행에서 자동으로 보관할 수 없습니다.")
		}
	}
	queue, err := readWebQueue(ctx, client, id)
	if err != nil {
		return err
	}
	if len(queue.Data) != 0 {
		return errors.New("기존 창의 Codex 대기 입력을 먼저 확인해 주세요.")
	}
	return nil
}

func (w *WebChat) waitWebRelease(ctx context.Context, source *webInputOwner, id string) error {
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	identity, err := cr.NativeProcessIdentity(source.pid)
	if err != nil || identity != source.incarnation {
		if !cr.WriterBusy(home, id) {
			return nil
		}
		return errWebOtherOwner
	}
	for cr.WriterBusy(home, id) {
		if ctx.Err() != nil {
			return errWebWriterStillHeld
		}
		pids, err := webHandoffWriters(ctx, home, id)
		if err != nil {
			if ctx.Err() != nil {
				return errWebWriterStillHeld
			}
			return err
		}
		if len(pids) != 0 && (len(pids) != 1 || pids[0] != source.pid) {
			return errWebOtherOwner
		}
		select {
		case <-ctx.Done():
			return errWebWriterStillHeld
		case <-time.After(125 * time.Millisecond):
		}
	}
	return nil
}
