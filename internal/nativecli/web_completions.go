package nativecli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/config"
)

type webCompletionID struct {
	Thread string `json:"thread"`
	Turn   string `json:"turn"`
}

type webCompletion struct {
	webCompletionID
	At    int64  `json:"at"`
	Title string `json:"title,omitempty"`
}

type projectedCompletion struct {
	At     int64
	Status string
}

func (w *WebChat) readCompletions(ctx context.Context, after int64) ([]webCompletion, error) {
	now := time.Now()
	if after < now.Add(-5*time.Minute).UnixMilli() || after > now.UnixMilli() {
		return nil, errors.New("completion reads require a recent timestamp")
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	rollouts, err := catalog.RecentLocalRollouts(ctx, home, after)
	result := []webCompletion{}
	if err != nil || len(rollouts) == 0 {
		return result, err
	}
	projected, err := readProjectedCompletions(ctx, home, rollouts, after, w.c.Runtime.WebGUIRelay)
	if err != nil {
		return nil, err
	}
	// The paginated history index provides status. The rollout also supplies
	// precise event times and covers older chats with legacy history.
	roots := make(map[bool]*os.Root, 2)
	for archived, directory := range map[bool]string{false: "sessions", true: "archived_sessions"} {
		if path, err := filepath.EvalSymlinks(filepath.Join(home, directory)); err == nil {
			if root, err := os.OpenRoot(path); err == nil {
				defer root.Close()
				roots[archived] = root
			}
		}
	}
	titles := make(map[string]string, len(rollouts))
	for _, rollout := range rollouts {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		titles[rollout.ID] = rollout.Title
		root := roots[rollout.Archived]
		if root == nil || rollout.ID == w.c.Runtime.WebGUIRelay || !threadID.MatchString(rollout.ID) {
			continue
		}
		for _, event := range rolloutCompletions(root, rollout, after) {
			status, known := projected[event.webCompletionID]
			if !known || status.Status == "completed" {
				projected[event.webCompletionID] = projectedCompletion{event.At, "completed"}
			}
		}
	}
	for id, row := range projected {
		if row.Status == "completed" && row.At >= after && row.At <= now.UnixMilli() {
			result = append(result, webCompletion{webCompletionID: id, At: row.At, Title: titles[id.Thread]})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].At < result[j].At })
	return result, nil
}

func readProjectedCompletions(ctx context.Context, home string, rollouts []catalog.RecentRollout, after int64, relay string) (map[webCompletionID]projectedCompletion, error) {
	found := make(map[webCompletionID]projectedCompletion)
	db, err := catalog.OpenReadOnly(filepath.Join(home, "thread_history_1.sqlite"))
	if os.IsNotExist(err) {
		return found, nil
	}
	if err != nil {
		return nil, err
	}
	defer db.Close()
	args := []any{after / 1000}
	for _, rollout := range rollouts {
		if threadID.MatchString(rollout.ID) && rollout.ID != relay {
			args = append(args, rollout.ID)
		}
	}
	if len(args) == 1 {
		return found, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT thread_id,turn_id,completed_at,status FROM thread_turns
		WHERE completed_at>=? AND (root_turn_id IS NULL OR root_turn_id='' OR root_turn_id=turn_id)
		AND thread_id IN (`+strings.Repeat("?,", len(args)-2)+`?)`, args...)
	if err != nil {
		if strings.Contains(err.Error(), "no such table: thread_turns") || strings.Contains(err.Error(), "no such column: root_turn_id") {
			return found, nil // Older stores are read from their native rollouts.
		}
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id webCompletionID
		var status projectedCompletion
		if err := rows.Scan(&id.Thread, &id.Turn, &status.At, &status.Status); err != nil {
			return nil, err
		}
		if threadID.MatchString(id.Turn) {
			if status.At < 100000000000 {
				status.At *= 1000
			}
			found[id] = status
		}
	}
	return found, rows.Err()
}

func rolloutCompletions(root *os.Root, rollout catalog.RecentRollout, after int64) []webCompletion {
	if !filepath.IsAbs(rollout.Path) || !strings.HasSuffix(filepath.Base(rollout.Path), "-"+rollout.ID+".jsonl") {
		return nil
	}
	filename, err := filepath.EvalSymlinks(rollout.Path)
	if err != nil {
		return nil
	}
	path, err := filepath.Rel(root.Name(), filename)
	if err != nil || !filepath.IsLocal(path) {
		return nil
	}
	file, err := root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.ModTime().UnixMilli() < after {
		return nil
	}
	start := max(0, info.Size()-maxWebRolloutTail)
	data := make([]byte, info.Size()-start)
	n, err := file.ReadAt(data, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil
	}
	return completionEvents(data[:n], start != 0, rollout.ID, after)
}

func completionEvents(data []byte, partialHead bool, thread string, after int64) []webCompletion {
	var events []webCompletion
	for {
		line, remaining, complete := bytes.Cut(data, []byte{'\n'})
		if !complete {
			break
		}
		data = remaining
		if partialHead {
			partialHead = false
			continue
		}
		if !bytes.Contains(line, []byte(`"task_complete"`)) {
			continue
		}
		var event struct {
			Type      string
			Timestamp time.Time
			Payload   struct {
				Type  string
				Turn  string `json:"turn_id"`
				Root  string `json:"root_turn_id"`
				Final string `json:"last_agent_message"`
			}
		}
		if json.Unmarshal(line, &event) == nil && event.Type == "event_msg" &&
			event.Payload.Type == "task_complete" && threadID.MatchString(event.Payload.Turn) &&
			(event.Payload.Root == "" || event.Payload.Root == event.Payload.Turn) &&
			event.Payload.Final != "" && event.Timestamp.UnixMilli() >= after {
			events = append(events, webCompletion{webCompletionID: webCompletionID{thread, event.Payload.Turn}, At: event.Timestamp.UnixMilli()})
		}
	}
	return events
}
