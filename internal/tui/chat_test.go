package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/store"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

func fixtureChatThread(id, output string) cr.Thread {
	var thread cr.Thread
	raw, _ := json.Marshal(map[string]any{"id": id, "turns": []any{map[string]any{"id": "turn-" + id, "status": "completed", "items": []any{map[string]any{"type": "userMessage", "content": []any{map[string]string{"type": "text", "text": "지난 지시"}}}, map[string]string{"type": "agentMessage", "text": output}}}}})
	_ = json.Unmarshal(raw, &thread)
	return thread
}

func TestSelectingChatReadsWithoutRegisteringAndFocusesDirectComposer(t *testing.T) {
	f, m := startTUIFixture(t)
	m.CatalogReady = true
	m.Catalog = catalog.List{Sessions: []catalog.Entry{{ID: "saved", ThreadID: "saved", Title: "기존 GUI 대화", State: "saved", Kind: "chat"}}}
	m.SelectedID = "saved"
	f.Handler = func(call supervisor.Call) (any, error) {
		if call.Command != "read" {
			t.Errorf("opening a conversation dispatched %s", call.Command)
		}
		var args supervisor.JobArgs
		json.Unmarshal(call.Args, &args)
		if args.ID != "" || args.ThreadID != "saved" {
			t.Errorf("native read used a fabricated job ID: %+v", args)
		}
		return map[string]any{"thread": fixtureChatThread("saved", "기존 답변"), "cursor": "older"}, nil
	}
	m, cmd := dashboardReviewClick(t, m, "session:0")
	if cmd == nil || !m.inChat() || m.Focus != "input" || m.Input.Value() != "" {
		t.Fatal("mouse selection did not open the chat composer")
	}
	m, _ = dashboardReviewUpdate(t, m, cmd())
	if !strings.Contains(m.content(), "기존 답변") || m.currentChat().Cursor != "older" {
		t.Fatal("existing chat history was not displayed")
	}
	if len(f.Operations) != 0 || len(m.Status.Jobs) != 0 {
		t.Fatal("opening a chat registered or started work")
	}
	m, cmd = dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || len(f.Operations) != 0 {
		t.Fatal("empty chat Enter opened a terminal or generated work")
	}
}

func TestDirectChatSendUsesExactTargetDurableOperationAndKeepsEditedDraft(t *testing.T) {
	f, m := startTUIFixture(t)
	j := jobs.Job{ID: "a", ThreadID: "thread-a", Managed: true, Kind: "chat", State: "idle", Account: "account-work"}
	m.Status.Jobs, m.SelectedID = []jobs.Job{j}, j.ID
	m.enterChat(j)
	m.focusInput("한글 지시\n두 번째 줄")
	m, cmd := dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || m.currentChat().Pending != "send" || m.Input.Value() == "" {
		t.Fatal("direct input was not sent with a recoverable draft")
	}
	if next := m.sendChat(m.Input.Value()); next != nil {
		t.Fatal("pending input could be submitted twice")
	}
	f.Handler = func(call supervisor.Call) (any, error) { return jobs.Action{ID: "accepted"}, nil }
	reply := cmd()
	if len(f.Operations) != 1 || f.Operations[0].ID == "" || f.Operations[0].Command != "send" {
		t.Fatal("chat did not use the durable shared operation layer")
	}
	var args supervisor.JobArgs
	json.Unmarshal(f.Operations[0].Args, &args)
	if args.ID != "a" || args.Text != "한글 지시\n두 번째 줄" || args.Steer {
		t.Fatal("chat send changed the target, text, or steering policy")
	}
	m.focusInput("전송 도중 작성한 다음 초안")
	m, _ = dashboardReviewUpdate(t, m, reply)
	if m.Input.Value() != "전송 도중 작성한 다음 초안" || m.currentChat().Pending != "" || len(m.History) != 1 || m.History[0] != args.Text {
		t.Fatal("acknowledgment erased a newer draft or lost input history")
	}
}

func TestNativeFirstChatSendRegistersThenSendsToSameConversation(t *testing.T) {
	for _, blocked := range []string{"", "paused", "external_writer"} {
		t.Run(blocked, func(t *testing.T) {
			f, m := startTUIFixture(t)
			m.CatalogReady = true
			m.Catalog = catalog.List{Sessions: []catalog.Entry{{ID: "native", ThreadID: "native", Kind: "chat", State: "saved"}}}
			j := m.allJobs()[0]
			m.enterChat(j)
			m.focusInput("기존 대화에 후속 작업")
			f.Handler = func(call supervisor.Call) (any, error) {
				if call.Command == "register" {
					return jobs.Job{ID: "managed", ThreadID: "native", Kind: "chat", Managed: true, State: blocked, Paused: blocked == "paused", Account: "account-secondary"}, nil
				}
				return jobs.Action{ID: "accepted"}, nil
			}
			cmd := m.sendChat(m.Input.Value())
			m, cmd = dashboardReviewUpdate(t, m, cmd())
			if len(f.Operations) != 1 || f.Operations[0].Command != "register" {
				t.Fatal("first native send bypassed metadata registration")
			}
			if blocked != "" {
				if m.currentChat().Pending != "" || m.Input.Value() != "기존 대화에 후속 작업" || m.currentChat().Error == "" {
					t.Fatal("blocked registration lost the draft or queued generation")
				}
				return
			}
			if cmd == nil || m.currentChat().Pending != "send" {
				t.Fatal("registered first input did not proceed to sending")
			}
			m, _ = dashboardReviewUpdate(t, m, cmd())
			if len(f.Operations) != 2 || f.Operations[1].Command != "send" || m.Input.Value() != "" || m.SelectedID != "managed" {
				t.Fatal("registered input was not acknowledged on the same conversation")
			}
			var args supervisor.JobArgs
			json.Unmarshal(f.Operations[1].Args, &args)
			selected, _ := m.selected()
			if args.ID != "managed" || !selected.Managed || selected.Account != "account-secondary" || selected.ThreadID != "native" {
				t.Fatal("metadata merge or subsequent send changed conversation/account")
			}
		})
	}
}

func TestChatSwitchKeepsDraftsHistoryAndLateSendOnOriginalTarget(t *testing.T) {
	f, m := startTUIFixture(t)
	a := jobs.Job{ID: "a", ThreadID: "ta", Kind: "chat", Managed: true}
	b := jobs.Job{ID: "b", ThreadID: "tb", Kind: "chat", Managed: true}
	m.Status.Jobs = []jobs.Job{a, b}
	m.enterChat(a)
	m.focusInput("A 지시")
	cmd := m.sendChat(m.Input.Value())
	m.enterChat(b)
	m.focusInput("B 초안")
	m, _ = dashboardReviewUpdate(t, m, cmd())
	if m.Chat.ThreadID != "tb" || m.Input.Value() != "B 초안" || m.SelectedID != "b" {
		t.Fatal("late A send changed B's view, draft or target")
	}
	if len(f.Operations) != 1 {
		t.Fatal("unexpected send count")
	}
	m.enterChat(a)
	if m.Input.Value() != "" || len(m.History) != 1 || m.History[0] != "A 지시" {
		t.Fatal("A acknowledgment was not retained in A's input history")
	}
	m, _ = dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModAlt})
	if m.Chat.ThreadID != "tb" || m.Input.Value() != "B 초안" {
		t.Fatal("Alt+Down did not restore the other chat's own draft")
	}
	m, _ = dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.Input.Value() != "B 초안" {
		t.Fatal("A history leaked into B's composer")
	}
}

func TestChatAmbiguousSendIsQueriedWithoutResending(t *testing.T) {
	f, m := startTUIFixture(t)
	j := jobs.Job{ID: "job", ThreadID: "thread", Kind: "chat", Managed: true}
	m.Status.Jobs = []jobs.Job{j}
	m.enterChat(j)
	m.focusInput("한 번만 전송")
	cmd := m.sendChat(m.Input.Value())
	f.BreakDispatch = true
	reply := cmd().(chatSendMsg)
	if reply.Reply.Error == nil || reply.Reply.OperationID == "" {
		t.Fatal("fixture failed to simulate an ambiguous operation")
	}
	m, _ = dashboardReviewUpdate(t, m, reply)
	if !m.currentChat().Uncertain || m.Input.Value() != "한 번만 전송" || m.sendChat(m.Input.Value()) != nil {
		t.Fatal("ambiguous outcome lost input or permitted a duplicate")
	}
	f.BreakDispatch = false
	f.Handler = func(call supervisor.Call) (any, error) {
		return store.Operation{ID: reply.Reply.OperationID, Command: "send", State: "done", Result: json.RawMessage(`{"id":"accepted"}`)}, nil
	}
	m, _ = dashboardReviewUpdate(t, m, m.checkChatOperation()())
	if m.currentChat().Uncertain || m.Input.Value() != "" || len(f.Operations) != 1 || len(m.History) != 1 {
		t.Fatal("operation lookup resent input or failed to consume confirmed acknowledgment")
	}
}

func TestChatFailureAndBlockedJobsKeepUnsentInput(t *testing.T) {
	for _, state := range []string{"paused", "external_writer", "failure"} {
		t.Run(state, func(t *testing.T) {
			f, m := startTUIFixture(t)
			j := jobs.Job{ID: "j", ThreadID: "t", Kind: "chat", Managed: true, State: state, Paused: state == "paused"}
			m.Status.Jobs = []jobs.Job{j}
			m.enterChat(j)
			m.focusInput("남겨둘 지시")
			cmd := m.sendChat(m.Input.Value())
			if state == "failure" {
				f.Handler = func(supervisor.Call) (any, error) { return nil, fmt.Errorf("fixture validation failed") }
				m, _ = dashboardReviewUpdate(t, m, cmd())
			} else if cmd != nil {
				t.Fatal("paused or foreign-owned job generated work")
			}
			if m.Input.Value() != "남겨둘 지시" || m.currentChat().Error == "" || m.currentChat().Uncertain {
				t.Fatal("explicit failure lost input or unnecessarily blocked recovery")
			}
		})
	}
}

func TestChatLateReadCannotReplaceAnotherChatOrRestartAfterLeaving(t *testing.T) {
	m := dashboardReviewModel()
	a := m.Status.Jobs[0]
	b := jobs.Job{ID: "b", ThreadID: "tb", Kind: "chat", Title: "두 번째"}
	m.Status.Jobs = append(m.Status.Jobs, b)
	m.enterChat(a)
	sequence := m.currentChat().ReadSequence
	m.enterChat(b)
	m.focusInput("B 초안")
	m, _ = dashboardReviewUpdate(t, m, chatReadMsg{ThreadID: a.ThreadID, Sequence: sequence, Thread: fixtureChatThread(a.ThreadID, "A 답변")})
	if m.Chat.ThreadID != "tb" || m.Input.Value() != "B 초안" || strings.Contains(m.content(), "A 답변") {
		t.Fatal("late history replaced another chat")
	}
	m, _ = dashboardReviewUpdate(t, m, chatReadMsg{ThreadID: "tb", Sequence: m.currentChat().ReadSequence, Thread: fixtureChatThread("wrong", "잘못된 대화")})
	if strings.Contains(m.content(), "잘못된 대화") || m.currentChat().ReadError == "" {
		t.Fatal("mismatched conversation identity was rendered")
	}
	m, _ = dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.inChat() || m.Target != "search" {
		t.Fatal("Escape did not return to management list")
	}
	m, _ = dashboardReviewUpdate(t, m, chatReadMsg{ThreadID: "tb", Sequence: 1, Thread: fixtureChatThread("tb", "B 답변")})
	if m.inChat() || m.Target != "search" {
		t.Fatal("late read reopened chat mode")
	}
}

func TestChatOlderHistoryAndLiveUpdatesPreserveTurnsAndManualScroll(t *testing.T) {
	m := dashboardReviewModel()
	j := m.Status.Jobs[0]
	m.Width, m.Height = 60, 24
	m.enterChat(j)
	s := m.currentChat()
	thread := fixtureChatThread(j.ThreadID, strings.Repeat("최근 대화\n", 80)+"LAST_ANSWER")
	m, _ = dashboardReviewUpdate(t, m, chatReadMsg{ThreadID: j.ThreadID, Sequence: s.ReadSequence, Thread: thread, Cursor: "older"})
	m.AutoFollow = false
	m.prepareDetail()
	m.Detail.SetYOffset(10)
	before := m.Detail.YOffset()
	older := fixtureChatThread(j.ThreadID, strings.Repeat("이전 대화\n", 30))
	older.Turns[0].ID = "older-turn"
	m.refreshChat(true)
	m, _ = dashboardReviewUpdate(t, m, chatReadMsg{ThreadID: j.ThreadID, Sequence: s.ReadSequence, Older: true, Thread: older, Cursor: "oldest"})
	if len(s.Thread.Turns) != 2 || s.Cursor != "oldest" || m.Detail.YOffset() <= before {
		t.Fatal("older-page prepend discarded live turns, cursor or visible scroll anchor")
	}
	offset := m.Detail.YOffset()
	m.refreshChat(false)
	thread.Turns[0].Items[1].Text += "\nSTREAMED_REPLY"
	m, _ = dashboardReviewUpdate(t, m, chatReadMsg{ThreadID: j.ThreadID, Sequence: s.ReadSequence, Thread: thread, Cursor: "newest-page"})
	if len(s.Thread.Turns) != 2 || s.Cursor != "oldest" || m.Detail.YOffset() != offset || !strings.Contains(m.content(), "STREAMED_REPLY") {
		t.Fatal("live refresh replaced old pages or reset manual scrolling")
	}
	m.leaveChat()
	m.enterChat(j)
	if m.Detail.YOffset() != offset || m.AutoFollow {
		t.Fatal("returning to the chat lost its scroll position")
	}
}

func TestChatNavigationFiltersAndNativePickerPreserveOwnDraft(t *testing.T) {
	m := dashboardReviewModel()
	j := m.Status.Jobs[0]
	m.enterChat(j)
	m.focusInput("원래 채팅 초안")
	m, _ = dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyF8})
	if m.inChat() || m.TaskForm.Kind != "filters" {
		t.Fatal("F8 did not leave chat for management filters")
	}
	m.TaskForm.Form.setValue("search", "검토")
	m.submitTaskForm("save")
	if m.Target != "search" || m.Filter != "검토" {
		t.Fatal("saving filters retained chat as the input target")
	}
	m.enterChat(j)
	if m.Input.Value() != "원래 채팅 초안" {
		t.Fatal("filters erased the chat draft")
	}
	native := jobs.Job{ID: "native", ThreadID: "native", Kind: "chat"}
	m.Status.Jobs = append(m.Status.Jobs, native)
	// A catalog-only conversation is not a registered job.
	m.Status.Jobs = m.Status.Jobs[:1]
	m.enterChat(native)
	m.focusInput("native 초안")
	m.slash("/open")
	m.focusInput("다른 계정 별칭")
	m, _ = dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if !m.inChat() || m.Input.Value() != "native 초안" {
		t.Fatal("canceling the native terminal picker replaced the chat draft with an account alias")
	}
	m.slash("/open")
	m.focusInput("잘못된 별칭")
	m.pickerAction("picker:cancel")
	if !m.inChat() || m.Input.Value() != "native 초안" {
		t.Fatal("mouse cancellation failed to restore the native chat draft")
	}
}

func TestChatResponsiveControlsAndToolErrorsAreVisible(t *testing.T) {
	for _, size := range [][2]int{{40, 20}, {120, 40}} {
		m := dashboardReviewModel()
		m.Width, m.Height = size[0], size[1]
		m.enterChat(m.Status.Jobs[0])
		for _, action := range []string{"chat:back", "chat:send", "command:actions", "area:input", "area:detail"} {
			dashboardReviewHit(t, m, action)
		}
		view := ansi.Strip(m.View().Content)
		if len(strings.Split(view, "\n")) != size[1] || !strings.Contains(view, "입력 대상: 채팅") {
			t.Fatal("chat mode overflowed or hid the composer")
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("chat line exceeds terminal width")
			}
		}
		s := m.currentChat()
		s.Thread = fixtureChatThread(s.Job.ThreadID, "최종 답변")
		s.Thread.Turns[0].Items = append(s.Thread.Turns[0].Items, cr.Item{Type: "commandExecution", Command: "go test", Status: "inProgress", AggregatedOutput: "실시간 도구 출력"})
		s.Thread.Turns[0].Error = &cr.TurnError{Message: "확인할 실행 오류"}
		for _, text := range []string{"실시간 도구 출력", "확인할 실행 오류", "최종 답변"} {
			if !strings.Contains(m.content(), text) {
				t.Fatalf("chat omitted %s", text)
			}
		}
	}
}

func TestChatStaleReadDoesNotGenerateWork(t *testing.T) {
	f, m := startTUIFixture(t)
	j := jobs.Job{ID: "j", ThreadID: "t", Kind: "chat", Managed: true}
	m.Status.Jobs = []jobs.Job{j}
	m.enterChat(j)
	s := m.currentChat()
	m, _ = dashboardReviewUpdate(t, m, chatReadMsg{ThreadID: "t", Sequence: s.ReadSequence - 1, Thread: fixtureChatThread("t", "STALE")})
	if !s.ReadPending || s.ReadReady || len(f.Operations) != 0 {
		t.Fatal("stale read completed another request or dispatched work")
	}
}

func TestChatLateRegistrationCannotCloseOtherConversationForm(t *testing.T) {
	m := dashboardReviewModel()
	a := m.Status.Jobs[0]
	b := jobs.Job{ID: "b", ThreadID: "tb", Kind: "chat", Managed: true}
	m.Status.Jobs = append(m.Status.Jobs, b)
	m.enterChat(a)
	epoch := m.ViewEpoch
	m.enterChat(b)
	m.focusInput("B 지시 초안")
	m.beginTaskForm("goal")
	m.TaskForm.Form.setValue("text", "B의 작성 중인 목표")
	raw, _ := json.Marshal(a)
	m, _ = dashboardReviewUpdate(t, m, replyMsg{Command: "register", Epoch: epoch, Data: raw})
	if m.Chat.ThreadID != b.ThreadID || m.TaskForm.Kind != "goal" || m.TaskForm.Form.value("text") != "B의 작성 중인 목표" || m.Input.Value() != "B 지시 초안" {
		t.Fatal("late registration closed or replaced the other chat's unfinished form")
	}
}

func TestChatPollDoesNotRetargetWhenListOrderOrFilterChanges(t *testing.T) {
	m := dashboardReviewModel()
	j := m.Status.Jobs[0]
	m.enterChat(j)
	m.focusInput("선택한 대화에 남길 지시")
	other := jobs.Job{ID: "other", ThreadID: "other-thread", Kind: "chat", Managed: true}
	m.Filter = "no matching title"
	m, _ = dashboardReviewUpdate(t, m, pollMsg{Status: supervisor.Status{Jobs: []jobs.Job{other, j}}})
	selected, _ := m.selected()
	if selected.ThreadID != j.ThreadID || m.SelectedID != j.ID || m.Input.Value() != "선택한 대화에 남길 지시" {
		t.Fatal("catalog polling switched the chat target or draft")
	}
}

func TestChatMultilineEditingAndSlashPaletteKeepTarget(t *testing.T) {
	m := dashboardReviewModel()
	j := m.Status.Jobs[0]
	m.enterChat(j)
	m.focusInput("첫 줄")
	m, cmd := dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	if cmd != nil || !strings.Contains(m.Input.Value(), "\n") {
		t.Fatal("Shift+Enter sent rather than inserting a newline")
	}
	m, _ = dashboardReviewUpdate(t, m, tea.PasteMsg{Content: "두 번째 줄\n세 번째 줄"})
	if !strings.Contains(m.Input.Value(), "세 번째 줄") || !m.inChat() {
		t.Fatal("multiline Korean paste lost the composer or target")
	}
	m.focusInput("/")
	m, _ = dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.PaletteIndex != 1 || m.Chat.ThreadID != j.ThreadID {
		t.Fatal("slash arrows changed conversation")
	}
	m, _ = dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if !m.inChat() || !m.PaletteDismissed {
		t.Fatal("Escape dismissed the chat instead of its command palette")
	}
}

func TestChatSendButtonUsesTheSameCommandRoutingAsEnter(t *testing.T) {
	f, m := startTUIFixture(t)
	j := jobs.Job{ID: "j", ThreadID: "t", Kind: "chat", Managed: true, Account: "account-work"}
	m.Status.Jobs = []jobs.Job{j}
	m.enterChat(j)
	m.focusInput("/account account-secondary")
	// Clicking the send button after scrolling the transcript must still route
	// management commands without submitting them to a model.
	m.Focus = "detail"
	cmd := m.dashboardAction("chat:send")
	if cmd == nil || m.currentChat().Pending != "" {
		t.Fatal("send button bypassed the command interpreter")
	}
	cmd()
	if len(f.Operations) != 1 || f.Operations[0].Command != "switch-account" {
		t.Fatal("send button dispatched a slash command as model input")
	}
}

func TestChatSameEpochRegistrationRetainsNewlyEditedComposer(t *testing.T) {
	m := dashboardReviewModel()
	j := m.Status.Jobs[0]
	m.enterChat(j)
	m.focusInput("등록 요청 이후 새로 작성한 지시")
	raw, _ := json.Marshal(j)
	m, _ = dashboardReviewUpdate(t, m, replyMsg{Command: "register", Epoch: m.ViewEpoch, Data: raw})
	if m.Input.Value() != "등록 요청 이후 새로 작성한 지시" {
		t.Fatal("metadata registration reverted the composer to an old cached draft")
	}
}

func TestChatFiltersFromNativePickerCancelOpenIntent(t *testing.T) {
	m := dashboardReviewModel()
	native := jobs.Job{ID: "native", ThreadID: "native", Kind: "chat"}
	m.enterChat(native)
	m.focusInput("보존할 지시")
	m.openSelected(native)
	m.WantOpen = true
	m, _ = dashboardReviewUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyF8})
	m.submitTaskForm("save")
	if m.Screen != "agents" || m.inChat() || m.WantOpen || m.TaskForm.Kind != "" {
		t.Fatal("filtering restored an old native account picker or open intent")
	}
	m.enterChat(native)
	if m.Input.Value() != "보존할 지시" {
		t.Fatal("leaving native picker through filters replaced the chat draft")
	}
}

func TestNewTaskOpensPendingChatAndAdoptsOfficialThreadWithoutLosingDraft(t *testing.T) {
	f, m := startTUIFixture(t)
	j := jobs.Job{ID: "created", Kind: "chat", Title: "새 대화", Managed: true, State: "queued", Account: "account-work"}
	f.Handler = func(call supervisor.Call) (any, error) {
		if call.Command == "new" {
			return j, nil
		}
		var args supervisor.JobArgs
		json.Unmarshal(call.Args, &args)
		return map[string]any{"thread": fixtureChatThread(args.ThreadID, "새 대화 기록")}, nil
	}
	m.beginTaskForm("new")
	if m.TaskForm.Form.value("open") != "false" {
		t.Fatal("new task would silently open an external terminal")
	}
	m.TaskForm.Form.setValue("cwd", t.TempDir())
	cmd := m.submitTaskForm("save")
	m, _ = dashboardReviewUpdate(t, m, cmd())
	if !m.inChat() || m.Focus != "input" || m.currentChat().Job.ID != j.ID || m.refreshChat(false) != nil {
		t.Fatal("new task did not enter its pending chat without a fabricated thread read")
	}
	m.focusInput("계속 작성한 초안")
	j.ThreadID, j.State = "official-created-thread", "idle"
	m, cmd = dashboardReviewUpdate(t, m, pollMsg{Status: supervisor.Status{Jobs: []jobs.Job{j}}})
	if m.Chat.ThreadID != j.ThreadID || m.Input.Value() != "계속 작성한 초안" || cmd == nil {
		t.Fatal("official thread materialization changed or lost the pending chat")
	}
	m, _ = dashboardReviewUpdate(t, m, cmd())
	if !strings.Contains(m.content(), "새 대화 기록") || len(f.Operations) != 1 {
		t.Fatal("new chat did not read its official thread or submitted additional work")
	}
}
