package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/activity"
	"github.com/codeasy-digix/codexm/internal/jobs"
)

type logFilter struct {
	Account, Reason string
	Days            int
}

func (m Model) filteredLogs() jobs.WorkLog {
	r := m.LogReport
	r.Runs, r.Changes = nil, nil
	now := r.At
	if now.IsZero() {
		now = time.Now()
	}
	from := time.Time{}
	if m.LogFilter.Days > 0 {
		from = now.Add(-time.Duration(m.LogFilter.Days) * 24 * time.Hour)
	}
	for _, run := range m.LogReport.Runs {
		if m.LogsJob != "" && run.JobID != m.LogsJob {
			continue
		}
		end := now
		if run.EndedAt != nil {
			end = *run.EndedAt
		}
		if m.LogFilter.Account != "" && run.Account != m.LogFilter.Account || !from.IsZero() && end.Before(from) {
			continue
		}
		r.Runs = append(r.Runs, run)
	}
	for _, change := range m.LogReport.Changes {
		if m.LogsJob != "" && change.JobID != m.LogsJob {
			continue
		}
		if m.LogFilter.Account != "" && change.From != m.LogFilter.Account && change.To != m.LogFilter.Account || !from.IsZero() && change.At.Before(from) || m.LogFilter.Reason != "" && !strings.Contains(strings.ToLower(change.Reason), strings.ToLower(m.LogFilter.Reason)) {
			continue
		}
		r.Changes = append(r.Changes, change)
	}
	return r
}

func (m Model) logsText() string {
	if !m.LogsOpen {
		if m.DetailText != "" {
			return m.DetailText
		}
		return "작업로그 · 전체: /logs all · 선택 작업: /logs\n이 범위의 로그를 불러오세요."
	}
	r := m.filteredLogs()
	scope := "전체 세션"
	if m.LogsJob != "" {
		scope = "선택한 작업 " + m.jobTitle(m.LogsJob)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "작업로그 · %s\n", scope)
	account := "모든 계정"
	if m.LogFilter.Account != "" {
		account = m.LogFilter.Account
	}
	period := "모든 기간"
	if m.LogFilter.Days > 0 {
		period = fmt.Sprintf("최근 %d일", m.LogFilter.Days)
	}
	fmt.Fprintf(&b, "필터: %s · %s · 변경 사유 %s\n", account, period, m.LogFilter.Reason)
	b.WriteString("실행 시간에는 대기가 포함됩니다. 한도는 계정 전체 관측값입니다.\n작업별 정확한 소비율로 환산하지 않습니다.\n\n")
	if !m.LogsOpen {
		b.WriteString("로그를 불러오세요: /logs all 또는 /logs\n")
	}
	now := r.At
	if now.IsZero() {
		now = time.Now()
	}
	for _, run := range r.Runs {
		end := now
		state := "진행 중"
		if run.EndedAt != nil {
			end = *run.EndedAt
			state = run.State
		}
		duration := max(time.Duration(0), end.Sub(run.StartedAt)).Truncate(time.Second)
		var before, after *jobs.UsageWindow
		if run.UsageStart != nil {
			before = run.UsageStart.Primary
		}
		if run.UsageLatest != nil {
			after = run.UsageLatest.Primary
		}
		fmt.Fprintf(&b, "%s · %s · %s\n  %s · %s · 단기 한도 사용 %s\n", m.jobTitle(run.JobID), run.Account, state, run.StartedAt.Local().Format("01-02 15:04:05"), duration, activity.Window(before, after))
		var secondaryBefore, secondaryAfter *jobs.UsageWindow
		checked := "미측정"
		if run.UsageStart != nil {
			secondaryBefore = run.UsageStart.Secondary
		}
		if run.UsageLatest != nil {
			secondaryAfter = run.UsageLatest.Secondary
			if !run.UsageLatest.At.IsZero() {
				checked = run.UsageLatest.At.Local().Format("01-02 15:04:05")
			}
		}
		fmt.Fprintf(&b, "  장기 한도 사용 %s · 마지막 관측 %s\n", activity.Window(secondaryBefore, secondaryAfter), checked)
	}
	if len(r.Runs) == 0 {
		b.WriteString("이 조건의 실행 기록이 없습니다.\n")
	}
	b.WriteString("\n계정 변경\n")
	for _, change := range r.Changes {
		fmt.Fprintf(&b, "%s · %s → %s\n  %s · %s\n", change.At.Local().Format("01-02 15:04:05"), change.From, change.To, m.jobTitle(change.JobID), change.Reason)
	}
	if len(r.Changes) == 0 {
		b.WriteString("이 조건의 계정 변경이 없습니다.\n")
	}
	return b.String()
}

func (m Model) jobTitle(id string) string {
	for _, j := range m.Status.Jobs {
		if j.ID == id && j.Title != "" {
			return j.Title
		}
	}
	return id
}
