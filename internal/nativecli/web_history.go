package nativecli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/codeasy-digix/codexm/internal/catalog"
	cr "github.com/codeasy-digix/codexm/internal/runtime"

	"github.com/codeasy-digix/codexm/internal/config"
)

type WebTurn struct {
	ID          string            `json:"id"`
	Status      string            `json:"status"`
	StartedAt   *int64            `json:"startedAt"`
	CompletedAt *int64            `json:"completedAt"`
	Items       []json.RawMessage `json:"items"`
	ItemsView   string            `json:"itemsView"`
	Error       json.RawMessage   `json:"error,omitempty"`
}

func (w *WebChat) history(ctx context.Context, c *webConnection, p webParams) (any, error) {
	if p.Thread == "" {
		return nil, errors.New("thread UUID required")
	}
	limit, err := webPageLimit(p.Limit, 20, 8)
	if err != nil {
		return nil, err
	}
	var read struct {
		Thread WebThread `json:"thread"`
	}
	if err := c.client.Call(ctx, "thread/read", map[string]any{"threadId": p.Thread, "includeTurns": false}, &read); err != nil {
		return nil, err
	}
	entry, err := catalog.Lookup(ctx, config.Expand(w.c.Runtime.SharedCodexHome), p.Thread)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		entry = catalog.Entry{ID: p.Thread, Cwd: read.Thread.Cwd, ProjectID: read.Thread.Project}
	}
	desktop, err := catalog.ReadDesktop(ctx, config.Expand(w.c.Runtime.SharedCodexHome))
	if err != nil {
		return nil, err
	}
	read.Thread.Archived = entry.Archived
	read.Thread.Pinned = desktop.IsPinned(entry)
	read.Thread.Project = desktop.ProjectFor(entry)
	owners, err := writerProcesses(ctx, config.Expand(w.c.Runtime.SharedCodexHome), p.Thread)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	read.Thread.External = len(owners) > 0 && (len(owners) != 1 || owners[0] != c.process.PID)
	var page struct {
		Data       []WebTurn `json:"data"`
		NextCursor *string   `json:"nextCursor"`
	}
	args := map[string]any{"threadId": p.Thread, "limit": limit, "sortDirection": "desc", "itemsView": "summary"}
	if p.Cursor != "" {
		args["cursor"] = p.Cursor
	}
	if err := c.client.Call(ctx, "thread/turns/list", args, &page); err != nil {
		var rpc *cr.RPCError
		if !errors.As(err, &rpc) || rpc.Code != -32600 || !strings.Contains(rpc.Message, "not materialized") {
			return nil, err
		}
		page.Data = []WebTurn{}
	}
	slices.Reverse(page.Data)
	var queue struct {
		Data       []json.RawMessage `json:"data"`
		NextCursor *string           `json:"nextCursor"`
	}
	queue.Data = []json.RawMessage{}
	// Native history reads support archived chats, but queue/list rejects them.
	// Never unarchive a conversation merely to display its existing messages.
	if !read.Thread.Archived {
		if err := c.client.Call(ctx, "thread/queue/list", map[string]any{"threadId": p.Thread, "limit": 50}, &queue); err != nil {
			return nil, err
		}
	}
	return map[string]any{"thread": read.Thread, "turns": page.Data, "nextCursor": page.NextCursor, "queue": queue.Data, "queueNextCursor": queue.NextCursor, "pending": w.pending(c, p.Thread)}, nil
}

func (w *WebChat) items(ctx context.Context, c *webConnection, p webParams) (any, error) {
	if p.Thread == "" || p.Turn == "" {
		return nil, errors.New("thread and turn IDs required")
	}
	limit, err := webPageLimit(p.Limit, 50, 30)
	if err != nil {
		return nil, err
	}
	args := map[string]any{"threadId": p.Thread, "turnId": p.Turn, "limit": limit, "sortDirection": "asc", "deferOutput": map[string]bool{"commandExecution": true, "mcpToolCall": true}}
	if p.Cursor != "" {
		args["cursor"] = p.Cursor
	}
	var result json.RawMessage
	if err := c.client.Call(ctx, "thread/items/list", args, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (w *WebChat) item(ctx context.Context, c *webConnection, p webParams) (any, error) {
	if p.Thread == "" || p.Turn == "" || p.Item == "" || len(p.Item) > 256 {
		return nil, errors.New("thread, turn and item IDs required")
	}
	var result json.RawMessage
	if err := c.client.Call(ctx, "thread/items/read", map[string]any{"threadId": p.Thread, "turnId": p.Turn, "itemIds": []string{p.Item}}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (w *WebChat) pending(c *webConnection, id string) []any {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := []any{}
	for key, event := range c.pending {
		var params map[string]any
		if json.Unmarshal(event.Params, &params) != nil {
			continue
		}
		thread, _ := params["threadId"].(string)
		if id != "" && thread != id {
			continue
		}
		result = append(result, map[string]any{"request_id": key, "method": event.Method, "params": params})
	}
	return result
}

func (w *WebChat) respond(ctx context.Context, c *webConnection, p webParams) (any, error) {
	if p.Thread == "" || p.Request == "" {
		return nil, errors.New("thread and pending request IDs required")
	}
	if err := w.verifyOwner(ctx, c, p.Thread, true); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	event, ok := c.pending[p.Request]
	if !ok {
		return nil, errors.New("request is no longer pending; no response was replayed")
	}
	var target struct {
		Thread string `json:"threadId"`
	}
	if json.Unmarshal(event.Params, &target) != nil || target.Thread != p.Thread {
		return nil, errors.New("pending request belongs to another thread")
	}
	var response map[string]any
	if err := json.Unmarshal(p.Response, &response); err != nil {
		return nil, err
	}
	switch event.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		decision, _ := response["decision"].(string)
		if decision != "accept" && decision != "decline" && decision != "cancel" {
			return nil, errors.New("select accept, decline or cancel")
		}
		response = map[string]any{"decision": decision}
	case "item/tool/requestUserInput":
		if _, ok := response["answers"].(map[string]any); !ok {
			return nil, errors.New("answers are required")
		}
	default:
		return nil, errors.New("this request type cannot be approved by the web frontend")
	}
	if err := c.client.Reply(event.ID, response); err != nil {
		return nil, err
	}
	delete(c.pending, p.Request)
	return map[string]any{"responded": true}, nil
}
