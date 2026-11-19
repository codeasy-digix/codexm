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
	"sort"
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
func webRolloutRevision(home string, entry catalog.Entry) string {
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

// The native projection can retain a complete transcript after an older
// rollout file is moved. Its per-thread progress is a cheap revision source.
// Also include it for live rollouts so a delayed index update invalidates pages.
func webHistoryProjectionVersions(ctx context.Context, home, id string) (map[string]string, error) {
	result := map[string]string{}
	filename := filepath.Join(home, "thread_history_1.sqlite")
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return result, nil
	}
	db, err := catalog.OpenReadOnly(filename)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	query := `SELECT thread_id, next_rollout_byte_offset, next_rollout_ordinal FROM thread_history_projection_state`
	var args []any
	if id != "" {
		query += " WHERE thread_id = ?"
		args = append(args, id)
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		if strings.Contains(err.Error(), "no such table: thread_history_projection_state") {
			return result, nil
		}
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var thread string
		var offset, ordinal int64
		if err := rows.Scan(&thread, &offset, &ordinal); err != nil {
			return nil, err
		}
		if ordinal > 0 {
			result[thread] = fmt.Sprintf("%d:%d:%d:%d", stat.Dev, stat.Ino, offset, ordinal)
		}
	}
	return result, rows.Err()
}

func webHistoryRevision(home string, entry catalog.Entry, projection string) string {
	rollout := webRolloutRevision(home, entry)
	if rollout == "" && projection == "" {
		return ""
	}
	key := fmt.Sprintf("%s\x00%s\x00%t:%d\x00%s:%s", home, entry.ID, entry.Archived, entry.ModifiedAt, rollout, projection)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
}

func webHistoryEntryRevision(ctx context.Context, home string, entry catalog.Entry) (string, error) {
	projection, err := webHistoryProjectionVersions(ctx, home, entry.ID)
	if err != nil {
		return "", err
	}
	return webHistoryRevision(home, entry, projection[entry.ID]), nil
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
	return webHistoryEntryRevision(ctx, home, entry)
}

type webHistorySource struct {
	Thread   string `json:"thread"`
	Revision string `json:"revision"`
}

func (w *WebChat) historySources(ctx context.Context) ([]webHistorySource, error) {
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	index, err := catalog.Read(ctx, home)
	if err != nil {
		return nil, err
	}
	projection, err := webHistoryProjectionVersions(ctx, home, "")
	if err != nil {
		return nil, err
	}
	// Native recency can move when a chat is opened. Prefer actual modifications
	// while using the file revision, even if two changes share one timestamp.
	sort.SliceStable(index.Sessions, func(i, j int) bool { return index.Sessions[i].ModifiedAt > index.Sessions[j].ModifiedAt })
	sources := make([]webHistorySource, 0, len(index.Sessions))
	for _, entry := range index.Sessions {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if entry.ID == w.c.Runtime.WebGUIRelay || !threadID.MatchString(entry.ID) || entry.AttachedParent != "" ||
			(entry.Source != "cli" && entry.Source != "vscode") {
			continue
		}
		if revision := webHistoryRevision(home, entry, projection[entry.ID]); revision != "" {
			sources = append(sources, webHistorySource{entry.ID, revision})
		}
	}
	return sources, nil
}

// Background reads never load a thread, take a writer lock or request a model.
// A page is reusable only if the same native revision exists before and after it.
func (w *WebChat) readHistoryPage(ctx context.Context, c *webConnection, p webParams) (any, error) {
	limit, err := webPageLimit(p.Limit, 20, 8)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	revision, err := w.historyVersion(ctx, p.Thread)
	if err != nil || revision == "" || revision != p.HistoryRevision {
		return nil, err
	}
	if p.Cursor == "" {
		writers, _ := w.writerSnapshot()
		state := writers[p.Thread].activity.State
		thread, err := c.client.Read(ctx, p.Thread, false)
		if err != nil || thread.Status.Type == "active" || state == "running" || state == "waiting" {
			return nil, err
		}
	}
	turns, next, complete, err := w.historyPage(ctx, c, p, limit)
	if err != nil || !complete {
		return nil, err
	}
	current, err := w.historyVersion(ctx, p.Thread)
	if err != nil || current != revision {
		return nil, err
	}
	return map[string]any{"turns": turns, "nextCursor": next, "historyRevision": revision}, nil
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
		if w.webOnly {
			draft, draftErr := w.thread(ctx, p.Thread)
			if draftErr == nil && draft != nil && draft.Status.Type == "idle" && !draft.Archived && draft.Account != c.account {
				if _, entryErr := catalog.Lookup(ctx, config.Expand(w.c.Runtime.SharedCodexHome), p.Thread); errors.Is(entryErr, sql.ErrNoRows) {
					q, queueErr := w.ownedQueueDisplay(p.Thread)
					if queueErr != nil {
						return nil, queueErr
					}
					return map[string]any{"thread": draft, "turns": []WebTurn{}, "queue": q, "pending": []webPendingRequest{}, "webOnly": true}, nil
				}
			}
		}
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
	writers, writersKnown := w.writerSnapshot()
	var owners []int
	if writer, found := writers[p.Thread]; writersKnown && found && writer.pid > 0 {
		owners = []int{writer.pid}
	} else if !writersKnown || found {
		// A slow owner probe must not hide saved messages. Mutations check
		// the actual writer separately and never use this observation.
		probe, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		owners, _ = writerProcesses(probe, home, p.Thread)
		cancel()
	}
	read.Thread.External = len(owners) > 0 && (len(owners) != 1 || owners[0] != c.process.PID)
	executionAccount := ""
	if w.webOnly && len(owners) == 1 {
		_, owner, release := w.leaseWebWriter(ctx, owners[0])
		if owner != nil {
			executionAccount = owner.loginName
			read.Thread.External = false
			release()
		}
	}
	var delivery *webDelivery
	var externalTurn string
	if read.Thread.External && p.Cursor == "" && !read.Thread.Archived {
		control := w.externalDelivery(ctx, p.Thread, owners)
		delivery = &control
		activity := writers[p.Thread].activity
		if activity.State == "running" {
			externalTurn = activity.TurnID
		}
	}
	queue := webQueuePage{Data: []json.RawMessage{}}
	// Native history reads support archived chats, but queue/list rejects them.
	if !read.Thread.Archived {
		queue, err = readWebQueue(ctx, c.client, p.Thread)
		if err != nil {
			return nil, err
		}
		if w.webOnly {
			owned, err := w.ownedQueueDisplay(p.Thread)
			if err != nil {
				return nil, err
			}
			queue.Data = append(queue.Data, owned...)
		}
	}
	result := map[string]any{
		"thread": read.Thread, "queue": queue.Data, "queueNextCursor": queue.NextCursor, "pending": w.writerPending(c, p.Thread, owners),
		"delivery": delivery, "loginDraining": w.loginChanged(c), "loginGeneration": c.generation,
	}
	if w.webOnly {
		result["webOnly"], result["executionAccount"] = true, executionAccount
	}
	if w.webOnly && p.Cursor == "" {
		check, cancel := context.WithTimeout(ctx, time.Second)
		if goal, err := c.client.Goal(check, p.Thread); err == nil {
			result["goal"] = goal
		}
		cancel()
	}
	if p.Cursor == "" {
		result["relayedInputs"] = webRelayedInputs(w.c, p.Thread)
	}
	revision, _ := webHistoryEntryRevision(ctx, home, entry)
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
	page, next, stable, err := w.historyPage(ctx, c, p, limit)
	if err != nil {
		return nil, err
	}
	result["turns"], result["nextCursor"] = page, next
	if stable && revision != "" {
		if current, err := w.historyVersion(ctx, p.Thread); err == nil && current == revision {
			result["historyRevision"] = revision
		}
	}
	return result, nil
}

func (w *WebChat) historyPage(ctx context.Context, c *webConnection, p webParams, limit int) ([]WebTurn, *string, bool, error) {
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
	stable := true
	itemsCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	for i := range page.Data {
		turn := &page.Data[i]
		messages, err := webTurnMessages(itemsCtx, c, p.Thread, turn.ID)
		if err != nil {
			stable = false
			continue // Keep the native summary available while the item source catches up.
		}
		turn.Items = messages
		stable = stable && turn.Status != "inProgress"
	}
	return page.Data, page.NextCursor, stable, nil
}

// Keep every displayed user and agent message in order, including input steered
// into older turns. Tool output continues to load through the existing details API.
func webTurnMessages(ctx context.Context, c *webConnection, thread, turn string) ([]json.RawMessage, error) {
	args := map[string]any{
		"threadId": thread, "turnId": turn, "limit": 50, "sortDirection": "asc",
		"deferOutput": map[string]bool{"commandExecution": true, "mcpToolCall": true},
	}
	messages := []json.RawMessage{}
	seen := map[string]bool{}
	for {
		var page struct {
			Data       []struct{ Item json.RawMessage }
			NextCursor string
		}
		if err := c.client.Call(ctx, "thread/items/list", args, &page); err != nil {
			return nil, err
		}
		for _, entry := range page.Data {
			var item struct {
				Type       string
				Moderation *struct{ Moderated bool }
			}
			if err := json.Unmarshal(entry.Item, &item); err != nil {
				return nil, err
			}
			if item.Type == "userMessage" || item.Type == "agentMessage" && (item.Moderation == nil || !item.Moderation.Moderated) {
				messages = append(messages, entry.Item)
			}
		}
		if page.NextCursor == "" {
			return messages, nil
		}
		if seen[page.NextCursor] {
			return nil, errors.New("native item cursor did not advance")
		}
		seen[page.NextCursor] = true
		args["cursor"] = page.NextCursor
	}
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
