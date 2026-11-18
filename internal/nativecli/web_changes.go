package nativecli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func webDirectory(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("select an absolute directory on this machine")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(resolved)
	if err != nil || !st.IsDir() {
		return "", errors.New("project directory is unavailable on this machine")
	}
	return resolved, nil
}

func (w *WebChat) change(ctx context.Context, c *webConnection, r Request, p webParams) (any, error) {
	if p.Project != "" && (r.Method == "web.create" || r.Method == "web.move") {
		d, err := catalog.ReadDesktop(ctx, config.Expand(w.c.Runtime.SharedCodexHome))
		if err != nil {
			return nil, err
		}
		p.Project, err = d.NativeProject(p.Project)
		if err != nil {
			return nil, err
		}
	}
	if r.Method == "web.projectCreate" {
		cwd, err := webDirectory(p.Cwd)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(p.Name)
		if name == "" {
			name = filepath.Base(cwd)
		}
		var result json.RawMessage
		err = c.client.Call(ctx, "project/create", map[string]any{"name": name, "roots": []map[string]string{{"path": cwd}}, "idempotencyKey": r.Operation}, &result)
		return result, err
	}
	if r.Method == "web.create" {
		if w.loginChanged(c) {
			return nil, errWebLoginDraining
		}
		cwd, err := webDirectory(p.Cwd)
		if err != nil {
			return nil, err
		}
		args := map[string]any{"cwd": cwd, "historyMode": "paginated"}
		if p.Project != "" {
			args["projectId"] = p.Project
		}
		var result struct {
			Thread WebThread `json:"thread"`
			Model  string    `json:"model"`
			Effort string    `json:"reasoningEffort"`
		}
		if err := c.client.Call(ctx, "thread/start", args, &result); err != nil {
			return nil, err
		}
		w.rememberAccount(result.Thread.ID, c.account)
		result.Thread.Account = c.account
		if result.Thread.Model == "" {
			result.Thread.Model = result.Model
		}
		if result.Thread.Effort == "" {
			result.Thread.Effort = result.Effort
		}
		return map[string]any{"thread": result.Thread, "account": c.account}, nil
	}
	if p.Thread == "" {
		return nil, errors.New("thread UUID required")
	}
	unlock, err := lockChatMutation(ctx, w.c, p.Thread)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if r.Method == "web.pin" {
		d, err := catalog.ReadDesktop(ctx, config.Expand(w.c.Runtime.SharedCodexHome))
		if err != nil {
			return nil, err
		}
		var section any
		if p.Pinned {
			if d.PinnedSection == "" {
				return nil, errors.New("native Pinned section is unavailable")
			}
			section = d.PinnedSection
		}
		if err := c.client.Call(ctx, "thread/section/move", map[string]any{"threadId": p.Thread, "sectionId": section}, nil); err != nil {
			return nil, err
		}
		return map[string]any{"thread_id": p.Thread, "pinned": p.Pinned}, nil
	}
	if r.Method == "web.send" {
		return w.send(ctx, c, r, p)
	}
	if err := w.verifyOwner(ctx, c, p.Thread, false); err != nil {
		return nil, err
	}
	if r.Method == "web.archive" || r.Method == "web.delete" {
		if err := w.verifyTree(ctx, c, p.Thread); err != nil {
			return nil, err
		}
	}
	var result json.RawMessage
	switch r.Method {
	case "web.move":
		if err := c.client.Call(ctx, "thread/metadata/update", map[string]any{"threadId": p.Thread, "projectId": p.Project}, &result); err != nil {
			return nil, err
		}
		return map[string]any{"thread_id": p.Thread, "project_id": p.Project}, nil
	case "web.archive":
		method := "thread/unarchive"
		if p.Archived {
			method = "thread/archive"
		}
		if err := c.client.Call(ctx, method, map[string]any{"threadId": p.Thread}, nil); err != nil {
			return nil, err
		}
		return map[string]any{"thread_id": p.Thread, "archived": p.Archived}, nil
	case "web.delete":
		if err := c.client.Call(ctx, "thread/delete", map[string]any{"threadId": p.Thread}, nil); err != nil {
			return nil, err
		}
		return map[string]any{"thread_id": p.Thread, "deleted": true}, nil
	case "web.interrupt":
		if p.Turn == "" {
			return nil, errors.New("active turn ID required")
		}
		if err := w.verifyOwner(ctx, c, p.Thread, true); err != nil {
			return nil, err
		}
		if err := c.client.Call(ctx, "turn/interrupt", map[string]any{"threadId": p.Thread, "turnId": p.Turn}, nil); err != nil {
			return nil, err
		}
		return map[string]any{"thread_id": p.Thread, "interrupted": true}, nil
	}
	return nil, errors.New("unsupported web mutation")
}

// Native archive/delete includes descendants. Confirm none belongs to another
// GUI/CLI before an explicitly confirmed tree mutation.
func (w *WebChat) verifyTree(ctx context.Context, c *webConnection, id string) error {
	for _, archived := range []bool{false, true} {
		cursor := ""
		for {
			var page struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
				NextCursor *string `json:"nextCursor"`
			}
			args := map[string]any{"ancestorThreadId": id, "archived": archived, "limit": 100, "sourceKinds": []string{}, "useStateDbOnly": true}
			if cursor != "" {
				args["cursor"] = cursor
			}
			if err := c.client.Call(ctx, "thread/list", args, &page); err != nil {
				return err
			}
			for _, child := range page.Data {
				if err := w.verifyOwner(ctx, c, child.ID, false); err != nil {
					return errors.New("a child chat is owned by another GUI/CLI; release that work before archiving or deleting this chat")
				}
			}
			if page.NextCursor == nil || *page.NextCursor == "" {
				break
			}
			if *page.NextCursor == cursor {
				return errors.New("descendant cursor did not advance")
			}
			cursor = *page.NextCursor
		}
	}
	return nil
}

func (w *WebChat) send(ctx context.Context, c *webConnection, r Request, p webParams) (any, error) {
	if strings.TrimSpace(p.Text) == "" {
		return nil, errors.New("enter a message")
	}
	if p.Mode != "immediate" && p.Mode != "queue" {
		return nil, errors.New("choose immediate or queue delivery")
	}
	if p.Model != "" || p.Effort != "" {
		if p.Mode != "immediate" || p.Turn != "" {
			return nil, errors.New("모델과 effort는 새 턴을 시작할 때 선택할 수 있습니다.")
		}
		if p.Model == "" {
			return nil, errors.New("effort를 적용할 모델을 선택해 주세요.")
		}
	}
	owners, err := writerProcesses(ctx, config.Expand(w.c.Runtime.SharedCodexHome), p.Thread)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(owners) > 1 {
		return nil, errors.New("채팅을 실행하는 프로세스를 하나로 확인할 수 없습니다.")
	}
	if len(owners) == 1 && (owners[0] != c.process.PID || p.Turn != "" || p.Mode == "queue") {
		return w.sendToWriter(ctx, c, r, p, owners[0])
	}
	if p.Turn != "" {
		return nil, errors.New("이전 턴이 끝났습니다. 채팅 상태를 확인한 뒤 다시 보내 주세요.")
	}
	if err := w.verifyLogin(ctx, c); err != nil {
		return nil, err
	}
	if p.Model != "" {
		if err := c.validateTurnModel(ctx, p.Model, p.Effort); err != nil {
			return nil, err
		}
	}
	if err := w.prepare(ctx, c, p.Thread); err != nil {
		return nil, err
	}
	if w.loginChanged(c) {
		return nil, errWebLoginDraining
	}
	if p.Mode == "queue" {
		return queueWebInput(ctx, c.client, r, p)
	}
	current, err := c.client.Read(ctx, p.Thread, false)
	if err != nil {
		return nil, err
	}
	if current.Status.Type == "active" {
		return nil, errors.New("채팅이 이미 실행 중입니다. 현재 턴을 확인한 뒤 다시 보내 주세요.")
	}
	args := map[string]any{"threadId": p.Thread, "clientUserMessageId": r.Operation, "input": cr.TextInput(p.Text)}
	if p.Model != "" {
		args["model"] = p.Model
		if p.Effort != "" {
			args["effort"] = p.Effort
		}
	}
	var reply struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := callWebInput(ctx, c.client, "turn/start", args, &reply); err != nil {
		return nil, err
	}
	if reply.Turn.ID == "" {
		return nil, errWebInputUnconfirmed
	}
	w.rememberAccount(p.Thread, c.account)
	return map[string]any{"thread_id": p.Thread, "turn_id": reply.Turn.ID, "mode": "start", "status": "accepted", "accepted_at": time.Now().Unix()}, nil
}

func (w *WebChat) sendToWriter(ctx context.Context, c *webConnection, r Request, p webParams, pid int) (any, error) {
	if p.Mode == "immediate" && p.Turn == "" {
		return nil, errWebForeignInput
	}
	probe, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	owner, err := w.openInputOwner(probe, p.Thread, pid)
	if err != nil {
		if p.Mode == "queue" && pid != c.process.PID {
			// The durable native queue can accept input from a different
			// connection. It does not load or take over the original chat.
			return queueWebInput(ctx, c.client, r, p)
		}
		return nil, errWebForeignInput
	}
	defer owner.release()
	if owner.web != nil {
		if err := owner.web.verifyExecutionLogin(ctx); err != nil {
			return nil, err
		}
	}
	if p.Mode == "immediate" || (owner.web != nil && w.loginChanged(owner.web)) {
		delivery, err := owner.delivery(probe, p.Thread)
		if err != nil {
			return nil, errors.New("현재 턴의 상태를 읽을 수 없습니다. 입력은 전송되지 않았습니다.")
		}
		if p.Mode == "immediate" {
			if delivery.State == "waiting" {
				return nil, errors.New("현재 턴이 승인 또는 사용자 입력을 기다립니다. 원래 창에서 응답해 주세요.")
			}
			if delivery.State != "ready" || delivery.TurnID != p.Turn {
				return nil, errors.New("개입하려던 턴이 바뀌었습니다. 현재 턴을 확인해 주세요.")
			}
		} else if delivery.State == "idle" {
			return nil, errWebLoginDraining
		}
	}
	if err := w.verifyInputOwner(probe, owner, p.Thread); err != nil {
		return nil, err
	}
	if p.Mode == "queue" {
		return queueWebInput(ctx, owner.client, r, p)
	}
	var reply struct{ TurnID string }
	args := map[string]any{"threadId": p.Thread, "expectedTurnId": p.Turn, "clientUserMessageId": r.Operation, "input": cr.TextInput(p.Text)}
	if err := callWebInput(ctx, owner.client, "turn/steer", args, &reply); err != nil {
		return nil, err
	}
	if reply.TurnID != p.Turn {
		return nil, errWebInputUnconfirmed
	}
	return map[string]any{"thread_id": p.Thread, "turn_id": p.Turn, "mode": "steer", "status": "accepted", "accepted_at": time.Now().Unix()}, nil
}

var errWebInputUnconfirmed = errors.New("전송 결과를 확인하지 못했습니다. 대화와 큐에서 원래 입력을 확인해 주세요.")

func callWebInput(ctx context.Context, client *cr.Client, method string, args, reply any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := client.Call(ctx, method, args, reply); err != nil {
		var rpc *cr.RPCError
		if !errors.As(err, &rpc) {
			return errWebInputUnconfirmed
		}
		return rpc
	}
	return nil
}

func queueWebInput(ctx context.Context, client *cr.Client, r Request, p webParams) (any, error) {
	var reply struct {
		Queued struct{ ID string } `json:"queuedSubmission"`
	}
	args := map[string]any{"threadId": p.Thread, "clientUserMessageId": r.Operation, "input": cr.TextInput(p.Text)}
	if err := callWebInput(ctx, client, "thread/queue/add", args, &reply); err != nil {
		return nil, err
	}
	if reply.Queued.ID == "" {
		return nil, errWebInputUnconfirmed
	}
	return map[string]any{"thread_id": p.Thread, "mode": "queue", "queued_id": reply.Queued.ID, "status": "accepted"}, nil
}
