package store

import (
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/jobs"
)

func TestRunHistoryPreservesStartAndUsageAcrossEndAndRestart(t *testing.T) {
	dir := t.TempDir()
	s, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	start := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Millisecond)
	r := jobs.Run{ID: "run", JobID: "job", Account: "account-work", StartedAt: start, State: "starting"}
	if e = s.PutRun(r); e != nil {
		t.Fatal(e)
	}
	reset := time.Now().Add(time.Hour).Unix()
	for i, percent := range []int{0, 7} {
		u := jobs.AccountUsage{At: start.Add(time.Duration(i) * time.Minute), Primary: &jobs.UsageWindow{UsedPercent: percent, ResetsAt: &reset}}
		if e = s.SampleRun(r.ID, u); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.IdentifyRun(r.ID, "thread"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	end := start.Add(2 * time.Minute)
	if e = s.EndRun(r.ID, "ended", end); e != nil {
		t.Fatal(e)
	}
	if e = s.EndRun(r.ID, "wrong", end.Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	if e = s.SampleRun(r.ID, jobs.AccountUsage{At: end.Add(time.Hour)}); e != nil {
		t.Fatal(e)
	}
	got, e := s.Run(r.ID)
	if e != nil {
		t.Fatal(e)
	}
	if !got.StartedAt.Equal(start) || got.EndedAt == nil || !got.EndedAt.Equal(end) || got.State != "ended" || got.ThreadID != "thread" {
		t.Fatalf("run metadata changed: %+v", got)
	}
	if got.UsageStart == nil || got.UsageStart.Primary.UsedPercent != 0 || got.UsageLatest.Primary.UsedPercent != 7 {
		t.Fatal("usage history lost")
	}
	var n int
	if e = s.DB.QueryRow("SELECT count(*) FROM run_usage_samples WHERE run_id=?", r.ID).Scan(&n); e != nil || n != 2 {
		t.Fatal("late sample added", n, e)
	}
}

func TestAccountChangeIsAtomicAndLogsFilterByJob(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	j := jobs.Job{ID: "one", Account: "account-work", ThreadID: "thread"}
	if e = s.PutJob(j); e != nil {
		t.Fatal(e)
	}
	c := jobs.WorkChange{ID: "change", JobID: j.ID, ThreadID: j.ThreadID, From: "account-work", To: "account-secondary", Reason: "quota", At: time.Now()}
	j.Account = "account-secondary"
	if e = s.ChangeAccount(j, c); e != nil {
		t.Fatal(e)
	}
	j.Account = "account-main"
	if e = s.ChangeAccount(j, c); e == nil {
		t.Fatal("duplicate change should rollback")
	}
	got, _ := s.Job(j.ID)
	if got.Account != "account-secondary" {
		t.Fatal("partial account update")
	}
	for _, id := range []string{"one", "two"} {
		if e = s.PutRun(jobs.Run{ID: id, JobID: id, StartedAt: time.Now()}); e != nil {
			t.Fatal(e)
		}
	}
	log, e := s.WorkLogs("one", 10)
	if e != nil || len(log.Runs) != 1 || len(log.Changes) != 1 || log.Changes[0].From != "account-work" || log.Runs[0].ThreadID != "thread" {
		t.Fatal(log, e)
	}
	log, e = s.WorkLogs("two", 10)
	if e != nil || len(log.Runs) != 1 || len(log.Changes) != 0 {
		t.Fatal(log, e)
	}
}
