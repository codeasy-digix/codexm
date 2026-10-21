package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"path/filepath"
	"sync"
	"testing"
)

func TestJournalAtomicReplayAndRetention(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	first, err := s.ConsistentSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	j := jobs.Job{ID: "j", ThreadID: "thread", Host: "host", Store: "store"}
	for i := range 150 {
		j.State = fmt.Sprint(i)
		if err = s.PutJob(j); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := s.ConsistentSnapshot()
	if err != nil || len(snap.Jobs) != 1 || snap.Sequence != 150 {
		t.Fatal(snap, err)
	}
	// A failed uniqueness write must not advance the journal.
	j.ID = "collision"
	if s.PutJob(j) == nil {
		t.Fatal("collision accepted")
	}
	next, _ := s.ConsistentSnapshot()
	if next.Sequence != snap.Sequence {
		t.Fatal("rolled-back change published")
	}
	replay, err := s.Replay(first.Instance, 0, 100)
	if err != nil || replay.Resync || len(replay.Entries) != 100 || !replay.More {
		t.Fatal(replay, err)
	}
	replay, err = s.Replay(first.Instance, 100, 100)
	if err != nil || len(replay.Entries) != 50 || replay.More {
		t.Fatal(replay, err)
	}
	if _, err = s.DB.Exec("DELETE FROM journal WHERE sequence<=100"); err != nil {
		t.Fatal(err)
	}
	replay, _ = s.Replay(first.Instance, 0, 100)
	if !replay.Resync {
		t.Fatal("retention gap missed")
	}
	replay, _ = s.Replay("replaced database", 150, 100)
	if !replay.Resync {
		t.Fatal("replacement missed")
	}
}
func TestJournalMigratesWALBackupAndOlderWrites(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("PRAGMA journal_mode=WAL;" + schema); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(jobs.Job{ID: "old"})
	if _, err = db.Exec("INSERT INTO jobs(id,data)VALUES('old',?)", string(b)); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	defer db.Close()
	files, _ := filepath.Glob(filepath.Join(dir, "state-before-web-*.sqlite"))
	if len(files) != 1 {
		t.Fatal(files)
	}
	backup, err := sql.Open("sqlite", files[0])
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var n int
	if err = backup.QueryRow("SELECT count(*) FROM jobs").Scan(&n); err != nil || n != 1 {
		t.Fatal("invalid WAL backup", n, err)
	}
	if _, err = db.Exec("UPDATE jobs SET data=data WHERE id='old'"); err != nil {
		t.Fatal(err)
	}
	snap, err := s.ConsistentSnapshot()
	if err != nil || snap.Sequence != 1 {
		t.Fatal("older writer invisible", snap, err)
	}
}
func TestConcurrentOperationClaimAndCrashRecovery(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var wg sync.WaitGroup
	wins := make(chan bool, 20)
	for range 20 {
		wg.Go(func() {
			_, won, e := s.ClaimOperation("op", "hash", "send")
			if e != nil {
				t.Error(e)
			}
			wins <- won
		})
	}
	wg.Wait()
	close(wins)
	n := 0
	for won := range wins {
		if won {
			n++
		}
	}
	if n != 1 {
		t.Fatal(n)
	}
	if err = s.RecoverOperations(); err != nil {
		t.Fatal(err)
	}
	op, won, err := s.ClaimOperation("op", "hash", "send")
	if err != nil || won || op.State != "outcome_unknown" {
		t.Fatal(op, won, err)
	}
}
