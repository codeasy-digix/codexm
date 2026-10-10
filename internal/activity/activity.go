package activity

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/jobs"
)

func Usage(s accounts.Snapshot, model string) jobs.AccountUsage {
	u := jobs.AccountUsage{At: s.CheckedAt}
	if s.Error != "" || !s.Authenticated {
		u.Error = "한도 확인 실패"
		return u
	}
	b := s.Single
	if x, ok := s.Buckets["codex"]; ok {
		b = x
	}
	for _, x := range s.Buckets {
		if model != "" && x.NormalModel == model {
			b = x
			break
		}
	}
	u.LimitID = b.ID
	window := func(w *accounts.Window) *jobs.UsageWindow {
		if w == nil {
			return nil
		}
		return &jobs.UsageWindow{UsedPercent: w.UsedPercent, Minutes: w.Minutes, ResetsAt: w.ResetsAt}
	}
	u.Primary, u.Secondary = window(b.Primary), window(b.Secondary)
	return u
}

func Window(start, end *jobs.UsageWindow) string {
	value := func(w *jobs.UsageWindow) string {
		if w == nil {
			return "?"
		}
		return fmt.Sprintf("%d%%", w.UsedPercent)
	}
	r := value(start) + "→" + value(end)
	if start == nil || end == nil {
		return r
	}
	if start.ResetsAt == nil || end.ResetsAt == nil {
		return r
	}
	if *start.ResetsAt != *end.ResetsAt || start.Minutes != nil && end.Minutes != nil && *start.Minutes != *end.Minutes || end.UsedPercent < start.UsedPercent {
		return r + " (갱신)"
	}
	return fmt.Sprintf("%s (+%d%%p)", r, end.UsedPercent-start.UsedPercent)
}

func duration(r jobs.Run, now time.Time) string {
	if r.StartedAt.IsZero() {
		return "알 수 없음"
	}
	end := now
	if r.EndedAt != nil {
		end = *r.EndedAt
	}
	d := end.Sub(r.StartedAt).Truncate(time.Second)
	if d < 0 {
		d = 0
	}
	return d.String()
}

func short(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
func clock(t time.Time) string {
	if t.IsZero() {
		return "알 수 없음"
	}
	return t.Local().Format("01-02 15:04:05")
}

func Format(report jobs.WorkLog) string {
	var b strings.Builder
	b.WriteString("작업로그 · " + clock(report.At) + "\n" + report.Note + "\n\n")
	w := tabwriter.NewWriter(&b, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "세션 / 작업\t계정\t시작\t실행시간\t기본 한도 사용률\t보조 한도 사용률\t상태")
	for _, r := range report.Runs {
		var sp, ss, ep, es *jobs.UsageWindow
		if r.UsageStart != nil {
			sp, ss = r.UsageStart.Primary, r.UsageStart.Secondary
		}
		if r.UsageLatest != nil {
			ep, es = r.UsageLatest.Primary, r.UsageLatest.Secondary
		}
		fmt.Fprintf(w, "%s / %s\t%s\t%s\t%s\t%s\t%s\t%s\n", short(r.ThreadID, 8), short(r.JobID, 8), r.Account, clock(r.StartedAt), duration(r, report.At), Window(sp, ep), Window(ss, es), r.State)
	}
	if len(report.Runs) == 0 {
		fmt.Fprintln(w, "아직 실행 기록이 없습니다.")
	}
	w.Flush()
	b.WriteString("\n계정 변경 / 인계\n")
	for _, c := range report.Changes {
		fmt.Fprintf(&b, "%s · %s / %s · %s → %s · %s\n", clock(c.At), short(c.ThreadID, 8), short(c.JobID, 8), c.From, c.To, c.Reason)
	}
	if len(report.Changes) == 0 {
		io.WriteString(&b, "기록 없음\n")
	}
	return b.String()
}
