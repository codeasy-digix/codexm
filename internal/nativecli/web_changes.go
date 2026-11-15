package nativecli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

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
		}
		if err := c.client.Call(ctx, "thread/start", args, &result); err != nil {
			return nil, err
		}
		w.rememberAccount(result.Thread.ID, c.account)
		result.Thread.Account = c.account
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
		if p.Pinned {
			if _, err := c.client.Read(ctx, p.Thread, false); err != nil {
				return nil, err
			}
		}
		return w.pin(p.Thread, p.Pinned)
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
	if err := w.verifyLogin(ctx, c); err != nil {
		return nil, err
	}
	if p.Mode == "queue" {
		owners, err := writerProcesses(ctx, config.Expand(w.c.Runtime.SharedCodexHome), p.Thread)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if len(owners) == 0 {
			if err := w.prepare(ctx, c, p.Thread); err != nil {
				return nil, err
			}
		}
		var reply struct {
			Queued struct {
				ID string `json:"id"`
			} `json:"queuedSubmission"`
		}
		if err := c.client.Call(ctx, "thread/queue/add", map[string]any{"threadId": p.Thread, "clientUserMessageId": r.Operation, "input": cr.TextInput(p.Text)}, &reply); err != nil {
			return nil, err
		}
		return map[string]any{"thread_id": p.Thread, "mode": "queue", "queued_id": reply.Queued.ID, "status": "accepted"}, nil
	}
	if err := w.prepare(ctx, c, p.Thread); err != nil {
		return nil, err
	}
	current, err := c.client.Read(ctx, p.Thread, false)
	if err != nil {
		return nil, err
	}
	method := "turn/start"
	args := map[string]any{"threadId": p.Thread, "clientUserMessageId": r.Operation, "input": cr.TextInput(p.Text)}
	if current.Status.Type == "active" {
		if p.Turn == "" {
			return nil, errors.New("the chat became active; refresh before intervening")
		}
		method = "turn/steer"
		args["expectedTurnId"] = p.Turn
	} else if p.Turn != "" {
		return nil, errors.New("the previous turn finished; refresh before sending a new turn")
	}
	var reply struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
		TurnID string `json:"turnId"`
	}
	if err := c.client.Call(ctx, method, args, &reply); err != nil {
		return nil, err
	}
	w.rememberAccount(p.Thread, c.account)
	id := reply.Turn.ID
	if id == "" {
		id = reply.TurnID
	}
	return map[string]any{"thread_id": p.Thread, "turn_id": id, "mode": strings.TrimPrefix(method, "turn/"), "status": "accepted", "accepted_at": time.Now().Unix()}, nil
}
