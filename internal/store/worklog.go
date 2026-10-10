package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/codeasy-digix/codexm/internal/jobs"
)

func (s *Store) Run(id string) (jobs.Run, error) {
	var b string
	var r jobs.Run
	e := s.DB.QueryRow("SELECT data FROM runs WHERE id=?", id).Scan(&b)
	if e != nil {
		return r, e
	}
	e = json.Unmarshal([]byte(b), &r)
	return r, e
}

// updateRun preserves the original start and usage history even when the
// runtime ends after a supervisor restart.
func (s *Store) updateRun(id string, update func(*jobs.Run)) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var b string
	if e = tx.QueryRow("SELECT data FROM runs WHERE id=?", id).Scan(&b); e != nil {
		return e
	}
	var r jobs.Run
	if e = json.Unmarshal([]byte(b), &r); e != nil {
		return e
	}
	update(&r)
	raw, e := json.Marshal(r)
	if e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE runs SET data=? WHERE id=?", string(raw), id); e != nil {
		return e
	}
	return tx.Commit()
}

func (s *Store) EndRun(id, state string, at time.Time) error {
	return s.updateRun(id, func(r *jobs.Run) {
		if r.EndedAt == nil {
			r.EndedAt = &at
			r.State = state
		}
	})
}

func (s *Store) IdentifyRun(id, thread string) error {
	return s.updateRun(id, func(r *jobs.Run) { r.ThreadID = thread; r.State = "connected" })
}

func (s *Store) SampleRun(id string, usage jobs.AccountUsage) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var b string
	if e = tx.QueryRow("SELECT data FROM runs WHERE id=?", id).Scan(&b); e != nil {
		return e
	}
	var r jobs.Run
	if e = json.Unmarshal([]byte(b), &r); e != nil {
		return e
	}
	if r.EndedAt != nil || r.UsageLatest != nil && !usage.At.After(r.UsageLatest.At) {
		return nil
	}
	if r.UsageStart == nil {
		r.UsageStart = &usage
	}
	r.UsageLatest = &usage
	raw, e := json.Marshal(r)
	if e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE runs SET data=? WHERE id=?", string(raw), id); e != nil {
		return e
	}
	raw, e = json.Marshal(usage)
	if e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT INTO run_usage_samples(run_id,at,data) VALUES(?,?,?)", id, usage.At.UnixMilli(), string(raw)); e != nil {
		return e
	}
	return tx.Commit()
}

// ChangeAccount commits the selected account and its log as one mutation.
func (s *Store) ChangeAccount(j jobs.Job, c jobs.WorkChange) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = putJob(tx, j); e != nil {
		return e
	}
	raw, e := json.Marshal(c)
	if e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT INTO work_changes(id,job_id,at,data) VALUES(?,?,?,?)", c.ID, j.ID, c.At.UnixMilli(), string(raw)); e != nil {
		return e
	}
	return tx.Commit()
}

func (s *Store) WorkLogs(jobID string, limit int) (jobs.WorkLog, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	report := jobs.WorkLog{At: time.Now(), Note: "실행시간에는 대기가 포함됩니다. %는 해당 계정 전체 한도의 관측값이며 세션별 사용량이 아닙니다. 갱신된 한도 구간은 증가량을 계산하지 않습니다.", Runs: []jobs.Run{}, Changes: []jobs.WorkChange{}}
	q, args := "SELECT data FROM runs", []any{}
	if jobID != "" {
		q += " WHERE job_id=?"
		args = append(args, jobID)
	}
	q += " ORDER BY rowid DESC LIMIT ?"
	args = append(args, limit)
	rows, e := s.DB.Query(q, args...)
	if e != nil {
		return report, e
	}
	for rows.Next() {
		var b string
		var r jobs.Run
		if e = rows.Scan(&b); e == nil {
			e = json.Unmarshal([]byte(b), &r)
		}
		if e != nil {
			break
		}
		report.Runs = append(report.Runs, r)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return report, e
	}
	// Legacy runs did not carry a thread ID. Do not change their saved history.
	for i := range report.Runs {
		if report.Runs[i].ThreadID == "" {
			j, err := s.Job(report.Runs[i].JobID)
			if err == nil {
				report.Runs[i].ThreadID = j.ThreadID
			} else if err != sql.ErrNoRows {
				return report, err
			}
		}
	}
	q, args = "SELECT data FROM work_changes", []any{}
	if jobID != "" {
		q += " WHERE job_id=?"
		args = append(args, jobID)
	}
	q += " ORDER BY at DESC, rowid DESC LIMIT ?"
	args = append(args, limit)
	rows, e = s.DB.Query(q, args...)
	if e != nil {
		return report, e
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		var c jobs.WorkChange
		if e = rows.Scan(&b); e == nil {
			e = json.Unmarshal([]byte(b), &c)
		}
		if e != nil {
			return report, e
		}
		report.Changes = append(report.Changes, c)
	}
	return report, rows.Err()
}
