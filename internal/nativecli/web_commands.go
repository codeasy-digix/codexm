package nativecli

import (
	"context"
	"database/sql"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func (c *webConnection) inPlanMode(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.planMode[id]
}

func (c *webConnection) setPlanMode(id string, planning bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !planning {
		delete(c.planMode, id)
		return
	}
	if c.planMode == nil {
		c.planMode = make(map[string]bool)
	}
	c.planMode[id] = true
}

// An official collaboration-mode override includes the effective model and
// effort. Read those settings only when entering or leaving the plan mode.
func webTurnMode(ctx context.Context, client *cr.Client, id string, args map[string]any, plan bool) (map[string]any, error) {
	var current struct{ Thread WebThread }
	if err := client.Call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &current); err != nil {
		return nil, err
	}
	if current.Thread.ID != id {
		return nil, errors.New("현재 채팅의 실행 설정을 확인할 수 없습니다.")
	}
	model, effort := current.Thread.Model, current.Thread.Effort
	if chosen, ok := args["model"].(string); ok && chosen != model {
		model, effort = chosen, ""
	}
	if chosen, ok := args["effort"].(string); ok {
		effort = chosen
	}
	if model == "" {
		return nil, errors.New("계획 모드에 사용할 모델을 확인할 수 없습니다. 모델을 선택한 뒤 다시 시도해 주세요.")
	}
	mode := "default"
	if plan {
		mode = "plan"
	}
	settings := map[string]any{"model": model, "developer_instructions": nil, "reasoning_effort": nil}
	if effort != "" {
		settings["reasoning_effort"] = effort
	}
	return map[string]any{"mode": mode, "settings": settings}, nil
}

func (w *WebChat) renameWebChat(ctx context.Context, selected *webConnection, p webParams) (any, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" || len(name) > 200 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return nil, errors.New("채팅 이름을 한 줄로 입력해 주세요.")
	}
	thread, err := w.thread(ctx, p.Thread)
	if err != nil {
		return nil, err
	}
	if thread == nil {
		return nil, errors.New("이름을 바꿀 채팅을 찾을 수 없습니다.")
	}
	// A draft is only loaded on its creating account. Persisted titles can be
	// set using the native metadata API without taking over the chat's writer.
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	if _, err := catalog.Lookup(ctx, home, p.Thread); errors.Is(err, sql.ErrNoRows) && thread.Account != selected.account {
		selected, err = w.connection(ctx, thread.Account)
		if err != nil {
			return nil, err
		}
		defer w.releaseConnection(selected)
	}
	args := map[string]any{"threadId": p.Thread, "name": name}
	if err := callWebInput(ctx, selected.client, "thread/name/set", args, nil); err != nil {
		return nil, err
	}
	if after, err := selected.client.Read(ctx, p.Thread, false); err != nil || after.ID != p.Thread || after.Name != name {
		return nil, errWebInputUnconfirmed
	}
	return map[string]any{"thread_id": p.Thread, "name": name}, nil
}

func (w *WebChat) forkWebChat(ctx context.Context, c *webConnection, id string) (any, error) {
	if strings.EqualFold(id, w.c.Runtime.WebGUIRelay) {
		return nil, errors.New("복제할 채팅을 찾을 수 없습니다.")
	}
	source, err := w.thread(ctx, id)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, errors.New("복제할 채팅을 찾을 수 없습니다.")
	}
	if w.loginChanged(c) {
		return nil, errWebLoginDraining
	}
	// Fork reads the saved source history and creates a new native thread. It
	// needs neither the source writer nor a turn/start call.
	var result struct {
		Thread struct {
			WebThread
			ForkedFrom string `json:"forkedFromId"`
		} `json:"thread"`
		Model  string `json:"model"`
		Effort string `json:"reasoningEffort"`
	}
	args := map[string]any{"threadId": id, "excludeTurns": true}
	if err := callWebInput(ctx, c.client, "thread/fork", args, &result); err != nil {
		return nil, err
	}
	thread := result.Thread.WebThread
	if !threadID.MatchString(thread.ID) || strings.EqualFold(thread.ID, id) ||
		(result.Thread.ForkedFrom != "" && result.Thread.ForkedFrom != id) {
		return nil, errWebInputUnconfirmed
	}
	w.rememberAccount(thread.ID, c.account)
	thread.Account, thread.Project = c.account, source.Project
	if thread.Model == "" {
		thread.Model = result.Model
	}
	if thread.Effort == "" {
		thread.Effort = result.Effort
	}
	return map[string]any{"thread": thread, "status": "accepted"}, nil
}

func webReviewTarget(ctx context.Context, cwd, branch string) (map[string]string, error) {
	if !filepath.IsAbs(cwd) {
		return nil, errors.New("코드 리뷰를 실행할 작업 폴더를 확인할 수 없습니다.")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	git := func(args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = cwd
		return cmd
	}
	inside, err := git("rev-parse", "--is-inside-work-tree").Output()
	if err != nil || strings.TrimSpace(string(inside)) != "true" {
		return nil, errors.New("코드 리뷰는 Git 작업 폴더에서 사용할 수 있습니다.")
	}
	if branch == "" {
		return map[string]string{"type": "uncommittedChanges"}, nil
	}
	if len(branch) > 200 || strings.HasPrefix(branch, "-") || git("check-ref-format", "refs/heads/"+branch).Run() != nil ||
		git("rev-parse", "--verify", "--end-of-options", branch+"^{commit}").Run() != nil {
		return nil, errors.New("이 작업 폴더에 있는 기준 브랜치 이름을 입력해 주세요.")
	}
	return map[string]string{"type": "baseBranch", "branch": branch}, nil
}

func (w *WebChat) runWebCommand(ctx context.Context, selected *webConnection, method string, p webParams) (any, error) {
	thread, err := w.thread(ctx, p.Thread)
	if err != nil {
		return nil, err
	}
	if thread == nil || thread.Archived || strings.EqualFold(p.Thread, w.c.Runtime.WebGUIRelay) {
		return nil, errors.New("이 명령을 실행할 활성 채팅을 찾을 수 없습니다.")
	}
	q, err := w.readOwnedQueue(p.Thread)
	if err != nil {
		return nil, err
	}
	if nextWebQueuedInput(q) >= 0 {
		return nil, errors.New("앞서 등록한 대기 입력을 먼저 확인해 주세요.")
	}
	args := map[string]any{"threadId": p.Thread}
	if method == "web.review" {
		target, err := webReviewTarget(ctx, thread.Cwd, strings.TrimSpace(p.Branch))
		if err != nil {
			return nil, err
		}
		args["target"], args["delivery"] = target, "inline"
	}
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	pids, err := webHandoffWriters(ctx, home, p.Thread)
	if err != nil || len(pids) > 1 {
		return nil, errWebOtherOwner
	}
	pid := selected.process.PID
	if len(pids) == 0 {
		if err := w.verifyLogin(ctx, selected); err != nil {
			return nil, err
		}
		if err := w.prepare(ctx, selected, p.Thread); err != nil {
			return nil, err
		}
	} else {
		pid = pids[0]
	}
	owner, err := w.openInputOwnerFor(ctx, p.Thread, pid, false)
	if err != nil {
		return nil, errWebOtherOwner
	}
	defer owner.release()
	if !sameWebLogin(owner.web, selected) {
		return nil, errors.New("현재 채팅을 실행하는 계정을 선택해 주세요.")
	}
	if err := w.verifyLogin(ctx, owner.web); err != nil {
		return nil, err
	}
	if delivery, err := owner.delivery(ctx, p.Thread); err != nil || delivery.State != "idle" {
		return nil, errWebAccountBusy
	}
	queue, err := readWebQueue(ctx, owner.client, p.Thread)
	if err != nil {
		return nil, err
	}
	if len(queue.Data) != 0 {
		return nil, errors.New("앞서 등록한 대기 입력을 먼저 확인해 주세요.")
	}
	if err := w.verifyInputOwner(ctx, owner, p.Thread, owner.web == selected); err != nil {
		return nil, err
	}
	result := map[string]any{"thread_id": p.Thread, "status": "accepted"}
	if method == "web.compact" {
		if err := callWebInput(ctx, owner.client, "thread/compact/start", args, nil); err != nil {
			return nil, err
		}
		return result, nil
	}
	var reply struct {
		ReviewThreadID string
		Turn           cr.Turn
	}
	if err := callWebInput(ctx, owner.client, "review/start", args, &reply); err != nil {
		return nil, err
	}
	if reply.ReviewThreadID != p.Thread || reply.Turn.ID == "" {
		return nil, errWebInputUnconfirmed
	}
	result["turn_id"] = reply.Turn.ID
	return result, nil
}
