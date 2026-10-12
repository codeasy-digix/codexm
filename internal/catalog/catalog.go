// Package catalog reads Codex's conversation index without owning its schema.
// Manager jobs, accounts and logs stay in codexm's separate database.
package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/jobs"
	_ "modernc.org/sqlite"
)

// Entry contains list metadata, never conversation turns or goal objectives.
type Entry struct {
	ID        string `json:"id"`
	ThreadID  string `json:"thread_id,omitempty"`
	JobID     string `json:"job_id,omitempty"`
	Title     string `json:"title"`
	Cwd       string `json:"cwd"`
	Account   string `json:"account,omitempty"`
	State     string `json:"state"`
	Kind      string `json:"kind"`
	Managed   bool   `json:"managed"`
	Archived  bool   `json:"archived"`
	Source    string `json:"source,omitempty"`
	Model     string `json:"model,omitempty"`
	Effort    string `json:"effort,omitempty"`
	Tokens    int64  `json:"tokens_used"`
	UpdatedAt int64  `json:"updated_at_ms"`
}

type List struct {
	Sessions []Entry `json:"sessions"`
	Database string  `json:"database,omitempty"`
	Warning  string  `json:"warning,omitempty"`
}

// OpenReadOnly is also used to read codexm metadata when its supervisor is off.
// mode=ro forbids creation and writes; query_only is applied to every connection.
// Do not use immutable=1: it would omit committed changes in a live WAL.
func OpenReadOnly(filename string) (*sql.DB, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular SQLite file: %s", filename)
	}
	u := url.URL{Scheme: "file", Path: filename}
	q := url.Values{"mode": {"ro"}, "_pragma": {"query_only(1)", "busy_timeout(1000)"}}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err == nil {
		db.SetMaxOpenConns(1)
	}
	return db, err
}

func database(home string) (string, error) {
	files, err := filepath.Glob(filepath.Join(home, "state_*.sqlite"))
	if err != nil {
		return "", err
	}
	type candidate struct {
		name    string
		version int
	}
	var found []candidate
	for _, file := range files {
		v, e := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(file), "state_"), ".sqlite"))
		if e == nil {
			found = append(found, candidate{file, v})
		}
	}
	if len(found) == 0 {
		return "", nil
	}
	sort.Slice(found, func(i, j int) bool { return found[i].version > found[j].version })
	return found[0].name, nil
}

func Read(ctx context.Context, home string) (List, error) {
	list := List{Sessions: []Entry{}}
	filename, err := database(home)
	if err != nil || filename == "" {
		return list, err
	}
	list.Database = filename
	db, err := OpenReadOnly(filename)
	if err != nil {
		return list, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "PRAGMA table_info(threads)")
	if err != nil {
		return list, err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, primary int
		var name, kind string
		var fallback any
		if err = rows.Scan(&cid, &name, &kind, &notNull, &fallback, &primary); err != nil {
			rows.Close()
			return list, err
		}
		columns[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return list, err
	}
	if !columns["id"] {
		return list, fmt.Errorf("unsupported Codex conversation index; missing threads.id (database left unchanged)")
	}
	text := func(name string) string {
		if columns[name] {
			return `COALESCE("` + name + `", '')`
		}
		return `''`
	}
	number := func(name string) string {
		if columns[name] {
			return `COALESCE("` + name + `", 0)`
		}
		return `0`
	}
	title := text("title")
	if columns["name"] {
		title = `COALESCE(NULLIF("name", ''), ` + title + `)`
	}
	updated := number("updated_at") + ` * 1000`
	if columns["updated_at_ms"] {
		updated = `COALESCE(NULLIF("updated_at_ms", 0), ` + updated + `)`
	}
	if columns["recency_at_ms"] {
		updated = `COALESCE(NULLIF("recency_at_ms", 0), ` + updated + `)`
	}
	query := `SELECT "id", ` + title + `, ` + text("cwd") + `, ` + text("source") + `, ` + text("model") + `, ` + text("reasoning_effort") + `, ` + number("tokens_used") + `, ` + number("archived") + `, ` + updated + ` FROM threads ORDER BY 9 DESC, id`
	rows, err = db.QueryContext(ctx, query)
	if err != nil {
		return list, err
	}
	defer rows.Close()
	for rows.Next() {
		var e Entry
		if err = rows.Scan(&e.ID, &e.Title, &e.Cwd, &e.Source, &e.Model, &e.Effort, &e.Tokens, &e.Archived, &e.UpdatedAt); err != nil {
			return list, err
		}
		e.ThreadID, e.Kind, e.State = e.ID, "chat", "saved"
		if strings.HasPrefix(e.Source, "{") {
			e.Source = "subagent"
		}
		if e.Archived {
			e.State = "archived"
		}
		if e.Title == "" {
			e.Title = e.ID
		}
		list.Sessions = append(list.Sessions, e)
	}
	return list, rows.Err()
}

// Merge decorates native rows with manager metadata. It never imports them.
func Merge(list List, managed []jobs.Job) List {
	byThread := map[string]int{}
	for i, entry := range list.Sessions {
		byThread[entry.ThreadID] = i
	}
	for _, j := range managed {
		e := Entry{ID: j.ThreadID, ThreadID: j.ThreadID, JobID: j.ID, Title: j.Title, Cwd: j.Cwd, Account: j.Account, State: j.State, Kind: j.Kind, Managed: j.Managed, Model: j.Model, Effort: j.Effort, UpdatedAt: j.UpdatedAt.UnixMilli()}
		if e.ID == "" {
			e.ID = j.ID
		}
		if i, ok := byThread[j.ThreadID]; ok && j.ThreadID != "" {
			native := list.Sessions[i]
			e.Archived, e.Source, e.Tokens = native.Archived, native.Source, native.Tokens
			if native.Title != "" {
				e.Title = native.Title
			}
			if native.UpdatedAt > e.UpdatedAt {
				e.UpdatedAt = native.UpdatedAt
			}
			list.Sessions[i] = e
		} else {
			list.Sessions = append(list.Sessions, e)
		}
	}
	sort.SliceStable(list.Sessions, func(i, j int) bool {
		if list.Sessions[i].UpdatedAt == list.Sessions[j].UpdatedAt {
			return list.Sessions[i].ID < list.Sessions[j].ID
		}
		return list.Sessions[i].UpdatedAt > list.Sessions[j].UpdatedAt
	})
	return list
}

// Job is an ephemeral display projection, never a row to persist or execute.
func (e Entry) Job(home string) jobs.Job {
	id := e.JobID
	if id == "" {
		id = e.ID
	}
	return jobs.Job{ID: id, ThreadID: e.ThreadID, Title: e.Title, Cwd: e.Cwd, Store: home, Account: e.Account, State: e.State, Kind: e.Kind, Managed: e.Managed, Model: e.Model, Effort: e.Effort, UpdatedAt: time.UnixMilli(e.UpdatedAt)}
}
