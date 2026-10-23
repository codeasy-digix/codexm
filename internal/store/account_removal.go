package store

import (
	"encoding/json"

	"github.com/codeasy-digix/codexm/internal/jobs"
)

// CommitAccountRemoval atomically saves detached assignments, handoff intent,
// superseded selections and audit records in codexm's own management database.
func (s *Store) CommitAccountRemoval(updates []jobs.Job, changes []jobs.WorkChange, actions []jobs.Action) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, j := range updates {
		if err = putJob(tx, j); err != nil {
			return err
		}
	}
	for _, c := range changes {
		raw, e := json.Marshal(c)
		if e != nil {
			return e
		}
		if _, err = tx.Exec("INSERT INTO work_changes(id,job_id,at,data) VALUES(?,?,?,?)", c.ID, c.JobID, c.At.UnixMilli(), string(raw)); err != nil {
			return err
		}
	}
	for _, a := range actions {
		if err = putAction(tx, a); err != nil {
			return err
		}
	}
	return tx.Commit()
}
