package nativecli

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

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

// Read the native index and file identity without loading or parsing a rollout.
// A missing, replaced, archived, or rewritten source cannot reuse the old page.
func webHistoryRevision(home string, entry catalog.Entry) string {
	if !filepath.IsAbs(entry.RolloutPath) {
		return ""
	}
	dir := "sessions"
	if entry.Archived {
		dir = "archived_sessions"
	}
	root, err := filepath.EvalSymlinks(filepath.Join(home, dir))
	if err != nil {
		return ""
	}
	path, err := filepath.EvalSymlinks(entry.RolloutPath)
	if err != nil || !strings.HasSuffix(filepath.Base(path), "-"+entry.ID+".jsonl") {
		return ""
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(relative) {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	key := fmt.Sprintf("%s\x00%s\x00%t:%d:%d:%d:%d:%d", home, path, entry.Archived, stat.Dev, stat.Ino, info.Size(), info.ModTime().UnixNano(), entry.ModifiedAt)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
}

func (w *WebChat) historyVersion(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", errors.New("thread UUID required")
	}
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	entry, err := catalog.Lookup(ctx, home, id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return webHistoryRevision(home, entry), nil
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
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	entry, err := catalog.Lookup(ctx, home, p.Thread)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		entry = catalog.Entry{ID: p.Thread, Cwd: read.Thread.Cwd, ProjectID: read.Thread.Project}
	}
	desktop, err := catalog.ReadDesktop(ctx, home)
	if err != nil {
		return nil, err
	}
	read.Thread.Archived = entry.Archived
	read.Thread.Pinned = desktop.IsPinned(entry)
	read.Thread.Project = desktop.ProjectFor(entry)
	if read.Thread.Model == "" {
		read.Thread.Model = entry.Model
	}
	if read.Thread.Effort == "" {
		read.Thread.Effort = entry.Effort
	}
	owners, err := writerProcesses(ctx, home, p.Thread)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	read.Thread.External = len(owners) > 0 && (len(owners) != 1 || owners[0] != c.process.PID)
	var delivery *webDelivery
	var externalTurn string
	if read.Thread.External && p.Cursor == "" && !read.Thread.Archived {
		control := w.externalDelivery(ctx, p.Thread, owners)
		delivery = &control
		writers, _ := w.writerSnapshot()
		activity := writers[p.Thread].activity
		if activity.State == "running" {
			externalTurn = activity.TurnID
		}
	}
	var queue struct {
		Data       []json.RawMessage `json:"data"`
		NextCursor *string           `json:"nextCursor"`
	}
	queue.Data = []json.RawMessage{}
	// Native history reads support archived chats, but queue/list rejects them.
	if !read.Thread.Archived {
		if err := c.client.Call(ctx, "thread/queue/list", map[string]any{"threadId": p.Thread, "limit": 50}, &queue); err != nil {
			return nil, err
		}
	}
	result := map[string]any{
		"thread": read.Thread, "queue": queue.Data, "queueNextCursor": queue.NextCursor, "pending": w.pending(c, p.Thread),
		"delivery": delivery, "loginDraining": w.loginChanged(c), "loginGeneration": c.generation,
	}
	revision := webHistoryRevision(home, entry)
	// The live reply and queue above are always read from the official server.
	// Keep unfinished turns out of the persistent cache.
	live := read.Thread.Status.Type == "active" || externalTurn != "" ||
		(delivery != nil && (delivery.State == "ready" || delivery.State == "waiting"))
	if p.Cursor == "" && live {
		revision = ""
	}
	if revision != "" && revision == p.HistoryRevision {
		result["historyRevision"] = revision
		result["historyUnchanged"] = true
		return result, nil
	}
	page, next, stable, err := w.historyPage(ctx, c, home, p, limit, read.Thread.Archived, externalTurn)
	if err != nil {
		return nil, err
	}
	result["turns"], result["nextCursor"] = page, next
	if stable && revision != "" && revision == webHistoryRevision(home, entry) {
		result["historyRevision"] = revision
	}
	return result, nil
}

func (w *WebChat) historyPage(ctx context.Context, c *webConnection, home string, p webParams, limit int, archived bool, externalTurn string) ([]WebTurn, *string, bool, error) {
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
			return nil, nil, false, err
		}
		return []WebTurn{}, nil, false, nil
	}
	slices.Reverse(page.Data)
	turnIDs := make([]string, len(page.Data))
	for i, turn := range page.Data {
		turnIDs[i] = turn.ID
	}
	commentary, err := readWebCommentary(ctx, home, p.Thread, turnIDs)
	stable := err == nil || errors.Is(err, os.ErrNotExist)
	for i := range page.Data {
		turn := &page.Data[i]
		appendWebCommentary(turn, commentary[turn.ID])
		stable = stable && turn.Status != "inProgress"
	}
	if p.Cursor == "" && !archived {
		// The compact summary drops additional user input within a turn.
		// Include recent in-turn messages even after a queued turn starts.
		itemsCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		for i := max(0, len(page.Data)-2); i < len(page.Data); i++ {
			turn := &page.Data[i]
			if !addRecentMessages(itemsCtx, c, p.Thread, turn, turn.ID == externalTurn) {
				stable = false
			}
		}
	}
	return page.Data, page.NextCursor, stable, nil
}

// Keep the summary bounded and leave tool output deferred. External running
// commentary can be read from persisted items without claiming the chat.
func addRecentMessages(ctx context.Context, c *webConnection, thread string, turn *WebTurn, progress bool) bool {
	var page struct {
		Data []struct {
			Item json.RawMessage
		}
	}
	args := map[string]any{
		"threadId": thread, "turnId": turn.ID, "limit": 12, "sortDirection": "desc",
		"deferOutput": map[string]bool{"commandExecution": true, "mcpToolCall": true},
	}
	if err := c.client.Call(ctx, "thread/items/list", args, &page); err != nil {
		return false
	}
	seen := make(map[string]bool)
	for _, raw := range turn.Items {
		var existing struct{ ID string }
		if json.Unmarshal(raw, &existing) == nil {
			seen[existing.ID] = true
		}
	}
	slices.Reverse(page.Data)
	for _, entry := range page.Data {
		var item struct {
			ID         string
			Type       string
			Phase      string
			Text       string
			Moderation *struct{ Moderated bool }
		}
		if json.Unmarshal(entry.Item, &item) != nil || seen[item.ID] || item.ID == "" {
			continue
		}
		switch item.Type {
		case "userMessage":
			turn.Items = append(turn.Items, entry.Item)
		case "agentMessage":
			if progress && item.Phase == "commentary" && item.Text != "" && (item.Moderation == nil || !item.Moderation.Moderated) {
				raw, _ := json.Marshal(map[string]string{"id": item.ID, "type": item.Type, "phase": item.Phase, "text": limitLiveText(item.Text)})
				turn.Items = append(turn.Items, raw)
			}
		}
	}
	return true
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
