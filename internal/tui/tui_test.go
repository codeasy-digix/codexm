package tui

import (
	tea "charm.land/bubbletea/v2"
	"encoding/json"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"strings"
	"testing"
)

func TestInputTargetAndKoreanMultilinePaste(t *testing.T) {
	c := config.Defaults()
	c.Manager.Account = "account-backup"
	m := New(c, "codexm")
	m.Status.Jobs = []jobs.Job{{ID: "job", Title: "작업", Managed: true, Kind: "chat"}}
	m.SelectedID = "job"
	m.focusInput("")
	text := "한글 입력\n여러 줄 지시"
	next, _ := m.Update(tea.PasteMsg{Content: text})
	m = next.(Model)
	if m.Input.Value() != text {
		t.Fatal(m.Input.Value())
	}
	m.slash("/manager")
	if m.Target != "manager" || !strings.Contains(m.View().Content, "관리자 account-backup (모델 호출)") {
		t.Fatal("manager target hidden")
	}
	m.slash("/job")
	if m.Target != "job" {
		t.Fatal("job target was not restored")
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 40, Height: 20}, {Width: 120, Height: 40}} {
		next, _ = m.Update(size)
		m = next.(Model)
		if !strings.Contains(m.View().Content, "입력 대상:") {
			t.Fatal("input target missing after resize")
		}
	}
}

func TestLogsRefreshDoesNotStealInputOrReopenAfterEscape(t *testing.T) {
	c := config.Defaults()
	c.Manager.Account = "account-backup"
	m := New(c, "codexm")
	m.SelectedID = "job"
	m.Status.Jobs = []jobs.Job{{ID: "job"}}
	if cmd := m.slash("/logs"); cmd == nil || !m.LogsOpen || m.LogsJob != "job" {
		t.Fatal("logs not opened")
	}
	m.Focus = "input"
	raw, _ := json.Marshal(jobs.WorkLog{Note: "account-wide quota"})
	next, _ := m.Update(replyMsg{Command: "logs", Data: raw, Epoch: m.ViewEpoch})
	m = next.(Model)
	if m.Focus != "input" {
		t.Fatal("refresh stole input focus")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(Model)
	next, _ = m.Update(replyMsg{Command: "logs", Data: raw, Epoch: m.ViewEpoch - 1})
	m = next.(Model)
	if m.LogsOpen || m.Focus != "input" || m.Panel != "" {
		t.Fatal("late logs response reopened the view")
	}
}

func press(m Model, key tea.KeyPressMsg) (Model, tea.Cmd) {
	next, cmd := m.Update(key)
	return next.(Model), cmd
}

func TestDefaultInputPaletteCompletionAndEscapePreserveDraft(t *testing.T) {
	c := config.Defaults()
	c.Manager.Account = "account-backup"
	m := New(c, "codexm")
	if m.Focus != "input" || !m.Input.Focused() {
		t.Fatal("input not focused at startup")
	}
	m, _ = press(m, tea.KeyPressMsg{Code: '/', Text: "/"})
	if !strings.Contains(m.View().Content, "명령 목록") || len(m.commandMatches()) < 10 {
		t.Fatal("slash menu missing")
	}
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	m, cmd := press(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if cmd != nil || m.Input.Value() != "/usage" {
		t.Fatal("tab did not complete without executing")
	}
	m.focusInput("/codexm_l")
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyTab})
	if m.Input.Value() != "/codexm_logs " {
		t.Fatal(m.Input.Value())
	}
	m.focusInput("/codexm_")
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.Input.Value() != "/codexm_" || m.Focus != "input" || len(m.commandMatches()) != 0 {
		t.Fatal("escape erased draft or failed to dismiss")
	}
	m.focusInput("한글 초안\n추가 지시")
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyTab})
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.Focus != "input" || m.Input.Value() != "한글 초안\n추가 지시" {
		t.Fatal("list navigation erased draft")
	}
}

func TestNativeStopAndScopedLogsAndLateUsage(t *testing.T) {
	c := config.Defaults()
	c.Manager.Account = "account-backup"
	m := New(c, "codexm")
	m.Status.Jobs = []jobs.Job{{ID: "selected", ThreadID: "thread", Managed: true}}
	m.SelectedID = "selected"
	if cmd := m.slash("/stop"); cmd != nil || m.Panel != "native" || m.NativeCommand != "/stop" {
		t.Fatal("native /stop was routed to manager mutation")
	}
	if cmd := m.slash("/codexm_logs"); cmd == nil || m.LogsJob != "selected" {
		t.Fatal("selected log scope missing")
	}
	epoch := m.ViewEpoch
	if cmd := m.slash("/codexm_logs all"); cmd == nil || m.LogsJob != "" {
		t.Fatal("all log scope missing")
	}
	raw, _ := json.Marshal(jobs.WorkLog{Note: "stale selected logs"})
	next, _ := m.Update(replyMsg{Command: "logs", Data: raw, Epoch: epoch})
	m = next.(Model)
	if strings.Contains(m.DetailText, "stale selected") {
		t.Fatal("old scope response replaced all logs")
	}
	m.slash("/codexm_usage")
	epoch = m.ViewEpoch
	m.slash("/status")
	next, _ = m.Update(replyMsg{Command: "usage", Data: json.RawMessage(`{"manager":[],"workers":[]}`), Epoch: epoch})
	m = next.(Model)
	if m.Panel != "status" || m.Focus != "input" {
		t.Fatal("late usage stole panel or focus")
	}
	before := m.Status.Jobs[0].Goal
	if cmd := m.slash("/goal"); cmd != nil || m.Status.Jobs[0].Goal != before {
		t.Fatal("goal query mutated goal")
	}
}

func TestPaletteFitsNarrowTerminalAndReportsKeepInput(t *testing.T) {
	c := config.Defaults()
	c.Manager.Account = "account-backup"
	m := New(c, "codexm")
	m.Status.Jobs = []jobs.Job{{ID: "one"}, {ID: "two"}, {ID: "three"}, {ID: "four"}}
	m.SelectedID = "one"
	for _, size := range []tea.WindowSizeMsg{{Width: 40, Height: 20}, {Width: 100, Height: 30}, {Width: 120, Height: 40}} {
		next, _ := m.Update(size)
		m = next.(Model)
		m.focusInput("/")
		view := m.View().Content
		if lines := strings.Count(view, "\n") + 1; lines > size.Height {
			t.Fatalf("view %d lines exceeds %d at %d cols", lines, size.Height, size.Width)
		}
		if !strings.Contains(view, "입력 대상:") || !strings.Contains(view, "명령 목록") {
			t.Fatal("composer or palette hidden")
		}
	}
	m.Input.Reset()
	m.slash("/codexm_settings")
	raw, _ := json.Marshal(c)
	next, _ := m.Update(replyMsg{Command: "settings", Data: raw, Epoch: m.ViewEpoch})
	m = next.(Model)
	if m.Focus != "input" || strings.Contains(m.DetailText, `"max_parallel_jobs"`) || !strings.Contains(m.DetailText, "동시 작업:") {
		t.Fatal("settings still raw or stole focus")
	}
}

func TestInputScrollingSurvivesPollAndCopySkipsRunningOutput(t *testing.T) {
	c := config.Defaults()
	c.Manager.Account = "account-backup"
	m := New(c, "codexm")
	m.SelectedID = "job"
	m.Status.Jobs = []jobs.Job{{ID: "job", LastOutput: strings.Repeat("scroll line\n", 100)}}
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	first := m.Detail.YOffset()
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	second := m.Detail.YOffset()
	next, _ := m.Update(pollMsg{Status: m.Status})
	m = next.(Model)
	if second >= first || m.Detail.YOffset() != second || m.Focus != "input" {
		t.Fatal("refresh reset scroll while composing")
	}
	thread := cr.Thread{Turns: []cr.Turn{
		{Status: "completed", Items: []cr.Item{{Type: "agentMessage", Text: "completed answer"}}},
		{Status: "inProgress", Items: []cr.Item{{Type: "agentMessage", Text: "unfinished output"}}},
	}}
	if got := completedOutput(thread); got != "completed answer" {
		t.Fatal(got)
	}
}
