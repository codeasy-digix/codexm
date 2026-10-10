// Package display renders saved observations without running a model or exposing
// raw transcripts, goal objectives, credentials or manager prompts.
package display

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/activity"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/manager"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

type UsageReport struct {
	Manager []manager.Record `json:"manager"`
	Workers []struct {
		JobID    string          `json:"job_id"`
		ThreadID string          `json:"thread_id"`
		Account  string          `json:"account"`
		Tokens   json.RawMessage `json:"reported_usage"`
		Goal     *jobs.Goal      `json:"goal"`
	} `json:"workers"`
}

type tokenCounts struct {
	Total  *int64 `json:"totalTokens"`
	Input  *int64 `json:"inputTokens"`
	Cached *int64 `json:"cachedInputTokens"`
	Output *int64 `json:"outputTokens"`
}
type tokenUsage struct {
	Total  *tokenCounts `json:"total"`
	Last   *tokenCounts `json:"last"`
	Window *int64       `json:"modelContextWindow"`
}

func Number(n int64) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
func count(n *int64) string {
	if n == nil {
		return "미측정"
	}
	return Number(*n)
}
func timeText(t time.Time) string {
	if t.IsZero() {
		return "미확인"
	}
	return t.Local().Format("01-02 15:04:05")
}
func fallback(s string) string {
	if s == "" {
		return "미확인"
	}
	return s
}
func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return fallback(s)
}
func State(s string) string {
	labels := map[string]string{"connected": "연결됨", "running": "실행 중", "starting": "시작 중", "idle": "입력 대기", "complete": "완료", "paused": "일시 중지", "stopped": "종료", "observed": "열람", "queued": "대기열", "quota_wait": "한도 회복 대기", "waiting_on_user": "응답 대기", "waiting_on_approval": "승인 대기", "available": "사용 가능", "candidate": "잔여 한도 있음", "exhausted": "한도 소진", "unknown": "미확인"}
	if label, ok := labels[s]; ok {
		return label
	}
	return fallback(s)
}

func Tokens(raw json.RawMessage) string {
	var u tokenUsage
	if json.Unmarshal(raw, &u) != nil || u.Total == nil {
		return "토큰 사용량: 미측정\n"
	}
	s := fmt.Sprintf("누적 토큰: %s · 입력 %s · 캐시 입력 %s · 출력 %s\n", count(u.Total.Total), count(u.Total.Input), count(u.Total.Cached), count(u.Total.Output))
	if u.Last != nil {
		s += "최근 턴: " + count(u.Last.Total) + " tokens\n"
	}
	if u.Window != nil {
		s += "컨텍스트 창: " + count(u.Window) + " tokens\n"
	}
	return s
}

func Goal(g *jobs.Goal) string {
	if g == nil {
		return "목표: 없음\n"
	}
	budget := "명시적 예산 없음"
	if g.TokenBudget != nil {
		budget = "예산 " + Number(*g.TokenBudget)
	}
	return fmt.Sprintf("목표: %s · 사용 %s tokens · %s\n", g.Status, Number(g.TokensUsed), budget)
}

func Session(j jobs.Job, st supervisor.Status) string {
	var b strings.Builder
	b.WriteString("선택한 Codex 세션\n\n")
	fmt.Fprintf(&b, "세션: %s\n작업: %s\n상태: %s\n계정: %s\n폴더: %s\n", fallback(j.ThreadID), j.ID, State(j.State), fallback(j.Account), j.Cwd)
	fmt.Fprintf(&b, "모델: %s · 추론: %s\n", fallback(j.Model), fallback(j.Effort))
	fmt.Fprintf(&b, "승인: %s\n샌드박스: %s\n", policy(j.Approval), policy(j.Sandbox))
	b.WriteString(Tokens(j.TokenUsage) + Goal(j.Goal))
	b.WriteString("\n" + AccountFor(st, j.Account, j.Model))
	b.WriteString("\n공식 Codex의 전체 설정·사용량 화면: /codexm_attach\n전체 관리 상태: /codexm_status\n")
	return b.String()
}

// Extract only policy fields, rather than dumping engine configuration JSON.
func policy(raw string) string {
	var text string
	if json.Unmarshal([]byte(raw), &text) == nil {
		return text
	}
	var obj struct {
		Type          string   `json:"type"`
		WritableRoots []string `json:"writableRoots"`
	}
	if json.Unmarshal([]byte(raw), &obj) == nil && obj.Type != "" {
		s := obj.Type
		if len(obj.WritableRoots) > 0 {
			s += " · 쓰기 경로 " + strings.Join(obj.WritableRoots, ", ")
		}
		return s
	}
	return "미확인"
}

func Manager(st supervisor.Status, c config.Config) string {
	running, waiting, complete := 0, 0, 0
	for _, j := range st.Jobs {
		switch {
		case jobs.Busy(j.State):
			running++
		case jobs.Terminal(j.State):
			complete++
		default:
			waiting++
		}
	}
	s := fmt.Sprintf("codexm 관리 상태\n\n화면 버전: %s · 감시 버전: %s · 엔진: %s\n감시: %s\n관리자: %s · 최근 1시간 모델 호출 %d회\n작업: 전체 %d · 실행 %d/%d · 대기 %d · 완료/종료 %d\n계정 순서: %s\n", config.Version, fallback(st.Version), fallback(st.CodexVersion), State(st.Health), fallback(st.ManagerAccount), st.ManagerCalls, len(st.Jobs), running, c.Execution.MaxParallelJobs, waiting, complete, strings.Join(c.WorkerAccounts.Priority, " → "))
	s += "\n" + Accounts(st, c)
	s += "\n" + st.Attach + "\n정상 감시·상태 조회·계정 한도 조회에는 모델 호출이 없습니다.\n"
	return s
}

func quota(w *jobs.UsageWindow) string {
	if w == nil {
		return "미확인"
	}
	label := "한도"
	if w.Minutes != nil {
		switch {
		case *w.Minutes == 10080:
			label = "주간"
		case *w.Minutes == 1440:
			label = "일간"
		case *w.Minutes > 0 && *w.Minutes%60 == 0:
			label = fmt.Sprintf("%d시간", *w.Minutes/60)
		case *w.Minutes > 0:
			label = fmt.Sprintf("%d분", *w.Minutes)
		}
	}
	reset := "갱신 시각 미확인"
	if w.ResetsAt != nil {
		reset = "갱신 " + time.Unix(*w.ResetsAt, 0).Local().Format("01-02 15:04")
	}
	return fmt.Sprintf("%s: 잔여 %d%% · 사용 %d%% · %s", label, max(0, min(100, 100-w.UsedPercent)), w.UsedPercent, reset)
}

func Account(a accounts.Snapshot, model string) string {
	u := activity.Usage(a, model)
	s := fmt.Sprintf("%s · %s · %s\n  기본 %s\n  보조 %s\n  확인 %s\n", a.Account, State(a.State(model)), fallback(a.Plan), quota(u.Primary), quota(u.Secondary), timeText(a.CheckedAt))
	if a.Error != "" {
		s += "  조회 실패: " + a.Error + "\n"
	}
	return s
}

func AccountFor(st supervisor.Status, name, model string) string {
	for _, a := range st.Accounts {
		if a.Account == name {
			return Account(a, model)
		}
	}
	return fallback(name) + " · 한도 미확인 · /codexm_accounts로 새로 조회\n"
}

func Accounts(st supervisor.Status, c config.Config) string {
	byName := map[string]accounts.Snapshot{}
	for _, a := range st.Accounts {
		byName[a.Account] = a
	}
	order := append([]string(nil), c.WorkerAccounts.Priority...)
	extra := []string{}
	for name := range byName {
		found := false
		for _, n := range order {
			if name == n {
				found = true
				break
			}
		}
		if !found {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	order = append(order, extra...)
	var b strings.Builder
	b.WriteString("계정별 한도 (계정 전체의 관측값)\n")
	for _, name := range order {
		if a, ok := byName[name]; ok {
			b.WriteString(Account(a, ""))
		} else {
			b.WriteString(name + " · 미확인\n")
		}
	}
	b.WriteString("새 한도 조회: /codexm_accounts · 갱신 시각은 로컬 시간\n")
	return b.String()
}

func Usage(r UsageReport, st supervisor.Status, c config.Config) string {
	var b strings.Builder
	b.WriteString("codexm 전체 사용량\n\n" + Accounts(st, c) + "\n")
	fmt.Fprintf(&b, "관리자 %s · 최근 1시간 모델 호출 %d회\n", fallback(st.ManagerAccount), st.ManagerCalls)
	var total int64
	measured := 0
	for _, rec := range r.Manager {
		var u tokenUsage
		if json.Unmarshal(rec.Tokens, &u) == nil && u.Total != nil && u.Total.Total != nil {
			total += *u.Total.Total
			measured++
		}
	}
	fmt.Fprintf(&b, "최근 관리자 기록 %d건 · 토큰 관측 %d건", len(r.Manager), measured)
	if measured > 0 {
		fmt.Fprintf(&b, " · 합계 %s", Number(total))
	}
	b.WriteString("\n\n하위 세션별 토큰\n")
	if len(r.Workers) == 0 {
		b.WriteString("등록된 세션 없음\n")
	}
	for _, j := range r.Workers {
		fmt.Fprintf(&b, "\n세션 %s · 작업 %s · %s\n", short(j.ThreadID), short(j.JobID), fallback(j.Account))
		b.WriteString(Tokens(j.Tokens))
		if j.Goal != nil {
			b.WriteString(Goal(j.Goal))
		}
	}
	b.WriteString("\n미측정 값은 0으로 간주하지 않습니다. 계정 한도 %는 세션별 소비량이 아닙니다.\n계정별 실행시간·전환 이력: /codexm_logs all\n")
	return b.String()
}

func Settings(c config.Config) string {
	return fmt.Sprintf("codexm 설정\n\n관리자: %s · 모델 %s\n관리자 모델 호출: 사건당 최대 %d회 · 시간당 최대 %d회\n작업 계정 순서: %s\n최후 계정: %s (상위 계정 전체 소진 시)\n상위 계정 회복 확인: %d초\n동시 작업: %d · 폴더 쓰기 정책: %s\n감시 확인: %d초 · 한도 표본: %d초\n설정: %s\n상태: %s\n", c.Manager.Account, fallback(c.Manager.Model), c.Manager.MaxCallsPerIncident, c.Manager.MaxCallsPerHour, strings.Join(c.WorkerAccounts.Priority, " → "), c.WorkerAccounts.FallbackAccount, c.WorkerAccounts.PreferredRecheckSeconds, c.Execution.MaxParallelJobs, c.Execution.WorkspaceWritePolicy, c.Supervisor.ReconcileIntervalSeconds, c.Logging.UsageSampleSeconds, c.Path, c.StateDir())
}

func Events(es []jobs.Event) string {
	var b strings.Builder
	b.WriteString("선택한 작업의 사건 기록\n\n")
	if len(es) == 0 {
		b.WriteString("기록 없음\n")
	}
	for _, e := range es {
		fmt.Fprintf(&b, "%s · %s · 실행 %s\n", timeText(e.At), e.Method, short(e.RunID))
	}
	return b.String()
}
