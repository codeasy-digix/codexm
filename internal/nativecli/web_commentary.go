package nativecli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/catalog"
)

// Read the stored user-facing messages from the native history projection.
// Use its thread/turn index and leave tool items and reasoning in the details.
func readWebCommentary(ctx context.Context, home, thread string, turns []string) (map[string][]webLiveItem, error) {
	if len(turns) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	db, err := catalog.OpenReadOnly(filepath.Join(home, "thread_history_1.sqlite"))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	args := make([]any, 0, len(turns)+1)
	args = append(args, thread)
	for _, turn := range turns {
		args = append(args, turn)
	}
	query := `SELECT turn_id, item_id, substr(json_extract(item_json, '$.text'), 1, 16384)
		FROM thread_items
		WHERE thread_id = ? AND turn_id IN (` + strings.Repeat("?,", len(turns)-1) + `?)
			AND item_type = 'agentMessage'
			AND json_extract(item_json, '$.phase') = 'commentary'
			AND json_extract(item_json, '$.moderation.moderated') IS NOT 1
			AND json_type(item_json, '$.text') = 'text'
		ORDER BY rollout_ordinal DESC LIMIT 256`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string][]webLiveItem)
	for rows.Next() {
		var turn string
		item := webLiveItem{Phase: "commentary"}
		if err := rows.Scan(&turn, &item.ID, &item.Text); err != nil {
			return nil, err
		}
		if item.ID != "" && item.Text != "" {
			result[turn] = append(result[turn], item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, items := range result {
		slices.Reverse(items)
	}
	return result, nil
}

func appendWebCommentary(turn *WebTurn, commentary []webLiveItem) {
	if len(commentary) == 0 {
		return
	}
	seen := make(map[string]bool, len(turn.Items))
	for _, raw := range turn.Items {
		var item struct{ ID string }
		if json.Unmarshal(raw, &item) == nil {
			seen[item.ID] = true
		}
	}
	for _, item := range commentary {
		if seen[item.ID] {
			continue
		}
		raw, _ := json.Marshal(struct {
			Type string `json:"type"`
			webLiveItem
		}{Type: "agentMessage", webLiveItem: item})
		turn.Items = append(turn.Items, raw)
	}
}
