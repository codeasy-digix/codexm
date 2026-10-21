package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

// These tests inspect queued commands without executing them. In particular,
// neither a real management service nor an authenticated runtime is needed.
func dashboardReviewModel() Model {
	c := config.Defaults()
	c.Manager.Account = "review-manager"
	c.WorkerAccounts.Priority = []string{"primary", "secondary"}
	m := New(c, "codexm")
	m.Status.Jobs = []jobs.Job{{ID: "review-job", ThreadID: "review-thread", RunID: "review-run", Title: "검토할 작업", Account: "primary", Kind: "chat", Managed: true, State: "idle"}}
	m.SelectedID = "review-job"
	return m
}

func dashboardReviewUpdate(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	updated, ok := next.(Model)
	if !ok {
		t.Fatalf("unexpected model type %T", next)
	}
	return updated, cmd
}

func dashboardReviewHit(t *testing.T, m Model, action string) hitRegion {
	t.Helper()
	_, hits := m.dashboard()
	for _, hit := range hits {
		if hit.Action != action {
			continue
		}
		if hit.X < 0 || hit.Y < 0 || hit.W <= 0 || hit.H <= 0 || hit.X+hit.W > m.Width || hit.Y+hit.H > m.Height {
			t.Fatalf("%s hit is outside the %dx%d terminal: %+v", action, m.Width, m.Height, hit)
		}
		return hit
	}
	t.Fatalf("visible hit %q is missing from:\n%s", action, ansi.Strip(m.View().Content))
	return hitRegion{}
}

func dashboardReviewClick(t *testing.T, m Model, action string) (Model, tea.Cmd) {
	t.Helper()
	hit := dashboardReviewHit(t, m, action)
	return dashboardReviewUpdate(t, m, tea.MouseClickMsg{X: hit.X, Y: hit.Y, Button: tea.MouseLeft})
}

func TestDashboardReviewCtrlSCannotConsumeFallbackOnlyRequests(t *testing.T) {
	cases := []struct {
		name, method, params string
	}{
		{"secret question", "item/tool/requestUserInput", `{"questions":[{"id":"private","question":"비공개 답변","isSecret":true}]}`},
		{"unsupported request", "unsupported/request", `{}`},
		{"unsupported elicitation", "mcpServer/elicitation/request", `{"mode":"form","message":"복합 입력","requestedSchema":{"properties":{"nested":{"type":"object"}},"required":["nested"]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := dashboardReviewModel()
			r := jobs.Request{ID: json.RawMessage(`"request-one"`), RunID: "review-run", Method: tc.method, Params: json.RawMessage(tc.params)}
			m.Status.Jobs[0].PendingRequests = []jobs.Request{r}
			m.focusInput("보내지 않은 입력 초안")
			if cmd := m.slash("/requests"); cmd != nil || m.TaskForm.Kind != "requests" {
				t.Fatal("opening a pending request did not stay local")
			}
			for _, action := range m.TaskForm.Form.Actions {
				if action.Key == "accept" {
					t.Fatal("fallback-only request offered an accept action")
				}
			}
			m, cmd := dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
			if cmd != nil || m.TaskForm.Busy || m.TaskForm.Pending != "" {
				t.Fatal("Ctrl+S queued a reply for a request that requires the official terminal")
			}
			if m.TaskForm.Kind != "requests" || len(m.Status.Jobs[0].PendingRequests) != 1 || m.Input.Value() != "보내지 않은 입력 초안" {
				t.Fatal("fallback request or composer draft was lost")
			}
		})
	}
}

func TestDashboardReviewCtrlSStillSubmitsSupportedQuestionAnswers(t *testing.T) {
	m := dashboardReviewModel()
	m.Status.Jobs[0].PendingRequests = []jobs.Request{{ID: json.RawMessage(`"question"`), RunID: "review-run", Method: "item/tool/requestUserInput", Params: json.RawMessage(`{"questions":[{"id":"choice","question":"선택하세요"}]}`)}}
	m.slash("/requests")
	m.TaskForm.Form.setValue("choice", "선택한 답변")
	m, cmd := dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if cmd == nil || !m.TaskForm.Busy || m.TaskForm.Pending != "reply" || m.TaskForm.JobID != "review-job" || m.TaskForm.RunID != "review-run" {
		t.Fatal("Ctrl+S did not queue a supported answer for the original job and run")
	}
}

func TestDashboardReviewAmbiguousSendCannotBeSubmittedAgain(t *testing.T) {
	for _, failure := range []error{context.DeadlineExceeded, io.EOF} {
		t.Run(failure.Error(), func(t *testing.T) {
			m := dashboardReviewModel()
			m.slash("/send")
			m.TaskForm.Form.setValue("text", "한 번만 보낼 지시")
			m, cmd := dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
			if cmd == nil || !m.TaskForm.Busy {
				t.Fatal("fixture did not queue the first explicit send")
			}
			m, _ = dashboardReviewUpdate(t, m, replyMsg{Command: "send", Error: failure, Epoch: m.ViewEpoch})
			if !m.TaskForm.Uncertain || m.TaskForm.Form.value("text") != "한 번만 보낼 지시" {
				t.Fatal("ambiguous transport failure did not preserve the instruction for inspection")
			}
			m, cmd = dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
			if cmd != nil || m.TaskForm.Busy || m.TaskForm.Pending != "" {
				t.Fatal("Ctrl+S retried an instruction whose first outcome is unknown")
			}
		})
	}
}

func TestDashboardReviewApprovalDetailsCanBeReadWithMouseWheel(t *testing.T) {
	m := dashboardReviewModel()
	m, _ = dashboardReviewUpdate(t, m, tea.WindowSizeMsg{Width: 40, Height: 20})
	details := "FIRST_APPROVAL_LINE\n" + strings.Repeat("명령의 세부 내용을 확인하세요.\n", 25) + "LAST_APPROVAL_LINE"
	params, err := json.Marshal(map[string]string{"command": details})
	if err != nil {
		t.Fatal(err)
	}
	m.Status.Jobs[0].PendingRequests = []jobs.Request{{ID: json.RawMessage(`"approval"`), RunID: "review-run", Method: "item/commandExecution/requestApproval", Params: params}}
	m.slash("/requests")
	if !strings.Contains(ansi.Strip(m.View().Content), "FIRST_APPROVAL_LINE") {
		t.Fatal("fixture did not initially display the approval's first line")
	}
	hit := dashboardReviewHit(t, m, "task:field:0")
	m, cmd := dashboardReviewUpdate(t, m, tea.MouseWheelMsg{X: hit.X, Y: hit.Y, Button: tea.MouseWheelDown})
	if cmd != nil || m.TaskForm.Busy || m.TaskForm.Form.value("info") != details || m.TaskForm.Form.Editing || m.TaskForm.Form.Index != 0 {
		t.Fatal("scrolling approval details edited the request or submitted a decision")
	}
	if strings.Contains(ansi.Strip(m.View().Content), "FIRST_APPROVAL_LINE") {
		t.Fatal("mouse wheel did not scroll the approval details")
	}
}

func TestDashboardReviewWrappedDetailBottomReachable(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 100, Height: 30}, {Width: 60, Height: 24}} {
		for _, input := range []string{"page-down", "mouse-wheel"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size.Width, size.Height, input), func(t *testing.T) {
				m := dashboardReviewModel()
				m.AutoFollow = false
				m.Panel = "read"
				m.DetailText = strings.Repeat("줄바꿈을 확인할 긴 대화 내용입니다. ", 150) + "\nWRAPPED_BOTTOM_SENTINEL"
				m, _ = dashboardReviewUpdate(t, m, size)
				m, _ = dashboardReviewClick(t, m, "area:detail")
				m, _ = dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyHome})
				hit := dashboardReviewHit(t, m, "area:detail")
				if m.Detail.Width() != hit.W || m.Detail.Height() != hit.H {
					t.Fatalf("persistent viewport is %dx%d; visible pane is %dx%d", m.Detail.Width(), m.Detail.Height(), hit.W, hit.H)
				}
				if strings.Contains(ansi.Strip(m.View().Content), "WRAPPED_BOTTOM_SENTINEL") {
					t.Fatal("fixture is too short to require scrolling")
				}
				for attempts := 0; attempts < 300; attempts++ {
					var msg tea.Msg = tea.KeyPressMsg{Code: tea.KeyPgDown}
					if input == "mouse-wheel" {
						msg = tea.MouseWheelMsg{X: hit.X, Y: hit.Y, Button: tea.MouseWheelDown}
					}
					m, _ = dashboardReviewUpdate(t, m, msg)
					if strings.Contains(ansi.Strip(m.View().Content), "WRAPPED_BOTTOM_SENTINEL") {
						return
					}
				}
				t.Fatalf("%s could not reach the final wrapped detail line (offset %d)", input, m.Detail.YOffset())
			})
		}
	}
}

func TestDashboardReviewOldLogsCannotAppearUnderUnregisteredScope(t *testing.T) {
	m := dashboardReviewModel()
	managed := m.Status.Jobs[0]
	managed.Title = "PREVIOUS_MANAGED_RUN"
	m, _ = dashboardReviewUpdate(t, m, pollMsg{Status: supervisor.Status{Jobs: []jobs.Job{managed}}, Catalog: catalog.List{Sessions: []catalog.Entry{
		{ID: managed.ThreadID, ThreadID: managed.ThreadID, JobID: managed.ID, Title: managed.Title, Managed: true, Kind: "chat"},
		{ID: "native-only", ThreadID: "native-only", Title: "등록하지 않은 대화", Kind: "chat", State: "saved"},
	}}})
	if cmd := m.slash("/logs all"); cmd == nil {
		t.Fatal("all-job logs were not requested")
	}
	now := time.Date(2026, 10, 22, 6, 0, 0, 0, time.UTC)
	report := jobs.WorkLog{At: now, Runs: []jobs.Run{{ID: "old-run", JobID: managed.ID, Account: "OLD_ACCOUNT_SENTINEL", StartedAt: now.Add(-time.Hour)}}, Changes: []jobs.WorkChange{{ID: "old-change", JobID: managed.ID, From: "primary", To: "secondary", Reason: "OLD_REASON_SENTINEL", At: now}}}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	oldReply := replyMsg{Command: "logs", Data: raw, Epoch: m.ViewEpoch}
	m, _ = dashboardReviewUpdate(t, m, oldReply)
	if !strings.Contains(ansi.Strip(m.View().Content), "OLD_ACCOUNT_SENTINEL") {
		t.Fatal("fixture did not display all-job logs before changing scope")
	}
	m, _ = dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyF1})
	m, _ = dashboardReviewClick(t, m, "session:1")
	m.focusInput("/logs")
	m, cmd := dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || m.LogsOpen || m.SelectedID != "native-only" || len(m.Status.Jobs) != 1 {
		t.Fatal("reading an unregistered conversation's logs imported it or queued work")
	}
	m, _ = dashboardReviewUpdate(t, m, oldReply)
	view := ansi.Strip(m.View().Content)
	for _, old := range []string{"PREVIOUS_MANAGED_RUN", "OLD_ACCOUNT_SENTINEL", "OLD_REASON_SENTINEL"} {
		if strings.Contains(view, old) {
			t.Fatalf("old all-job log %q leaked into the unregistered conversation's log scope:\n%s", old, view)
		}
	}
	if !strings.Contains(view, "등록한 이후") {
		t.Fatal("unregistered conversation did not explain when management logs begin")
	}
}

func TestDashboardReviewMouseTabCancelsAccountPickerWithoutChangingAccount(t *testing.T) {
	m := dashboardReviewModel()
	m.focusInput("전송하지 않은 초안")
	if cmd := m.slash("/account"); cmd != nil || m.Screen != "account" {
		t.Fatal("account picker did not open without mutation")
	}
	m, cmd := dashboardReviewClick(t, m, "picker:account:1")
	if cmd != nil || m.AccountNames[m.AccountSelected] != "secondary" {
		t.Fatal("choosing a different account did not remain a local selection")
	}
	m, cmd = dashboardReviewClick(t, m, "tab:tasks")
	if cmd != nil || m.Screen != "agents" || m.Tab != "tasks" || m.Focus != "input" {
		t.Fatal("clicking the task tab did not cancel the account picker locally")
	}
	if m.Input.Value() != "전송하지 않은 초안" || m.Status.Jobs[0].Account != "primary" || m.Status.Jobs[0].RunID != "review-run" {
		t.Fatal("leaving the picker changed the account, run, or unsent composer draft")
	}
	dashboardReviewHit(t, m, "area:detail")
}

func TestDashboardReviewNarrowComposerPaletteAndActionsRemainVisible(t *testing.T) {
	m := dashboardReviewModel()
	m, _ = dashboardReviewUpdate(t, m, tea.WindowSizeMsg{Width: 40, Height: 20})
	m.focusInput("narrow composer draft")
	for _, palette := range []bool{false, true} {
		if palette {
			m.focusInput("")
			m, _ = dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: '/', Text: "/"})
		}
		view := ansi.Strip(m.View().Content)
		lines := strings.Split(view, "\n")
		if len(lines) > 20 {
			t.Fatalf("narrow view has %d lines", len(lines))
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > 40 {
				t.Fatalf("narrow view overflows horizontally: %q", line)
			}
		}
		if !strings.Contains(view, "입력 대상:") {
			t.Fatal("input target disappeared in the narrow terminal")
		}
		inputHit := dashboardReviewHit(t, m, "area:input")
		if !strings.Contains(lines[inputHit.Y], m.Input.Value()) {
			t.Fatal("visible composer hit does not contain the unsent input")
		}
		for _, action := range []string{"command:new", "command:actions", "command:filters"} {
			dashboardReviewHit(t, m, action)
		}
		if palette && !strings.Contains(view, "명령 목록") {
			t.Fatal("slash palette is hidden in the narrow terminal")
		}
	}
	m, cmd := dashboardReviewClick(t, m, "command:actions")
	if cmd != nil || m.TaskForm.Kind != "actions" || m.Input.Value() != "/" {
		t.Fatal("narrow action menu queued work or discarded the composer")
	}
}
