package tui

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

func TestLargeChatTypingKeepsTranscriptWithoutReformattingIt(t *testing.T) {
	m := largeChatModel()
	revision := m.DetailCache.Revision
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < 10; i++ {
		m, _ = dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: 'a', Text: "a"})
		_ = m.View()
	}
	runtime.ReadMemStats(&after)
	if m.Input.Value() != strings.Repeat("a", 10) || m.DetailCache.Revision != revision {
		t.Fatal("typing changed/reformatted transcript content or lost input")
	}
	// The original path allocates about 194 MB for this input. Leave ample
	// headroom for Go/terminal versions while detecting a full-history rebuild.
	if bytes := after.TotalAlloc - before.TotalAlloc; bytes > 16<<20 {
		t.Fatalf("10 characters allocated %d bytes; transcript work leaked into typing", bytes)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "isolated command output line") {
		t.Fatal("cached output disappeared while typing")
	}
}

func TestChatReadCacheRefreshesNewContentButNotIdenticalPolls(t *testing.T) {
	m := largeChatModel()
	s := m.currentChat()
	initial := m.DetailCache.Revision
	m.refreshChat(false)
	m, _ = dashboardReviewUpdate(t, m, chatReadMsg{ThreadID: s.Job.ThreadID, Sequence: s.ReadSequence, Thread: s.Thread})
	if m.DetailCache.Revision != initial {
		t.Fatal("identical conversation polling rebuilt the transcript")
	}
	thread := fixtureChatThread(s.Job.ThreadID, "UPDATED_ANSWER")
	thread.Turns[0].ID = "new-turn"
	m.refreshChat(false)
	m, _ = dashboardReviewUpdate(t, m, chatReadMsg{ThreadID: s.Job.ThreadID, Sequence: s.ReadSequence, Thread: thread})
	if m.DetailCache.Revision <= initial || !strings.Contains(ansi.Strip(m.View().Content), "UPDATED_ANSWER") {
		t.Fatal("cached rendering hid the new answer")
	}
}

func TestClickingChatOutputKeepsInputAndEnterSendsExactDraft(t *testing.T) {
	f, m := startTUIFixture(t)
	j := jobs.Job{ID: "job", ThreadID: "thread", Title: "입력 검사", Kind: "chat", Managed: true, State: "idle"}
	m.Status.Jobs = []jobs.Job{j}
	m.enterChat(j)
	m.focusInput("본문을 클릭한 뒤에도 보낼 지시")
	m, _ = dashboardReviewClick(t, m, "area:detail")
	if m.Focus != "input" {
		t.Fatal("transcript click took the input focus")
	}
	m, cmd := dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter after clicking transcript did not submit the draft")
	}
	cmd()
	if len(f.Operations) != 1 || f.Operations[0].Command != "send" {
		t.Fatal("Enter did not reach the common send API")
	}
	var args supervisor.JobArgs
	json.Unmarshal(f.Operations[0].Args, &args)
	if args.ID != j.ID || args.Text != "본문을 클릭한 뒤에도 보낼 지시" {
		t.Fatal("output click changed the target or content")
	}
}

func TestChatKeyboardTranscriptFocusReturnsForTextPasteAndEnter(t *testing.T) {
	for _, event := range []tea.Msg{tea.KeyPressMsg{Code: '한', Text: "한"}, tea.PasteMsg{Content: "붙여넣기\n둘째 줄"}} {
		f, m := startTUIFixture(t)
		j := jobs.Job{ID: "job", ThreadID: "thread", Kind: "chat", Managed: true}
		m.Status.Jobs = []jobs.Job{j}
		m.enterChat(j)
		m.Focus = "detail"
		m.Input.Blur()
		m, _ = dashboardReviewUpdate(t, m, event)
		if m.Focus != "input" || m.Input.Value() == "" {
			t.Fatal("text/paste was swallowed by transcript focus")
		}
		m.Focus = "detail"
		m.Input.Blur()
		m, cmd := dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd == nil || m.Focus != "input" {
			t.Fatal("Enter did not return to the composer and send its draft")
		}
		cmd()
		if len(f.Operations) != 1 || f.Operations[0].Command != "send" {
			t.Fatal("keyboard transcript navigation swallowed Enter")
		}
	}
}

func TestChatBlockedStateStaysBesideComposerInLongHistory(t *testing.T) {
	for state, message := range map[string]string{"auth_required": "로그인 필요", "external_writer": "다른 GUI/터미널 사용 중", "paused": "일시 중지"} {
		t.Run(state, func(t *testing.T) {
			m := largeChatModel()
			m.Width, m.Height = 40, 20
			m.Status.Jobs[0].State, m.Status.Jobs[0].Paused = state, state == "paused"
			m.prepareDetail()
			lines := strings.Split(ansi.Strip(m.View().Content), "\n")
			if !strings.Contains(strings.Join(lines[len(lines)-5:], "\n"), message) {
				t.Fatal("important execution state scrolled away from the input")
			}
			if state == "auth_required" {
				found := false
				for _, button := range m.taskButtons() {
					found = found || button.Action == "command:resume"
				}
				if !found {
					t.Fatal("reauthenticated tasks have no visible resume action")
				}
			}
		})
	}
}

func TestNativeSubagentExplainsReadOnlyAndOpensParentWithoutSending(t *testing.T) {
	f, m := startTUIFixture(t)
	m.CatalogReady = true
	m.Catalog = catalog.List{Sessions: []catalog.Entry{
		{ID: "child", ThreadID: "child", Source: "subagent", ParentThreadID: "parent", Title: "하위 작업 · 검토", Kind: "chat"},
		{ID: "parent", ThreadID: "parent", Source: "cli", Title: "부모 대화", Kind: "chat"},
	}}
	m.enterChat(m.Catalog.Sessions[0].Job(m.Config.Runtime.SharedCodexHome))
	m.focusInput("보내지 않을 하위 작업 지시")
	if cmd := m.sendChat(m.Input.Value()); cmd != nil || len(f.Operations) != 0 || !strings.Contains(m.chatInputStatus(), "부모 대화") {
		t.Fatal("native child attempted independent registration/generation or hid the reason")
	}
	cmd := m.dashboardAction("chat:parent")
	if cmd == nil || m.Chat.ThreadID != "parent" || len(f.Operations) != 0 || m.Input.Value() != "" {
		t.Fatal("parent navigation started work or reused the child's draft")
	}
	if m.Chat.Sessions["child"].Composer.Draft != "보내지 않을 하위 작업 지시" {
		t.Fatal("opening parent lost the child draft")
	}
}

func TestOlderChatPagePreservesScrollWhenReplyArrivesDuringModal(t *testing.T) {
	m := largeChatModel()
	s := m.currentChat()
	s.Cursor = "older"
	m.AutoFollow = false
	m.Detail.SetYOffset(10)
	before := m.Detail.YOffset()
	m.refreshChat(true)
	m.beginTaskForm("goal")
	older := fixtureChatThread(s.Job.ThreadID, strings.Repeat("OLD_PAGE\n", 40))
	older.Turns[0].ID = "older-page"
	m, _ = dashboardReviewUpdate(t, m, chatReadMsg{ThreadID: s.Job.ThreadID, Sequence: s.ReadSequence, Older: true, Thread: older})
	if m.TaskForm.Kind != "goal" || s.PrependAnchor == nil || m.Detail.YOffset() != before {
		t.Fatal("older-page response disturbed the modal or discarded its scroll anchor")
	}
	m, _ = dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.TaskForm.Kind != "" || s.PrependAnchor != nil || m.Detail.YOffset() <= before {
		t.Fatal("closing the modal failed to apply the pending history scroll anchor")
	}
}

func TestChatWriterReleaseClearsOnlyTheOldBlockedMessage(t *testing.T) {
	m := dashboardReviewModel()
	j := m.Status.Jobs[0]
	j.State = "external_writer"
	m.Status.Jobs[0] = j
	m.enterChat(j)
	m.focusInput("실행권 해제 후 보낼 지시")
	if m.sendChat(m.Input.Value()) != nil || m.currentChat().Error == "" {
		t.Fatal("foreign writer was not respected")
	}
	j.State = "idle"
	m, _ = dashboardReviewUpdate(t, m, pollMsg{Status: supervisor.Status{Jobs: []jobs.Job{j}}})
	if m.currentChat().Error != "" || strings.Contains(m.chatInputStatus(), "실행권") || m.Input.Value() != "실행권 해제 후 보낼 지시" {
		t.Fatal("writer release left stale blocked status or erased the unsent draft")
	}
	if m.sendChat(m.Input.Value()) == nil {
		t.Fatal("released conversation cannot accept its retained draft")
	}
}
