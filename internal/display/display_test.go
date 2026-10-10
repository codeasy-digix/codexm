package display

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

func TestUsageReadsEngineTokensWithoutPrintingPrivateFields(t *testing.T) {
	raw := json.RawMessage(`{"manager":[{"account":"account-backup","tokens":{"total":{"totalTokens":1200}},"decision":{"summary":"PRIVATE-MANAGER-PROMPT"}}],"workers":[{"job_id":"job","thread_id":"thread","account":"account-work","reported_usage":{"total":{"totalTokens":23456,"inputTokens":22000,"cachedInputTokens":9000,"outputTokens":1456},"last":{"totalTokens":2345},"modelContextWindow":262144},"goal":{"objective":"PRIVATE-OBJECTIVE","status":"complete","tokensUsed":40000}}]}`)
	var report UsageReport
	if e := json.Unmarshal(raw, &report); e != nil {
		t.Fatal(e)
	}
	c := config.Defaults()
	s := Usage(report, supervisor.Status{ManagerAccount: "account-backup"}, c)
	for _, wanted := range []string{"23,456", "22,000", "9,000", "1,456", "262,144", "1,200", "40,000", "계정 한도 %는 세션별 소비량이 아닙니다"} {
		if !strings.Contains(s, wanted) {
			t.Fatalf("missing %s", wanted)
		}
	}
	for _, hidden := range []string{"PRIVATE-", "reported_usage", "totalTokens"} {
		if strings.Contains(s, hidden) {
			t.Fatalf("private or raw field exposed: %s", hidden)
		}
	}
	if s := Tokens(json.RawMessage(`{}`)); !strings.Contains(s, "미측정") {
		t.Fatal(s)
	}
	if s := Tokens(json.RawMessage(`{"total":{"totalTokens":0}}`)); !strings.Contains(s, "누적 토큰: 0") {
		t.Fatal("zero confused with missing")
	}
}

func TestAccountQuotaUsesModelBucketAndPreservesUnknown(t *testing.T) {
	minutes := 300
	reset := int64(1791626400)
	a := accounts.Snapshot{Account: "account-work", Authenticated: true, Plan: "pro", CheckedAt: time.Unix(reset-3600, 0), Single: accounts.Bucket{Primary: &accounts.Window{UsedPercent: 90}}, Buckets: map[string]accounts.Bucket{"model": {NormalModel: "special", Primary: &accounts.Window{UsedPercent: 0, Minutes: &minutes, ResetsAt: &reset}}}}
	s := Account(a, "special")
	for _, text := range []string{"5시간", "잔여 100%", "사용 0%", "갱신", "보조 미확인"} {
		if !strings.Contains(s, text) {
			t.Fatal(s)
		}
	}
	a.Error = "offline"
	s = Account(a, "special")
	if strings.Contains(s, "잔여 100%") || !strings.Contains(s, "미확인") || !strings.Contains(s, "offline") {
		t.Fatal("failed quota reported as available")
	}
}

func TestSessionStatusOmitsTranscriptAndGoalObjective(t *testing.T) {
	j := jobs.Job{ID: "job", ThreadID: "thread", Account: "account-secondary", Model: "gpt-6.1-sol", Approval: `"never"`, Sandbox: `{"type":"workspaceWrite","writableRoots":["/workspace"]}`, LastInput: "PRIVATE-INPUT", LastOutput: "PRIVATE-OUTPUT", Goal: &jobs.Goal{Objective: "PRIVATE-OBJECTIVE", TokensUsed: 1234, Status: "paused"}}
	s := Session(j, supervisor.Status{})
	if strings.Contains(s, "PRIVATE-") || !strings.Contains(s, "never") || !strings.Contains(s, "/workspace") || !strings.Contains(s, "1,234") {
		t.Fatal("status leaked transcript or lost policy")
	}
}
