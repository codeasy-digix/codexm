package nativecli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

// A native queue can be consumed by whichever account loads the chat. Keep
// web-only submissions private until the intended account can start them.
type webQueuedInput struct {
	ID       string     `json:"id"`
	Text     string     `json:"text"`
	Account  string     `json:"account"`
	Identity string     `json:"identity"`
	State    string     `json:"state"`
	Detail   string     `json:"detail,omitempty"`
	Skills   []webSkill `json:"skills,omitempty"`
	Goal     bool       `json:"goal,omitempty"`
}

const webQueueID = "web-"

func (w *WebChat) queuePath(id string) string {
	return filepath.Join(w.c.StateDir(), "web-chat", "queue", id+".json")
}

func (w *WebChat) readOwnedQueue(id string) ([]webQueuedInput, error) {
	b, err := os.ReadFile(w.queuePath(id))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var q []webQueuedInput
	if err := json.Unmarshal(b, &q); err != nil {
		return nil, errors.New("웹 대기열을 읽을 수 없습니다.")
	}
	return q, nil
}

func (w *WebChat) saveOwnedQueue(id string, q []webQueuedInput) error {
	path := w.queuePath(id)
	if len(q) == 0 {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	b, err := json.Marshal(q)
	if err != nil {
		return err
	}
	if len(b) > 1024*1024 {
		return errors.New("이 채팅의 웹 대기열이 가득 찼습니다.")
	}
	return config.AtomicWrite(path, b, 0600)
}

func queueLoginKey(c *webConnection) string {
	if c.login.Key != "" {
		return c.login.Key
	}
	return c.login.Revision
}

func (w *WebChat) enqueueWebInput(ctx context.Context, target *webConnection, r Request, p webParams) (any, error) {
	if err := w.verifyLogin(ctx, target); err != nil {
		return nil, err
	}
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	if entry, err := catalog.Lookup(ctx, home, p.Thread); err == nil {
		if entry.Archived {
			return nil, errors.New("보관된 채팅은 복원한 뒤 입력할 수 있습니다.")
		}
	} else if thread, readErr := target.client.Read(ctx, p.Thread, false); readErr != nil || thread.ID != p.Thread {
		return nil, errors.New("대기 입력을 추가할 채팅을 찾을 수 없습니다.")
	}
	q, err := w.readOwnedQueue(p.Thread)
	if err != nil {
		return nil, err
	}
	if len(q) >= 100 {
		return nil, errors.New("이 채팅의 웹 대기열이 가득 찼습니다.")
	}
	account := target.account
	if account == "default" {
		if linked := accounts.LinkedNativeDefault(w.c); linked != "" &&
			accounts.InspectIdentity(w.c, linked).Key == target.login.Key && target.login.Key != "" {
			account = linked
		}
	}
	queued := webQueuedInput{ID: webQueueID + r.Operation, Text: p.Text, Account: account,
		Identity: queueLoginKey(target), State: "pending", Skills: p.selectedSkills, Goal: p.Goal}
	q = append(q, queued)
	if err := w.saveOwnedQueue(p.Thread, q); err != nil {
		return nil, err
	}
	result := map[string]any{"thread_id": p.Thread, "mode": "queue", "queued_id": queued.ID, "status": "accepted",
		"account": queued.Account, "handoffError": ""}
	// The user's current submit can interrupt an old account. An automatic
	// dispatch after a browser/service disconnect never interrupts a turn.
	owner, err := w.acquireWebChat(ctx, target, p.Thread, webAcquire{
		prepare: true, interrupt: true, external: p.SwitchAccount, objective: goalObjective(p.Text, p.Goal),
	})
	if err == nil {
		defer owner.release()
		if p.SwitchAccount {
			if waitErr := w.pinQueueToAccount(p.Thread, q, owner.web); waitErr != nil {
				result["handoffError"] = cr.SafeText(waitErr.Error())
				return result, nil
			}
		}
		if len(q) == 1 || p.SwitchAccount {
			_ = w.startQueuedInput(ctx, owner, p.Thread, q)
		}
	} else {
		if errors.Is(err, errWebInputUnconfirmed) {
			q[len(q)-1].State = "blocked"
		}
		_ = w.noteQueueWait(p.Thread, q, len(q)-1, err)
		result["handoffError"] = q[len(q)-1].Detail
	}
	return result, nil
}

// An explicit change must not jump back to the depleted login when an earlier
// queue entry reaches the front. Each entry stays bound to its original login.
func (w *WebChat) pinQueueToAccount(id string, q []webQueuedInput, target *webConnection) error {
	key := queueLoginKey(target)
	changed := false
	for i := range q {
		entry := &q[i]
		switch {
		case entry.State == "pending" && entry.Identity != key:
			entry.State, entry.Detail = "held", "다른 계정으로 전환하여 보류했습니다. 큐에서 꺼내 다시 보낼 수 있습니다."
			changed = true
		case entry.State == "held" && entry.Identity == key:
			entry.State, entry.Detail = "pending", ""
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return w.saveOwnedQueue(id, q)
}

func nextWebQueuedInput(q []webQueuedInput) int {
	return slices.IndexFunc(q, func(input webQueuedInput) bool { return input.State != "held" })
}

func (w *WebChat) noteQueueWait(id string, q []webQueuedInput, index int, err error) error {
	detail := ""
	if err != nil && !errors.Is(err, errWebAccountBusy) {
		detail = cr.SafeText(err.Error())
	}
	if q[index].Detail == detail {
		return nil
	}
	q[index].Detail = detail
	return w.saveOwnedQueue(id, q)
}

func (w *WebChat) dispatchQueued(ctx context.Context) error {
	if !w.webOnly {
		return nil
	}
	entries, err := os.ReadDir(filepath.Dir(w.queuePath("")))
	if os.IsNotExist(err) || (err == nil && len(entries) == 0) {
		return nil
	}
	if err != nil {
		return err
	}
	// One chat per pass keeps an unavailable account from monopolizing stdio.
	i := len(entries)
	for index, entry := range entries {
		if entry.Name() > w.queueCursor {
			i = index
			break
		}
	}
	for offset := range len(entries) {
		file := entries[(i+offset)%len(entries)]
		id := strings.TrimSuffix(file.Name(), ".json")
		if file.IsDir() || id == file.Name() || !threadID.MatchString(id) {
			continue
		}
		w.queueCursor = file.Name()
		ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		return w.advanceOwnedQueue(ctx, id)
	}
	return nil
}

func (w *WebChat) advanceOwnedQueue(ctx context.Context, id string) error {
	unlock, err := lockChatMutation(ctx, w.c, id)
	if err != nil {
		return err
	}
	defer unlock()
	q, err := w.readOwnedQueue(id)
	index := nextWebQueuedInput(q)
	if err != nil || index < 0 || q[index].State != "pending" {
		return err
	}
	next := q[index]
	entry, err := catalog.Lookup(ctx, config.Expand(w.c.Runtime.SharedCodexHome), id)
	if err == nil && entry.Archived {
		return w.noteQueueWait(id, q, index, errors.New("채팅이 보관돼 있습니다. 복원하면 대기 입력을 이어갑니다."))
	}
	target, err := w.connection(ctx, next.Account)
	if err != nil {
		return w.noteQueueWait(id, q, index, err)
	}
	defer w.releaseConnection(target)
	if next.Identity != queueLoginKey(target) {
		return w.noteQueueWait(id, q, index, errors.New("등록한 로그인과 현재 로그인이 다릅니다. 대기 입력을 다시 지정해 주세요."))
	}
	if err := w.verifyLogin(ctx, target); err != nil {
		return w.noteQueueWait(id, q, index, err)
	}
	owner, err := w.acquireWebChat(ctx, target, id, webAcquire{prepare: true, objective: goalObjective(next.Text, next.Goal)})
	if err != nil {
		return w.noteQueueWait(id, q, index, err)
	}
	defer owner.release()
	return w.startQueuedInput(ctx, owner, id, q)
}

func (w *WebChat) startQueuedInput(ctx context.Context, owner *webInputOwner, id string, q []webQueuedInput) error {
	index := nextWebQueuedInput(q)
	if index < 0 {
		return nil
	}
	next := &q[index]
	if next.State != "pending" || next.Identity != queueLoginKey(owner.web) {
		return nil
	}
	delivery, err := owner.delivery(ctx, id)
	if err != nil {
		return w.noteQueueWait(id, q, index, err)
	}
	if delivery.State != "idle" {
		return w.noteQueueWait(id, q, index, nil)
	}
	if err := w.verifyLogin(ctx, owner.web); err != nil {
		return w.noteQueueWait(id, q, index, err)
	}
	// Previously queued native items stay in their original queue. If the
	// interruption left that queue idle, finish its earlier head first.
	native, err := readWebQueue(ctx, owner.client, id)
	if err != nil {
		return w.noteQueueWait(id, q, index, err)
	}
	if len(native.Data) > 0 {
		return w.startEarlierNativeInput(ctx, owner, id, q, index, native.Data[0])
	}
	if err := w.verifyInputOwner(ctx, owner, id, true); err != nil {
		return w.noteQueueWait(id, q, index, err)
	}
	p := webParams{Thread: id, Text: next.Text, Goal: next.Goal, Mode: "immediate"}
	for _, skill := range next.Skills {
		p.SkillIDs = append(p.SkillIDs, skill.ID)
	}
	if err := w.preparePrompt(ctx, owner.web, &p); err != nil {
		return w.noteQueueWait(id, q, index, err)
	}
	next.State, next.Detail = "sending", ""
	if err := w.saveOwnedQueue(id, q); err != nil {
		return err
	}
	r := Request{Operation: strings.TrimPrefix(next.ID, webQueueID)}
	if p.Goal {
		_, err = w.deliverGoal(ctx, owner, owner.web, r, p)
	} else {
		_, err = w.startWebTurn(ctx, owner, owner.web, r, p)
	}
	if err != nil {
		if errors.Is(err, errWebInputUnconfirmed) {
			next.Detail = "전송 결과를 확인하지 못했습니다. 채팅을 확인해 주세요."
		} else {
			next.State, next.Detail = "blocked", cr.SafeText(err.Error())
		}
		return w.saveOwnedQueue(id, q)
	}
	return w.saveOwnedQueue(id, slices.Delete(q, index, index+1))
}

func (w *WebChat) startEarlierNativeInput(ctx context.Context, owner *webInputOwner, id string, q []webQueuedInput, index int, first json.RawMessage) error {
	var entry struct{ ID string }
	if json.Unmarshal(first, &entry) != nil || entry.ID == "" {
		return w.noteQueueWait(id, q, index, errors.New("기존 대기 입력을 확인할 수 없습니다."))
	}
	if err := w.verifyInputOwner(ctx, owner, id, true); err != nil {
		return w.noteQueueWait(id, q, index, err)
	}
	// Persist the attempt before touching the native queue. A lost result
	// leaves the native input and the new web input available for inspection.
	const checking = "기존 대기 입력의 시작 결과를 확인해 주세요."
	q[index].State, q[index].Detail = "blocked", checking
	if err := w.saveOwnedQueue(id, q); err != nil {
		return err
	}
	var reply struct{ Turn cr.Turn }
	err := callWebInput(ctx, owner.client, "thread/queue/start", map[string]any{"threadId": id, "queuedSubmissionId": entry.ID}, &reply)
	if err != nil || reply.Turn.ID == "" {
		return nil
	}
	q[index].State, q[index].Detail = "pending", ""
	return w.saveOwnedQueue(id, q)
}

func (w *WebChat) ownedQueueDisplay(id string) ([]json.RawMessage, error) {
	q, err := w.readOwnedQueue(id)
	if err != nil {
		return nil, err
	}
	items := make([]json.RawMessage, 0, len(q))
	for _, input := range q {
		b, err := json.Marshal(map[string]any{"id": input.ID, "input": cr.TextInput(input.Text), "account": input.Account,
			"state": input.State, "detail": input.Detail, "skills": input.Skills, "goal": input.Goal})
		if err != nil {
			return nil, err
		}
		items = append(items, b)
	}
	return items, nil
}

func (w *WebChat) changeOwnedQueue(method string, p webParams) (any, error) {
	q, err := w.readOwnedQueue(p.Thread)
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(q, func(input webQueuedInput) bool { return input.ID == p.Queued })
	if index == -1 {
		return nil, errWebQueueGone
	}
	if p.ExpectedText == nil || q[index].Text != *p.ExpectedText {
		return nil, errWebQueueChanged
	}
	result := map[string]any{"queued_id": p.Queued}
	if method == "web.queueCancel" {
		result["deleted"], result["check_history"] = true, q[index].State == "sending"
		q = slices.Delete(q, index, index+1)
	} else {
		if q[index].State == "sending" {
			return nil, errWebQueueUncertain
		}
		p.Goal = q[index].Goal
		if err := normalizeWebPrompt(&p); err != nil {
			return nil, err
		}
		q[index].Text, q[index].Goal = p.Text, p.Goal
		if q[index].State != "held" {
			q[index].State, q[index].Detail = "pending", ""
		}
		result["updated"] = true
	}
	return result, w.saveOwnedQueue(p.Thread, q)
}
