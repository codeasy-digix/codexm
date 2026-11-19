package nativecli

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"time"

	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

var errWebAccountBusy = errors.New("이 채팅의 현재 작업이 끝나기를 기다리고 있습니다.")
var errWebOtherOwner = errors.New("다른 앱이 이 채팅을 열고 있습니다. 원래 창에서 채팅을 닫으면 웹에서 이어갈 수 있습니다.")

func sameWebLogin(a, b *webConnection) bool {
	return a == b || (a.login.Key != "" && a.login.Key == b.login.Key)
}

func (w *WebChat) interruptWebThread(ctx context.Context, p webParams) (any, error) {
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
	if err := owner.client.Call(ctx, "turn/interrupt", map[string]any{"threadId": p.Thread, "turnId": p.Turn}, nil); err != nil {
		return nil, err
	}
	return map[string]any{"thread_id": p.Thread, "interrupted": true}, nil
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
	if p.Mode == "queue" {
		return w.enqueueWebInput(ctx, target, r, p)
	}
	owner, err := w.acquireWebChat(ctx, target, p.Thread, automatic, automatic)
	if err != nil {
		return nil, err
	}
	defer owner.release()
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

// The selected thread can be released without stopping a shared app-server.
// Only a web-owned stdio connection can be used for this operation.
func (w *WebChat) acquireWebChat(ctx context.Context, target *webConnection, id string, prepare, interrupt bool) (*webInputOwner, error) {
	pids, err := webWriterPIDs(ctx, config.Expand(w.c.Runtime.SharedCodexHome), id)
	if err != nil {
		return nil, err
	}
	switch len(pids) {
	case 1:
		source, err := w.openInputOwner(ctx, id, pids[0])
		if err != nil {
			return nil, errWebOtherOwner
		}
		same := sameWebLogin(source.web, target)
		if same && (source.web == target || !w.loginChanged(source.web)) {
			return source, nil
		}
		defer source.release()
		if !prepare {
			return nil, errWebNoActiveTurn
		}
		if target.login.Key == "" {
			return nil, errors.New("선택한 계정의 로그인과 워크스페이스를 확인한 뒤 다시 시도해 주세요.")
		}
		if err := w.verifyLogin(ctx, target); err != nil {
			return nil, err
		}
		if err := w.releaseWebChat(ctx, source, id, interrupt && !same); err != nil {
			return nil, err
		}
	case 0:
		if !prepare {
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

func (w *WebChat) releaseWebChat(ctx context.Context, source *webInputOwner, id string, interrupt bool) error {
	ctx, cancel := context.WithTimeout(ctx, 9*time.Second)
	defer cancel()
	if err := source.web.verifyExecutionLogin(ctx); err != nil {
		return err
	}
	thread, err := source.client.Read(ctx, id, false)
	if err != nil || thread.ID != id {
		return errors.New("계정을 바꾸기 전에 현재 채팅 상태를 확인할 수 없습니다.")
	}
	args := map[string]any{"threadId": id}
	if thread.Status.Type == "active" {
		if !interrupt {
			return errWebAccountBusy
		}
		var page struct{ Data []cr.Turn }
		if err := source.client.Call(ctx, "thread/turns/list",
			map[string]any{"threadId": id, "limit": 1, "sortDirection": "desc", "itemsView": "notLoaded"}, &page); err != nil {
			return err
		}
		if len(page.Data) != 1 || page.Data[0].Status != "inProgress" || page.Data[0].ID == "" {
			return errors.New("중단할 현재 작업을 확인할 수 없습니다. 채팅 상태를 새로 확인해 주세요.")
		}
		if err := w.verifyInputOwner(ctx, source, id, false); err != nil {
			return err
		}
		if err := source.client.Call(ctx, "turn/interrupt", map[string]any{"threadId": id, "turnId": page.Data[0].ID}, nil); err != nil {
			return err
		}
	} else if thread.Status.Type != "idle" {
		return errors.New("계정을 바꿀 수 있는 채팅 상태를 확인하지 못했습니다.")
	}
	if err := w.verifyInputOwner(ctx, source, id, false); err != nil {
		return err
	}
	if err := source.client.Call(ctx, "thread/unsubscribe", args, nil); err != nil {
		return err
	}
	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()
	for {
		pids, err := webWriterPIDs(ctx, config.Expand(w.c.Runtime.SharedCodexHome), id)
		if err != nil {
			return err
		}
		if len(pids) > 1 || (len(pids) == 1 && pids[0] != source.pid) {
			return errWebOtherOwner
		}
		if len(pids) == 0 {
			loaded, err := webLoadedThreads(ctx, source.client)
			if err != nil {
				return err
			}
			if _, found := loaded[id]; !found {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return errors.New("기존 계정이 채팅을 아직 놓지 않았습니다. 채팅 상태를 다시 확인해 주세요.")
		case <-ticker.C:
		}
	}
}
