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
	if w.c.Runtime.WebGUIRelay != "" && strings.EqualFold(p.Thread, w.c.Runtime.WebGUIRelay) {
		return nil, errors.New("이 채팅은 GUI 메시지 중계 전용입니다.")
	}
	if strings.TrimSpace(p.Text) == "" {
		return nil, errors.New("enter a message")
	}
	if p.Mode != "immediate" && p.Mode != "steer" && p.Mode != "queue" {
		return nil, errors.New("choose immediate or queue delivery")
	}
	if p.Model != "" || p.Effort != "" {
		if p.Mode != "immediate" || p.Turn != "" {
			return nil, errors.New("모델과 effort는 새 턴을 시작할 때 선택할 수 있습니다.")
		}
	}
	owners, err := webWriterPIDs(ctx, config.Expand(w.c.Runtime.SharedCodexHome), p.Thread)
	if p.Mode == "queue" {
		// The official queue accepts input independently of the owner's turn or
		// availability. Keep opening an unlocked chat when its web login is ready.
		if err == nil && len(owners) == 0 && w.verifyLogin(ctx, c) == nil {
			_ = w.prepare(ctx, c, p.Thread)
		}
		return queueWebInput(ctx, c.client, r, p)
	}
	if err != nil {
		return nil, err
	}
	if len(owners) > 1 {
		return nil, errors.New("채팅을 실행하는 프로세스를 하나로 확인할 수 없습니다.")
	}
	if len(owners) == 1 {
		return w.sendToWriter(ctx, c, r, p, owners[0])
	}
	if p.Mode == "steer" || p.Turn != "" {
		return nil, errWebNoActiveTurn
	}
	if err := w.verifyLogin(ctx, c); err != nil {
		return nil, err
	}
	if err := w.prepare(ctx, c, p.Thread); err != nil {
		return nil, err
	}
	return w.sendToWriter(ctx, c, r, p, c.process.PID)
}

func (w *WebChat) startWebTurn(ctx context.Context, owner *webInputOwner, selected *webConnection, r Request, p webParams) (any, error) {
	args := map[string]any{"threadId": p.Thread, "clientUserMessageId": r.Operation, "input": cr.TextInput(p.Text)}
	if owner.web == selected && (p.Model != "" || p.Effort != "") {
		if p.Model == "" {
			return nil, errors.New("effort를 적용할 모델을 선택해 주세요.")
		}
		if err := selected.validateTurnModel(ctx, p.Model, p.Effort); err != nil {
			return nil, err
		}
		args["model"] = p.Model
		if p.Effort != "" {
			args["effort"] = p.Effort
		}
	}
	// A draft loaded on the selected connection may not have a writer file yet.
	if err := w.verifyInputOwner(ctx, owner, p.Thread, owner.web == selected); err != nil {
		return nil, err
	}
	if owner.web != nil && w.loginChanged(owner.web) {
		return nil, errWebLoginDraining
	}
	var reply struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := callWebInput(ctx, owner.client, "turn/start", args, &reply); err != nil {
		return nil, err
	}
	if reply.Turn.ID == "" {
		return nil, errWebInputUnconfirmed
	}
	if owner.web != nil {
		w.rememberAccount(p.Thread, owner.web.account)
	}
	return map[string]any{"thread_id": p.Thread, "turn_id": reply.Turn.ID, "mode": "start", "status": "accepted", "accepted_at": time.Now().Unix()}, nil
}

var errWebNoActiveTurn = errors.New("현재 개입할 수 있는 작업이 없습니다. 채팅 상태를 확인해 주세요.")

func (w *WebChat) sendToWriter(ctx context.Context, c *webConnection, r Request, p webParams, pid int) (any, error) {
	probe, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	automatic := p.Mode == "immediate" && p.Turn == ""
	owner, err := w.openInputOwner(probe, p.Thread, pid)
	if err != nil {
		if automatic && pid != c.process.PID {
			return w.submitGUIRelay(ctx, c, r, p, pid)
		}
		return nil, errWebForeignInput
	}
	defer owner.release()
	if owner.web != nil {
		if err := owner.web.verifyExecutionLogin(ctx); err != nil {
			return nil, err
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		delivery, err := owner.delivery(probe, p.Thread)
		if err != nil {
			return nil, errors.New("현재 턴의 상태를 읽을 수 없습니다. 입력은 전송되지 않았습니다.")
		}
		switch delivery.State {
		case "waiting":
			return nil, errors.New("현재 턴이 승인 또는 사용자 입력을 기다립니다. 원래 창에서 응답해 주세요.")
		case "idle":
			if !automatic {
				return nil, errWebNoActiveTurn
			}
			return w.startWebTurn(ctx, owner, c, r, p)
		case "ready":
			if p.Turn != "" && delivery.TurnID != p.Turn {
				return nil, errWebNoActiveTurn
			}
			if err := w.verifyInputOwner(probe, owner, p.Thread, false); err != nil {
				return nil, err
			}
			result, err := steerWebTurn(ctx, owner.client, r, p, delivery.TurnID)
			if attempt == 0 && automatic && rejectedWebSteer(err, delivery.TurnID) {
				continue // The native server explicitly rejected this input before sending it.
			}
			return result, err
		default:
			if attempt == 0 && automatic {
				continue // Re-read if the status and the latest turn did not agree yet.
			}
			return nil, errors.New("채팅의 실행 상태를 확인할 수 없습니다. 입력은 전송되지 않았습니다.")
		}
	}
	return nil, errWebNoActiveTurn
}

func steerWebTurn(ctx context.Context, client *cr.Client, r Request, p webParams, activeTurn string) (any, error) {
	var reply struct{ TurnID string }
	// Bind the official RPC to the running turn observed now, independently of
	// the turn that was visible in the browser when the user began typing.
	args := map[string]any{"threadId": p.Thread, "expectedTurnId": activeTurn, "clientUserMessageId": r.Operation, "input": cr.TextInput(p.Text)}
	if err := callWebInput(ctx, client, "turn/steer", args, &reply); err != nil {
		return nil, err
	}
	if reply.TurnID != activeTurn {
		return nil, errWebInputUnconfirmed
	}
	return map[string]any{"thread_id": p.Thread, "turn_id": activeTurn, "mode": "steer", "status": "accepted", "accepted_at": time.Now().Unix()}, nil
}

func rejectedWebSteer(err error, turn string) bool {
	var rpc *cr.RPCError
	if !errors.As(err, &rpc) || rpc.Code != -32600 {
		return false
	}
	return rpc.Message == "no active turn to steer" || strings.HasPrefix(rpc.Message, "expected active turn id `"+turn+"` but found `")
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
