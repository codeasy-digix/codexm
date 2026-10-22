package tui

import (
	"reflect"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/jobs"
)

func TestLogFiltersKeepOverlappingRunsAndFilterChangesByReason(t *testing.T) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-time.Hour)
	m := managementTestModel()
	m.LogReport = jobs.WorkLog{At: now, Runs: []jobs.Run{
		{ID: "ongoing-old-start", JobID: "a", Account: "account-work", StartedAt: old},
		{ID: "ended-old", JobID: "a", Account: "account-work", StartedAt: old, EndedAt: &old},
		{ID: "other-account", JobID: "a", Account: "account-secondary", StartedAt: recent},
		{ID: "other-job", JobID: "b", Account: "account-work", StartedAt: recent},
	}, Changes: []jobs.WorkChange{
		{ID: "match", JobID: "a", From: "account-work", To: "account-secondary", At: recent, Reason: "Quota recovered"},
		{ID: "wrong-reason", JobID: "a", From: "account-secondary", To: "account-work", At: recent, Reason: "Manual switch"},
		{ID: "old", JobID: "a", From: "account-work", To: "account-secondary", At: old, Reason: "Quota recovered"},
		{ID: "other-job", JobID: "b", From: "account-work", To: "account-secondary", At: recent, Reason: "Quota recovered"},
	}}
	original := m.LogReport
	m.LogsJob = "a"
	m.LogFilter = logFilter{Account: "account-work", Days: 1, Reason: "quota"}
	filtered := m.filteredLogs()
	if len(filtered.Runs) != 1 || filtered.Runs[0].ID != "ongoing-old-start" || len(filtered.Changes) != 1 || filtered.Changes[0].ID != "match" {
		t.Fatal("scope, account, recent activity or change-reason semantics regressed")
	}
	if !reflect.DeepEqual(original, m.LogReport) {
		t.Fatal("filtering mutated cached unfiltered records")
	}
	m.LogsJob = ""
	m.LogFilter = logFilter{}
	all := m.filteredLogs()
	if len(all.Runs) != 4 || len(all.Changes) != 4 {
		t.Fatal("clearing filters did not restore all records")
	}
}
