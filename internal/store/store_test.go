package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFutureSchemaIsRejectedWithoutRewritingDatabase(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.sqlite")
	db, e := sql.Open("sqlite", p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("CREATE TABLE schema_version(version INTEGER NOT NULL); INSERT INTO schema_version VALUES(99)"); e != nil {
		t.Fatal(e)
	}
	db.Close()
	before, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	if s, e := Open(dir); e == nil {
		s.Close()
		t.Fatal("future schema opened")
	}
	after, e := os.ReadFile(p)
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("future database was modified", e)
	}
}

func TestEventAndDispatchPersistTogetherAcrossRestart(t *testing.T) {
	d := t.TempDir()
	s, e := Open(d)
	if e != nil {
		t.Fatal(e)
	}
	j := jobs.Job{ID: "a", Host: "local", Store: "/tmp/codex", ThreadID: "thread", State: "quota_wait"}
	ev := jobs.Event{Key: "event1", RunID: "run1", Method: "error", Payload: json.RawMessage(`{}`), At: time.Now()}
	a := jobs.Action{ID: "switch1", JobID: j.ID, Kind: "switch_account", State: "pending", Payload: json.RawMessage(`null`)}
	if e = s.Commit(j, &ev, []jobs.Action{a}); e != nil {
		t.Fatal(e)
	}
	j.State = "wrong"
	if e = s.Commit(j, &ev, []jobs.Action{a}); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(d)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	got, _ := s.Job("a")
	if got.State != "quota_wait" {
		t.Fatal("duplicate event rewrote state")
	}
	as, _ := s.Actions("pending")
	if len(as) != 1 {
		t.Fatal("dispatch lost or duplicated")
	}
	as[0].State = "executing"
	s.PutAction(as[0])
	s.RecoverActions()
	as, _ = s.Actions("outcome_unknown")
	if len(as) != 1 {
		t.Fatal("uncertain dispatch was automatically replayed")
	}
}
func TestHostStoreThreadIdentityAndAtomicRollback(t *testing.T) {
	s, _ := Open(t.TempDir())
	defer s.Close()
	j := jobs.Job{ID: "a", Host: "local", Store: "one", ThreadID: "thread"}
	s.PutJob(j)
	j.ID = "b"
	if s.PutJob(j) == nil {
		t.Fatal("duplicate thread accepted")
	}
	j.Store = "two"
	if e := s.PutJob(j); e != nil {
		t.Fatal(e)
	}
	j.ID = "a"
	j.Store = "two"
	ev := jobs.Event{Key: "bad", Method: "event", Payload: json.RawMessage(`{}`), At: time.Now()}
	if s.Commit(j, &ev, nil) == nil {
		t.Fatal("identity collision accepted")
	}
	es, _ := s.Events("a", 10)
	if len(es) != 0 {
		t.Fatal("event escaped failed transaction")
	}
}
