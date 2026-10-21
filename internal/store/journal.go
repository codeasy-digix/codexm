package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/codeasy-digix/codexm/internal/jobs"
)

// The additive web schema leaves schema_version=1 readable by older managers.
// Triggers also observe writes made by those binaries, in the writer transaction.
func (s *Store) migrateJournal(dir string, existing bool) error {
	var installed int
	if err := s.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='web_schema'").Scan(&installed); err != nil {
		return err
	}
	if installed != 0 {
		var v int
		if err := s.DB.QueryRow("SELECT version FROM web_schema").Scan(&v); err != nil {
			return err
		}
		if v != 1 {
			return fmt.Errorf("unsupported web state schema %d", v)
		}
		return s.journalRetention()
	}
	if existing {
		// SQLite includes committed WAL records; copying the main file would not.
		backup := filepath.Join(dir, "state-before-web-"+jobs.ID()+".sqlite")
		if _, err := s.DB.Exec("VACUUM INTO ?", backup); err != nil {
			return fmt.Errorf("state backup: %w", err)
		}
		if err := os.Chmod(backup, 0600); err != nil {
			return err
		}
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
CREATE TABLE web_schema(version INTEGER NOT NULL, instance TEXT NOT NULL);
CREATE TABLE journal(sequence INTEGER PRIMARY KEY AUTOINCREMENT, entity TEXT NOT NULL, entity_id TEXT NOT NULL, change TEXT NOT NULL, at INTEGER NOT NULL);
CREATE TABLE observed_revisions(source TEXT PRIMARY KEY, revision TEXT NOT NULL);
CREATE TABLE operations(id TEXT PRIMARY KEY, request_hash TEXT NOT NULL, command TEXT NOT NULL, state TEXT NOT NULL, result TEXT, error TEXT NOT NULL DEFAULT '', created INTEGER NOT NULL);
`)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO web_schema VALUES(1,?)", jobs.ID()); err != nil {
		return err
	}
	for table, key := range map[string]string{"jobs": "id", "runs": "id", "actions": "id", "events": "id", "account_snapshots": "account", "work_changes": "id", "observed_revisions": "source", "operations": "id"} {
		for _, change := range []string{"INSERT", "UPDATE", "DELETE"} {
			row := "NEW"
			if change == "DELETE" {
				row = "OLD"
			}
			q := fmt.Sprintf(`CREATE TRIGGER journal_%s_%s AFTER %s ON %s BEGIN INSERT INTO journal(entity,entity_id,change,at) VALUES('%s',%s.%s,'%s',CAST(unixepoch('subsec')*1000 AS INTEGER)); END`, table, change, change, table, table, row, key, change)
			if _, err = tx.Exec(q); err != nil {
				return err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return s.journalRetention()
}

// Invalidations are disposable; operation IDs and work history are not. Replay
// detects a retained-prefix gap and asks the subscriber for a new snapshot.
func (s *Store) journalRetention() error {
	_, err := s.DB.Exec(`CREATE TRIGGER IF NOT EXISTS journal_retention AFTER INSERT ON journal WHEN NEW.sequence % 256 = 0 BEGIN DELETE FROM journal WHERE sequence <= NEW.sequence - 50000; END`)
	return err
}

type JournalEntry struct {
	Sequence int64  `json:"sequence"`
	Entity   string `json:"entity"`
	ID       string `json:"id"`
	Change   string `json:"change"`
	At       int64  `json:"at"`
}
type Snapshot struct {
	Instance string            `json:"instance"`
	Sequence int64             `json:"sequence"`
	Jobs     []jobs.Job        `json:"jobs"`
	Accounts []json.RawMessage `json:"accounts"`
	Actions  []jobs.Action     `json:"actions"`
}
type Replay struct {
	Instance string         `json:"instance"`
	Sequence int64          `json:"sequence"`
	Entries  []JournalEntry `json:"entries"`
	Resync   bool           `json:"resync"`
	More     bool           `json:"more"`
}

func journalHead(tx *sql.Tx) (instance string, sequence int64, err error) {
	err = tx.QueryRow("SELECT instance FROM web_schema").Scan(&instance)
	if err == nil {
		err = tx.QueryRow("SELECT COALESCE((SELECT seq FROM sqlite_sequence WHERE name='journal'),0)").Scan(&sequence)
	}
	return
}

func (s *Store) ConsistentSnapshot() (Snapshot, error) {
	var out Snapshot
	tx, err := s.DB.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	out.Instance, out.Sequence, err = journalHead(tx)
	if err != nil {
		return out, err
	}
	read := func(query string, consume func([]byte) error) error {
		r, e := tx.Query(query)
		if e != nil {
			return e
		}
		defer r.Close()
		for r.Next() {
			var data []byte
			if e = r.Scan(&data); e != nil {
				return e
			}
			if e = consume(data); e != nil {
				return e
			}
		}
		return r.Err()
	}
	out.Jobs = []jobs.Job{}
	out.Accounts = []json.RawMessage{}
	out.Actions = []jobs.Action{}
	err = read("SELECT data FROM jobs ORDER BY rowid", func(b []byte) error {
		var j jobs.Job
		e := json.Unmarshal(b, &j)
		out.Jobs = append(out.Jobs, j)
		return e
	})
	if err != nil {
		return out, err
	}
	err = read("SELECT data FROM account_snapshots ORDER BY account", func(b []byte) error { out.Accounts = append(out.Accounts, json.RawMessage(b)); return nil })
	if err != nil {
		return out, err
	}
	err = read("SELECT data FROM actions WHERE state NOT IN ('done','cancelled') ORDER BY rowid", func(b []byte) error {
		var a jobs.Action
		e := json.Unmarshal(b, &a)
		out.Actions = append(out.Actions, a)
		return e
	})
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func (s *Store) Replay(instance string, after int64, limit int) (Replay, error) {
	out := Replay{Entries: []JournalEntry{}}
	tx, err := s.DB.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	out.Instance, out.Sequence, err = journalHead(tx)
	if err != nil {
		return out, err
	}
	var first sql.NullInt64
	if err = tx.QueryRow("SELECT min(sequence) FROM journal").Scan(&first); err != nil {
		return out, err
	}
	if instance != out.Instance || after < 0 || after > out.Sequence || first.Valid && after < first.Int64-1 || !first.Valid && after < out.Sequence {
		out.Resync = true
		return out, nil
	}
	if limit < 1 || limit > 500 {
		limit = 500
	}
	r, err := tx.Query("SELECT sequence,entity,entity_id,change,at FROM journal WHERE sequence>? ORDER BY sequence LIMIT ?", after, limit)
	if err != nil {
		return out, err
	}
	defer r.Close()
	for r.Next() {
		var e JournalEntry
		if err = r.Scan(&e.Sequence, &e.Entity, &e.ID, &e.Change, &e.At); err != nil {
			return out, err
		}
		out.Entries = append(out.Entries, e)
	}
	if err = r.Err(); err != nil {
		return out, err
	}
	if len(out.Entries) > 0 {
		out.More = out.Entries[len(out.Entries)-1].Sequence < out.Sequence
	}
	return out, nil
}

func (s *Store) ObserveRevision(source, revision string) error {
	_, err := s.DB.Exec("INSERT INTO observed_revisions(source,revision) VALUES(?,?) ON CONFLICT(source) DO UPDATE SET revision=excluded.revision WHERE revision<>excluded.revision", source, revision)
	return err
}

type Operation struct {
	ID      string          `json:"id"`
	Hash    string          `json:"-"`
	Command string          `json:"command"`
	State   string          `json:"state"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   string          `json:"error,omitempty"`
}

func (s *Store) ClaimOperation(id, hash, command string) (Operation, bool, error) {
	r, err := s.DB.Exec("INSERT OR IGNORE INTO operations(id,request_hash,command,state,created) VALUES(?,?,?,'pending',?)", id, hash, command, time.Now().UnixMilli())
	if err != nil {
		return Operation{}, false, err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return Operation{}, false, err
	}
	op, err := s.Operation(id)
	return op, n == 1, err
}
func (s *Store) Operation(id string) (Operation, error) {
	var op Operation
	var result sql.NullString
	err := s.DB.QueryRow("SELECT id,request_hash,command,state,result,error FROM operations WHERE id=?", id).Scan(&op.ID, &op.Hash, &op.Command, &op.State, &result, &op.Error)
	if result.Valid {
		op.Result = json.RawMessage(result.String)
	}
	return op, err
}
func (s *Store) FinishOperation(id string, result json.RawMessage, failure string) error {
	state := "done"
	if failure != "" {
		state = "failed"
	}
	_, err := s.DB.Exec("UPDATE operations SET state=?,result=?,error=? WHERE id=? AND state='pending'", state, string(result), failure, id)
	return err
}
func (s *Store) RecoverOperations() error {
	_, err := s.DB.Exec("UPDATE operations SET state='outcome_unknown' WHERE state='pending'")
	return err
}

func (s *Store) UnknownOperation(id, reason string) error {
	_, err := s.DB.Exec("UPDATE operations SET state='outcome_unknown',error=? WHERE id=? AND state='pending'", reason, id)
	return err
}
