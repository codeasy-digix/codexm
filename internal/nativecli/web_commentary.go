package nativecli

import (
	"context"
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
