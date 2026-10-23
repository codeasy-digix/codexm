package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/jobs"
	_ "modernc.org/sqlite"
)

type Store struct{ DB *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS schema_version(version INTEGER NOT NULL);
INSERT INTO schema_version SELECT 1 WHERE NOT EXISTS(SELECT 1 FROM schema_version);
CREATE TABLE IF NOT EXISTS jobs(id TEXT PRIMARY KEY, thread_key TEXT, data TEXT NOT NULL);
CREATE UNIQUE INDEX IF NOT EXISTS unique_thread ON jobs(thread_key) WHERE thread_key IS NOT NULL;
CREATE TABLE IF NOT EXISTS runs(id TEXT PRIMARY KEY, job_id TEXT NOT NULL, data TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS job_runs ON runs(job_id);
CREATE TABLE IF NOT EXISTS work_changes(id TEXT PRIMARY KEY,job_id TEXT NOT NULL,at INTEGER NOT NULL,data TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS job_work_changes ON work_changes(job_id,at);
CREATE TABLE IF NOT EXISTS run_usage_samples(id INTEGER PRIMARY KEY AUTOINCREMENT,run_id TEXT NOT NULL,at INTEGER NOT NULL,data TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS run_usage_history ON run_usage_samples(run_id,at);
CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY AUTOINCREMENT,event_key TEXT NOT NULL UNIQUE,job_id TEXT NOT NULL,run_id TEXT NOT NULL,method TEXT NOT NULL,payload TEXT NOT NULL,at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS job_events ON events(job_id,id);
CREATE TABLE IF NOT EXISTS actions(id TEXT PRIMARY KEY,job_id TEXT NOT NULL,kind TEXT NOT NULL,state TEXT NOT NULL,due INTEGER NOT NULL,data TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS pending_actions ON actions(state,due);
CREATE TABLE IF NOT EXISTS account_snapshots(account TEXT PRIMARY KEY,data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS quota_notifications(event_key TEXT PRIMARY KEY,account TEXT NOT NULL,at INTEGER NOT NULL,state TEXT NOT NULL,message TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS manager_decisions(id TEXT PRIMARY KEY,incident TEXT NOT NULL,at INTEGER NOT NULL,data TEXT NOT NULL);
`

// ReadOnly opens existing manager state without creating or migrating it.
func ReadOnly(dir string) (*Store, error) {
	db, err := catalog.OpenReadOnly(filepath.Join(dir, "state.sqlite"))
	if err != nil {
		return nil, err
	}
	return &Store{db}, nil
}

func Open(dir string) (*Store, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	p := filepath.Join(dir, "state.sqlite")
	f, e := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	db, e := sql.Open("sqlite", p)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	var existing int
	if e = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='schema_version'").Scan(&existing); e != nil {
		db.Close()
		return nil, e
	}
	if existing > 0 {
		var version int
		if e = db.QueryRow("SELECT version FROM schema_version").Scan(&version); e != nil || version != 1 {
			db.Close()
			return nil, fmt.Errorf("unsupported state schema %d; database left unchanged", version)
		}
	}
	if _, e = db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;" + schema); e != nil {
		db.Close()
		return nil, e
	}
	var v int
	if e = db.QueryRow("SELECT version FROM schema_version").Scan(&v); e != nil || v != 1 {
		db.Close()
		return nil, fmt.Errorf("unsupported state schema %d", v)
	}
	s := &Store{db}
	if e = s.migrateJournal(dir, existing > 0); e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func threadKey(j jobs.Job) any {
	if j.ThreadID == "" {
		return nil
	}
	return j.Host + "\x1f" + j.Store + "\x1f" + j.ThreadID
}
func putJob(tx *sql.Tx, j jobs.Job) error {
	b, e := json.Marshal(j)
	if e != nil {
		return e
	}
	_, e = tx.Exec("INSERT INTO jobs(id,thread_key,data) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET thread_key=excluded.thread_key,data=excluded.data", j.ID, threadKey(j), string(b))
	return e
}
func (s *Store) PutJob(j jobs.Job) error { return s.Commit(j, nil, nil) }
func (s *Store) Commit(j jobs.Job, ev *jobs.Event, actions []jobs.Action) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if ev != nil {
		r, e := tx.Exec("INSERT OR IGNORE INTO events(event_key,job_id,run_id,method,payload,at) VALUES(?,?,?,?,?,?)", ev.Key, j.ID, ev.RunID, ev.Method, string(ev.Payload), ev.At.UnixMilli())
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		if n == 0 {
			return tx.Commit()
		}
	}
	if e = putJob(tx, j); e != nil {
		return e
	}
	for _, a := range actions {
		if e = putAction(tx, a); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) Job(id string) (jobs.Job, error) {
	var b string
	var j jobs.Job
	e := s.DB.QueryRow("SELECT data FROM jobs WHERE id=?", id).Scan(&b)
	if e != nil {
		return j, e
	}
	e = json.Unmarshal([]byte(b), &j)
	return j, e
}
func (s *Store) Jobs() ([]jobs.Job, error) {
	r, e := s.DB.Query("SELECT data FROM jobs ORDER BY rowid")
	if e != nil {
		return nil, e
	}
	defer r.Close()
	out := []jobs.Job{}
	for r.Next() {
		var b string
		var j jobs.Job
		if e = r.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &j); e != nil {
			return nil, e
		}
		out = append(out, j)
	}
	return out, r.Err()
}
func putAction(tx *sql.Tx, a jobs.Action) error {
	b, e := json.Marshal(a)
	if e != nil {
		return e
	}
	_, e = tx.Exec("INSERT INTO actions(id,job_id,kind,state,due,data) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,due=excluded.due,data=excluded.data", a.ID, a.JobID, a.Kind, a.State, a.NotBefore.UnixMilli(), string(b))
	return e
}
func (s *Store) PutAction(a jobs.Action) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = putAction(tx, a); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Actions(state string) ([]jobs.Action, error) {
	q := "SELECT data FROM actions"
	args := []any{}
	if state != "" {
		q += " WHERE state=?"
		args = append(args, state)
	}
	q += " ORDER BY rowid"
	r, e := s.DB.Query(q, args...)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	out := []jobs.Action{}
	for r.Next() {
		var b string
		var a jobs.Action
		if e = r.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &a); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, r.Err()
}

func (s *Store) Action(id string) (jobs.Action, error) {
	var a jobs.Action
	var raw string
	err := s.DB.QueryRow("SELECT data FROM actions WHERE id=?", id).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &a)
	}
	return a, err
}
func (s *Store) RecoverActions() error {
	as, e := s.Actions("executing")
	if e != nil {
		return e
	}
	for _, a := range as {
		a.State = "outcome_unknown"
		a.Error = "supervisor restarted during dispatch; reconcile before retry"
		if e = s.PutAction(a); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) Events(jobID string, limit int) ([]jobs.Event, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	r, e := s.DB.Query("SELECT id,event_key,job_id,run_id,method,payload,at FROM events WHERE job_id=? ORDER BY id DESC LIMIT ?", jobID, limit)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	out := []jobs.Event{}
	for r.Next() {
		var ev jobs.Event
		var b string
		var at int64
		if e = r.Scan(&ev.ID, &ev.Key, &ev.JobID, &ev.RunID, &ev.Method, &b, &at); e != nil {
			return nil, e
		}
		ev.Payload = json.RawMessage(b)
		ev.At = time.UnixMilli(at)
		out = append(out, ev)
	}
	return out, r.Err()
}
func (s *Store) PutRun(r jobs.Run) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO runs(id,job_id,data) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", r.ID, r.JobID, string(b))
	return e
}
func (s *Store) Snapshot(account string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO account_snapshots(account,data) VALUES(?,?) ON CONFLICT(account) DO UPDATE SET data=excluded.data", account, string(b))
	return e
}
func (s *Store) Snapshots() ([]json.RawMessage, error) {
	r, e := s.DB.Query("SELECT data FROM account_snapshots")
	if e != nil {
		return nil, e
	}
	defer r.Close()
	out := []json.RawMessage{}
	for r.Next() {
		var b string
		if e = r.Scan(&b); e != nil {
			return nil, e
		}
		out = append(out, json.RawMessage(b))
	}
	return out, r.Err()
}
func (s *Store) Decision(id, incident string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO manager_decisions(id,incident,at,data) VALUES(?,?,?,?)", id, incident, time.Now().UnixMilli(), string(b))
	return e
}
func (s *Store) DecisionCounts(incident string) (int, int, error) {
	var hour, n int
	e := s.DB.QueryRow("SELECT count(*) FROM manager_decisions WHERE at>?", time.Now().Add(-time.Hour).UnixMilli()).Scan(&hour)
	if e != nil {
		return 0, 0, e
	}
	e = s.DB.QueryRow("SELECT count(*) FROM manager_decisions WHERE incident=?", incident).Scan(&n)
	return hour, n, e
}
func (s *Store) UpdateDecision(id string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("UPDATE manager_decisions SET data=? WHERE id=?", string(b), id)
	return e
}
func (s *Store) HasPending() bool {
	var n int
	return s.DB.QueryRow("SELECT count(*) FROM actions WHERE state IN ('pending','executing','outcome_unknown','manager_wait')").Scan(&n) == nil && n > 0
}
func NotFound(e error) bool { return errors.Is(e, sql.ErrNoRows) }
