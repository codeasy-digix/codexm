package catalog

import (
	"context"
	"database/sql"
)

func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

type RecentRollout struct {
	ID   string
	Path string
}

// Use the native update index to find recent top-level local chats without
// opening them or requiring the process that wrote them to stay alive.
func RecentLocalRollouts(ctx context.Context, home string, after int64) ([]RecentRollout, error) {
	filename, err := database(home)
	if err != nil || filename == "" {
		return nil, err
	}
	db, err := OpenReadOnly(filename)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	columns, err := tableColumns(ctx, db, "threads")
	if err != nil || !columns["rollout_path"] || !columns["updated_at"] || !columns["id"] {
		return nil, err
	}
	where := `(updated_at>=? OR updated_at>=? AND updated_at<100000000000)`
	args := []any{after, after / 1000}
	order := `updated_at`
	if columns["updated_at_ms"] {
		where = `(updated_at_ms>=? OR COALESCE(updated_at_ms,0)=0 AND ` + where + `)`
		args = append([]any{after}, args...)
		order = `updated_at_ms`
	}
	if columns["archived"] {
		where += ` AND COALESCE(archived,0)=0`
	}
	if columns["source"] {
		where += ` AND source IN ('cli','vscode')`
	}
	if columns["attached_thread_parent_id"] {
		where += ` AND COALESCE(attached_thread_parent_id,'')=''`
	}
	rows, err := db.QueryContext(ctx, `SELECT id,rollout_path FROM threads WHERE `+where+` ORDER BY `+order+` DESC LIMIT 256`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []RecentRollout
	for rows.Next() {
		var rollout RecentRollout
		if err := rows.Scan(&rollout.ID, &rollout.Path); err != nil {
			return nil, err
		}
		results = append(results, rollout)
	}
	return results, rows.Err()
}
