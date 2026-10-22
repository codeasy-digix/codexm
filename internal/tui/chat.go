package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/display"
	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/store"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

type composerState struct {
	Draft, HistoryDraft string
	History             []string
	HistoryIndex        int
}

type chatState struct {
	ThreadID string
	Sessions map[string]*chatSession
	Search   composerState
}

type chatSession struct {
	Job                         jobs.Job
	Composer                    composerState
	Thread                      cr.Thread
	Cursor, ReadError, Error    string
	ReadPending, ReadReady      bool
	OlderLoaded                 bool
	ReadSequence, SendSequence  uint64
	Pending, PendingText        string
	Uncertain, Checking         bool
	OperationID, UnknownCommand string
	Offset                      int
	Follow                      bool
}

// Replies carry the immutable conversation identity and request sequence, not
// whichever row happens to be selected when asynchronous work finishes.
type chatReadMsg struct {
	ThreadID string
	Sequence uint64
	Older    bool
	Thread   cr.Thread
	Cursor   string
	Error    error
}
type chatSendMsg struct {
	ThreadID, Command string
	Sequence          uint64
	Reply             replyMsg
}

func (m Model) inChat() bool {
	return m.Target == "chat" && m.Tab == "tasks" && m.Chat.ThreadID != ""
}
func (m Model) currentChat() *chatSession { return m.Chat.Sessions[m.Chat.ThreadID] }
func (m Model) captureComposer() composerState {
	return composerState{m.Input.Value(), m.HistoryDraft, slices.Clone(m.History), m.HistoryIndex}
}
func (m *Model) restoreComposer(c composerState) {
	m.Input.SetValue(c.Draft)
	m.Input.CursorEnd()
	m.History, m.HistoryIndex, m.HistoryDraft = slices.Clone(c.History), c.HistoryIndex, c.HistoryDraft
	m.PaletteDismissed = true
	m.returnInput()
}
func (m *Model) saveChatComposer() {
	if m.inChat() && m.Screen != "import-account" {
		if s := m.currentChat(); s != nil {
			s.Composer = m.captureComposer()
			s.Offset, s.Follow = m.Detail.YOffset(), m.AutoFollow
		}
	}
}
func (m *Model) leaveChat() {
	m.WantOpen, m.WantRead, m.WantCommand = false, false, ""
	if m.inChat() {
		m.saveChatComposer()
		m.Target = "search"
		m.Chat.ThreadID = ""
		m.restoreComposer(m.Chat.Search)
	}
	m.Input.Placeholder = "작업 검색 · / 관리 명령 목록"
}
func (m *Model) enterChat(j jobs.Job) tea.Cmd {
	if (j.ThreadID == "" && (!j.Managed || j.ID == "")) || j.Kind == "group" {
		m.Notice = "채팅할 하위 대화를 선택하세요. 그룹 관리는 F9를 사용하세요."
		return nil
	}
	if m.inChat() {
		m.saveChatComposer()
	} else if m.Target == "search" {
		m.Chat.Search = m.captureComposer()
	}
	if m.Chat.Sessions == nil {
		m.Chat.Sessions = map[string]*chatSession{}
	}
	key := j.ThreadID
	if key == "" {
		key = "job:" + j.ID // New jobs acquire their official thread asynchronously.
	}
	s := m.Chat.Sessions[key]
	if s == nil && j.ThreadID != "" {
		s = m.Chat.Sessions["job:"+j.ID]
		delete(m.Chat.Sessions, "job:"+j.ID)
	}
	if s == nil {
		s = &chatSession{Follow: true}
	}
	m.Chat.Sessions[key] = s
	s.Job = j
	m.Chat.ThreadID, m.SelectedID = key, j.ID
	for i, item := range m.filtered() {
		if item.ID == j.ID {
			m.Selected = i
		}
	}
	m.Target, m.Tab, m.Screen = "chat", "tasks", "agents"
	m.Panel, m.DetailText, m.LogsOpen = "", "", false
	m.WantOpen, m.WantRead, m.WantCommand = false, false, ""
	m.ViewEpoch++
	m.AutoFollow = s.Follow
	m.restoreComposer(s.Composer)
	m.Input.Placeholder = "이 대화에 지시 입력 · Enter 전송 · Shift+Enter 줄바꿈"
	m.prepareDetail()
	m.Detail.SetYOffset(s.Offset)
	if s.Follow {
		m.Detail.GotoBottom()
	}
	m.Notice = "채팅 모드 · Enter 전송 · Esc 작업 목록 · F9 관리 동작"
	return m.refreshChat(false)
}

func (m *Model) refreshChat(older bool) tea.Cmd {
	if !m.inChat() {
		return nil
	}
	s := m.currentChat()
	if s == nil || s.ReadPending || s.Job.ThreadID == "" {
		return nil
	}
	if older && s.Cursor == "" {
		m.Notice = "이전 대화가 더 없습니다."
		return nil
	}
	s.ReadPending = true
	s.ReadSequence++
	thread, seq := m.Chat.ThreadID, s.ReadSequence
	args := supervisor.JobArgs{ThreadID: thread}
	if m.registered(s.Job) {
		args.ID = s.Job.ID
	}
	if older {
		args.Cursor = s.Cursor
	}
	c, parent := m.Config, m.Context
	return func() tea.Msg {
		if parent == nil {
			parent = context.Background()
		}
		ctx, cancel := context.WithTimeout(parent, 15*time.Second)
		defer cancel()
		var page struct {
			Thread cr.Thread `json:"thread"`
			Cursor string    `json:"cursor"`
		}
		err := supervisor.Request(ctx, c, "read", args, &page)
		return chatReadMsg{thread, seq, older, page.Thread, page.Cursor, err}
	}
}

func mergeChatTurns(first, second []cr.Turn) []cr.Turn {
	out := slices.Clone(first)
	index := map[string]int{}
	for i, turn := range out {
		index[turn.ID] = i
	}
	for _, turn := range second {
		if i, ok := index[turn.ID]; ok {
			out[i] = turn
		} else {
			index[turn.ID] = len(out)
			out = append(out, turn)
		}
	}
	return out
}

func chatBlocked(j jobs.Job) string {
	if j.Paused {
		return "일시 중지된 대화입니다. 재개를 누른 뒤 전송하세요. 초안은 유지됩니다."
	}
	if j.State == "external_writer" {
		return "다른 GUI/터미널이 이 대화를 사용 중입니다. 해당 대화의 실행권이 해제된 뒤 전송하세요."
	}
	if j.Kind == "group" || j.ThreadID == "" {
		if j.Managed && j.Kind != "group" && j.ID != "" {
			return "새 대화를 준비 중입니다. 작업 상태가 갱신된 뒤 전송하세요. 초안은 유지됩니다."
		}
		return "채팅할 하위 대화를 선택하세요."
	}
	return ""
}

func (m *Model) sendChat(text string) tea.Cmd {
	s := m.currentChat()
	if s == nil || strings.TrimSpace(text) == "" {
		return nil
	}
	if s.Uncertain {
		m.Notice = "전송 결과를 확인해야 합니다. 전송 확인 버튼으로 기존 요청부터 조회하세요."
		return nil
	}
	if s.Pending != "" {
		m.Notice = "전송 결과를 확인 중입니다. 같은 지시를 다시 보내지 않습니다."
		return nil
	}
	j, ok := m.selected()
	if !ok {
		return nil
	}
	s.Job = j
	if reason := chatBlocked(j); reason != "" {
		s.Error, m.Notice = reason, reason
		return nil
	}
	s.PendingText, s.Error = text, ""
	if !m.registered(j) || !j.Managed {
		return m.chatMutation(s, "register", supervisor.JobArgs{ThreadID: j.ThreadID})
	}
	return m.chatMutation(s, "send", supervisor.JobArgs{ID: j.ID, Text: text})
}

func (m Model) chatMutation(s *chatSession, command string, args supervisor.JobArgs) tea.Cmd {
	s.Pending = command
	s.SendSequence++
	seq, thread := s.SendSequence, s.Job.ThreadID
	call := m.request(command, args)
	return func() tea.Msg { return chatSendMsg{thread, command, seq, call().(replyMsg)} }
}

func (m *Model) checkChatOperation() tea.Cmd {
	s := m.currentChat()
	if s == nil || !s.Uncertain || s.OperationID == "" || s.Checking {
		return nil
	}
	s.Checking = true
	thread, seq := s.Job.ThreadID, s.SendSequence
	call := m.appCall("operation.read", supervisor.JobArgs{OperationID: s.OperationID})
	return func() tea.Msg { return chatSendMsg{thread, "check", seq, call().(replyMsg)} }
}

func (m *Model) acceptChatJob(s *chatSession, j jobs.Job) {
	s.Job = j
	found := false
	for i, saved := range m.Status.Jobs {
		if saved.ID == j.ID {
			m.Status.Jobs[i], found = j, true
		}
	}
	if !found {
		m.Status.Jobs = append(m.Status.Jobs, j)
	}
	if m.CatalogReady {
		m.Catalog = catalog.Merge(m.Catalog, m.Status.Jobs)
	}
	if m.inChat() && m.Chat.ThreadID == j.ThreadID {
		m.SelectedID = j.ID
	}
}

func (m *Model) chatUpdate(msg tea.Msg) (tea.Cmd, bool) {
	switch v := msg.(type) {
	case chatReadMsg:
		s := m.Chat.Sessions[v.ThreadID]
		if s == nil || s.ReadSequence != v.Sequence {
			return nil, true
		}
		s.ReadPending = false
		if v.Error != nil {
			s.ReadError = "대화 조회 실패: " + v.Error.Error()
			return nil, true
		}
		if v.Thread.ID != v.ThreadID {
			s.ReadError = "요청한 대화와 조회 결과가 달라 표시하지 않았습니다."
			return nil, true
		}
		active := m.inChat() && m.Chat.ThreadID == v.ThreadID
		oldLines := m.Detail.TotalLineCount()
		if v.Older {
			v.Thread.Turns = mergeChatTurns(v.Thread.Turns, s.Thread.Turns)
			s.Cursor, s.OlderLoaded = v.Cursor, true
		} else {
			v.Thread.Turns = mergeChatTurns(s.Thread.Turns, v.Thread.Turns)
			if !s.OlderLoaded {
				s.Cursor = v.Cursor
			}
		}
		s.Thread, s.ReadReady, s.ReadError = v.Thread, true, ""
		if active {
			m.prepareDetail()
			if v.Older && !m.AutoFollow {
				m.Detail.SetYOffset(m.Detail.YOffset() + max(0, m.Detail.TotalLineCount()-oldLines))
			}
		}
		return nil, true
	case chatSendMsg:
		s := m.Chat.Sessions[v.ThreadID]
		if s == nil || s.SendSequence != v.Sequence {
			return nil, true
		}
		active := m.inChat() && m.Chat.ThreadID == v.ThreadID
		if v.Command == "check" {
			s.Checking = false
			if v.Reply.Error != nil {
				s.Error = "기존 요청을 확인할 수 없습니다: " + v.Reply.Error.Error()
				return nil, true
			}
			var op store.Operation
			if err := json.Unmarshal(v.Reply.Data, &op); err != nil || op.ID != s.OperationID || op.Command != s.UnknownCommand {
				s.Error = "기존 요청 결과를 확인할 수 없습니다. 재전송하지 말고 기록을 확인하세요."
				return nil, true
			}
			switch op.State {
			case "done":
				s.Uncertain, s.Pending = false, s.UnknownCommand
				return m.chatUpdate(chatSendMsg{v.ThreadID, s.Pending, v.Sequence, replyMsg{Data: op.Result}})
			case "failed":
				s.Uncertain = false
				s.Error = "요청 실패 확인: " + op.Error
			default:
				s.Error = "전송 결과 미확정 · 재전송하지 않습니다. 대화와 기록을 확인하세요."
			}
			return nil, true
		}
		if s.Pending != v.Command {
			return nil, true
		}
		s.Pending = ""
		if v.Reply.Error != nil {
			s.Error = v.Reply.Error.Error()
			if ambiguousUIError(v.Reply.Error) {
				s.Uncertain, s.OperationID, s.UnknownCommand = true, v.Reply.OperationID, v.Command
				s.Error += " · 전송 결과 미확정. 전송 확인으로 조회하기 전에는 다시 보내지 않습니다."
			}
			if active {
				m.Notice = s.Error
			}
			return nil, true
		}
		s.Error = ""
		if v.Command == "register" {
			var j jobs.Job
			if err := json.Unmarshal(v.Reply.Data, &j); err != nil || j.ThreadID != v.ThreadID || !j.Managed || j.ID == "" {
				s.Error = "대화 등록 결과를 확인할 수 없습니다. 초안을 유지합니다."
				return nil, true
			}
			m.acceptChatJob(s, j)
			if reason := chatBlocked(j); reason != "" {
				s.Error = reason
				return m.poll(), true
			}
			return m.chatMutation(s, "send", supervisor.JobArgs{ID: j.ID, Text: s.PendingText}), true
		}
		if active {
			m.rememberInput(s.PendingText)
			if m.Input.Value() == s.PendingText {
				m.Input.Reset()
			}
			s.Composer = m.captureComposer()
			m.AutoFollow = true
			m.Notice = "지시를 접수했습니다. 이 대화에서 답변과 실행 상태를 확인하세요."
			m.Panel, m.DetailText, m.LogsOpen = "", "", false
			return tea.Batch(m.poll(), m.refreshChat(false)), true
		}
		c := &s.Composer
		if len(c.History) == 0 || c.History[len(c.History)-1] != s.PendingText {
			c.History = append(c.History, s.PendingText)
			if len(c.History) > 100 {
				c.History = c.History[len(c.History)-100:]
			}
		}
		c.HistoryIndex, c.HistoryDraft = len(c.History), ""
		if c.Draft == s.PendingText {
			c.Draft = ""
		}
		return m.poll(), true
	}
	return nil, false
}

func (m Model) chatText() string {
	s := m.currentChat()
	if s == nil {
		return ""
	}
	j := s.Job
	if current, ok := m.selected(); ok {
		j = current
	}
	account := j.Account
	if account == "" {
		account = "전송 시 우선순위 자동 선택"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n계정 %s · %s", j.Title, account, display.State(j.State))
	if j.Model != "" {
		fmt.Fprintf(&b, " · %s", j.Model)
	}
	b.WriteString("\n")
	if j.Paused || j.State == "external_writer" {
		b.WriteString(chatBlocked(j) + "\n")
	}
	if len(j.PendingRequests) > 0 {
		fmt.Fprintf(&b, "응답/승인 대기 %d개 · /requests에서 답변하세요.\n", len(j.PendingRequests))
	}
	if j.LastError != "" {
		b.WriteString("실행 상태: " + j.LastError + "\n")
	}
	if s.Pending != "" {
		b.WriteString("지시 접수 중… 초안을 유지하고 중복 전송을 막습니다.\n")
	}
	if s.Error != "" {
		b.WriteString(s.Error + "\n")
	}
	if s.ReadError != "" {
		b.WriteString(s.ReadError + "\n")
	}
	b.WriteString("\n")
	for _, turn := range s.Thread.Turns {
		for _, item := range turn.Items {
			switch item.Type {
			case "userMessage":
				b.WriteString("사용자\n")
				for _, part := range item.Content {
					if part.Type == "text" {
						b.WriteString(part.Text + "\n")
					}
				}
				b.WriteString("\n")
			case "agentMessage":
				b.WriteString("Codex\n" + item.Text + "\n\n")
			case "commandExecution":
				fmt.Fprintf(&b, "도구 · %s · %s\n%s\n\n", item.Command, item.Status, item.AggregatedOutput)
			case "fileChange":
				fmt.Fprintf(&b, "파일 변경 · %s\n", item.Status)
				for _, change := range item.Changes {
					b.WriteString(change.Path + "\n" + change.Diff + "\n")
				}
				b.WriteString("\n")
			default:
				fmt.Fprintf(&b, "처리 · %s · %s\n", item.Type, item.Status)
				if item.Text != "" {
					b.WriteString(item.Text + "\n")
				}
			}
		}
		if turn.Error != nil {
			b.WriteString("오류: " + turn.Error.Message + "\n\n")
		}
	}
	if !s.ReadReady {
		b.WriteString("최근 대화를 불러오는 중…\n")
	} else if len(s.Thread.Turns) == 0 {
		b.WriteString("이 대화에 첫 지시를 입력하세요.\n")
	}
	return b.String()
}
