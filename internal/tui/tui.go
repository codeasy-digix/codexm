package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/activity"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/display"
	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/supervisor"
	"github.com/codeasy-digix/codexm/internal/terminal"
)

type readyMsg struct {
	Config config.Config
	Error  error
}
type pollMsg struct {
	Status supervisor.Status
	Error  error
}
type replyMsg struct {
	Command string
	Data    json.RawMessage
	Error   error
	Epoch   uint64
}
type tickMsg time.Time
type openMsg struct {
	Launch terminal.Launch
	Error  error
}

type Model struct {
	Config           config.Config
	Self             string
	ClientID         string
	Status           supervisor.Status
	Names            []string
	Selected         int
	SelectedID       string
	Focus            string
	Screen           string
	Filter           string
	StateFilter      string
	Target           string
	Input            textarea.Model
	Detail           viewport.Model
	Threads          []cr.Thread
	Cursor           string
	ThreadSelected   int
	Wizard           []string
	WizardIndex      int
	New              supervisor.NewArgs
	Width, Height    int
	Notice           string
	Loading          bool
	Ready            bool
	DetailText       string
	ReadCursor       string
	WantOpen         bool
	WantRead         bool
	LogsOpen         bool
	LogsJob          string
	Panel            string
	Usage            display.UsageReport
	PaletteIndex     int
	PaletteDismissed bool
	ViewEpoch        uint64
	AutoFollow       bool
}

func New(c config.Config, self string) Model {
	in := textarea.New()
	in.Placeholder = "작업 검색 · / 관리 명령 목록"
	in.CharLimit = 65536
	in.SetHeight(3)
	in.ShowLineNumbers = false
	in.Focus()
	m := Model{Config: c, Self: self, ClientID: jobs.ID(), Input: in, Detail: viewport.New(), Width: 100, Height: 30, Focus: "input", Screen: "agents", Target: "search", AutoFollow: true, Notice: "감시 프로그램 연결 중…"}
	if c.Manager.Account == "" {
		m.Screen = "setup"
		m.Focus = "list"
		m.Input.Blur()
		m.Names, _ = accounts.List(c)
		m.Notice = "최초 관리자 계정을 선택하세요. Enter로 저장하며 이후 실행에서 다시 불러옵니다."
	}
	return m
}
func Run(c config.Config, self string) error {
	p := tea.NewProgram(New(c, self))
	result, e := p.Run()
	if m, ok := result.(Model); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = supervisor.Request(ctx, m.Config, "detach", supervisor.JobArgs{ClientID: m.ClientID}, nil)
		cancel()
	}
	return e
}
func (m Model) Init() tea.Cmd {
	if m.Screen == "setup" {
		return nil
	}
	return m.start(false)
}
func (m Model) start(choose bool) tea.Cmd {
	c := m.Config
	name := ""
	if choose && len(m.Names) > 0 {
		name = m.Names[min(m.Selected, len(m.Names)-1)]
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if choose {
			r, e := cr.Resolve(ctx, c)
			if e != nil {
				return readyMsg{c, e}
			}
			a := accounts.New(r).Read(ctx, name)
			if !a.Authenticated || a.Error != "" {
				return readyMsg{c, fmt.Errorf("%s: %s", name, a.Error)}
			}
			c.Manager.Account = name
			if e = c.Save(); e != nil {
				return readyMsg{c, e}
			}
		}
		e := supervisor.Start(ctx, c, m.Self)
		return readyMsg{c, e}
	}
}
func tick() tea.Cmd { return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }) }
func (m Model) poll() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = supervisor.Request(ctx, m.Config, "client", supervisor.JobArgs{ClientID: m.ClientID}, nil)
		var st supervisor.Status
		e := supervisor.Request(ctx, m.Config, "status", nil, &st)
		return pollMsg{st, e}
	}
}
func (m Model) request(command string, args any) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()
		var data json.RawMessage
		e := supervisor.Request(ctx, m.Config, command, args, &data)
		return replyMsg{Command: command, Data: data, Error: e, Epoch: m.ViewEpoch}
	}
}
func (m Model) selected() (jobs.Job, bool) {
	for _, j := range m.Status.Jobs {
		if j.ID == m.SelectedID {
			return j, true
		}
	}
	return jobs.Job{}, false
}
func (m Model) filtered() []jobs.Job {
	out := []jobs.Job{}
	for _, j := range m.Status.Jobs {
		if m.StateFilter != "" && j.State != m.StateFilter {
			continue
		}
		if m.Filter != "" && !strings.Contains(strings.ToLower(j.Title+" "+j.ID+" "+j.Account+" "+j.Cwd), strings.ToLower(m.Filter)) {
			continue
		}
		out = append(out, j)
	}
	return out
}
func (m *Model) selectIndex(delta int) {
	js := m.filtered()
	m.Selected = max(0, min(m.Selected+delta, len(js)-1))
	if len(js) > 0 {
		m.SelectedID = js[m.Selected].ID
	}
	m.DetailText = ""
	m.Panel = ""
	m.LogsOpen = false
	m.ViewEpoch++
	m.AutoFollow = true
	m.Detail.GotoBottom()
}
func (m *Model) focusInput(value string) {
	m.Focus = "input"
	m.Input.SetValue(value)
	m.Input.CursorEnd()
	m.Input.Focus()
	m.PaletteIndex = 0
	m.PaletteDismissed = false
}
func (m *Model) returnInput() { m.Focus = "input"; m.Input.Focus() }
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.prepareDetail()
	var cmd tea.Cmd
	switch v := msg.(type) {
	case readyMsg:
		m.Loading = false
		if v.Error != nil {
			m.Notice = v.Error.Error()
			return m, nil
		}
		m.Config = v.Config
		m.Ready = true
		m.Screen = "agents"
		m.Focus = "input"
		m.Input.Focus()
		m.Notice = "연결됨 · " + m.Config.Supervisor.TmuxSession + " · tmux attach -t " + m.Config.Supervisor.TmuxSession
		return m, tea.Batch(m.poll(), tick())
	case tickMsg:
		if m.Ready {
			if m.LogsOpen {
				return m, tea.Batch(m.poll(), m.request("logs", supervisor.JobArgs{ID: m.LogsJob}), tick())
			}
			if m.Panel == "usage" {
				return m, tea.Batch(m.poll(), m.request("usage", nil), tick())
			}
			return m, tea.Batch(m.poll(), tick())
		}
		return m, nil
	case pollMsg:
		if v.Error != nil {
			m.Notice = "연결 끊김: " + v.Error.Error() + " · R로 재연결"
			return m, nil
		}
		old := m.Status.ManagerDecision
		m.Status = v.Status
		if d := v.Status.ManagerDecision; d != nil && (old == nil || old.ID != d.ID || old.State != d.State) {
			if d.Decision != nil {
				m.Notice = "관리자: " + d.Decision.Summary
			} else if d.Error != "" {
				m.Notice = "관리자 대기: " + d.Error
			}
		}
		js := m.filtered()
		if m.SelectedID == "" && len(js) > 0 {
			m.SelectedID = js[0].ID
		}
		found := false
		for i, j := range js {
			if j.ID == m.SelectedID {
				m.Selected = i
				found = true
			}
		}
		if !found && len(js) > 0 {
			m.Selected = min(m.Selected, len(js)-1)
			m.SelectedID = js[m.Selected].ID
		}
	case replyMsg:
		switch v.Command {
		case "logs", "usage", "read", "events", "settings", "threads", "accounts":
			if v.Epoch != m.ViewEpoch {
				return m, nil
			}
		}
		m.Loading = false
		if v.Error != nil {
			m.Notice = v.Error.Error()
			if v.Command == "new" {
				m.WizardIndex = 0
				m.focusInput(m.New.Cwd)
			}
			return m, nil
		}
		m.Notice = v.Command + " 처리됨"
		switch v.Command {
		case "new", "resume":
			var j jobs.Job
			if json.Unmarshal(v.Data, &j) == nil {
				m.SelectedID = j.ID
				m.Screen = "agents"
				m.Focus = "input"
				m.Input.Focus()
				m.DetailText = ""
				m.Panel = ""
				if m.WantOpen && j.Kind != "group" {
					m.WantOpen = false
					return m, tea.Batch(m.poll(), m.openID(j.ID))
				}
				if m.WantRead {
					m.WantRead = false
					return m, tea.Batch(m.poll(), m.request("read", supervisor.JobArgs{ID: j.ID}))
				}
			}
		case "threads":
			var p struct {
				Threads []cr.Thread `json:"threads"`
				Cursor  string      `json:"cursor"`
			}
			_ = json.Unmarshal(v.Data, &p)
			m.Threads = p.Threads
			m.Cursor = p.Cursor
			m.ThreadSelected = 0
			m.Screen = "resume"
			m.Focus = "list"
			m.Input.Blur()
		case "read":
			var p struct {
				Thread cr.Thread `json:"thread"`
				Cursor string    `json:"cursor"`
			}
			_ = json.Unmarshal(v.Data, &p)
			m.DetailText = transcript(p.Thread)
			m.prepareDetail()
			m.Detail.GotoBottom()
			m.ReadCursor = p.Cursor
			m.Screen = "agents"
			m.Panel = "read"
		case "accounts":
			var as []accounts.Snapshot
			_ = json.Unmarshal(v.Data, &as)
			m.Status.Accounts = as
			m.DetailText = ""
			m.Panel = "accounts"
			m.Screen = "agents"
		case "logs":
			if !m.LogsOpen {
				return m, nil
			}
			var report jobs.WorkLog
			if err := json.Unmarshal(v.Data, &report); err != nil {
				m.Notice = err.Error()
				return m, nil
			}
			scope := "전체 세션"
			if m.LogsJob != "" {
				scope = "선택한 작업 " + m.LogsJob
			}
			m.DetailText = "작업로그 · " + scope + "\n전체: /logs all · 선택 작업: /logs\n\n" + activity.Format(report)
			m.Panel = "logs"
			m.Screen = "agents"
		case "usage":
			if m.Panel != "usage" {
				return m, nil
			}
			if e := json.Unmarshal(v.Data, &m.Usage); e != nil {
				m.Notice = e.Error()
				return m, nil
			}
			m.DetailText = ""
			m.Screen = "agents"
		case "events":
			var es []jobs.Event
			if e := json.Unmarshal(v.Data, &es); e != nil {
				m.Notice = e.Error()
				return m, nil
			}
			m.DetailText = display.Events(es)
			m.Panel = "events"
			m.Screen = "agents"
		case "settings":
			var c config.Config
			if e := json.Unmarshal(v.Data, &c); e != nil {
				m.Notice = e.Error()
				return m, nil
			}
			c.Path = m.Config.Path
			m.DetailText = display.Settings(c)
			m.Panel = "settings"
			m.Screen = "agents"
		case "shutdown":
			m.Notice = "작업과 감시 프로그램의 종료를 요청했습니다."
			return m, tea.Quit
		case "set-manager":
			var a string
			_ = json.Unmarshal(v.Data, &a)
			m.Config.Manager.Account = a
			m.Status.ManagerAccount = a
		}
		return m, m.poll()
	case openMsg:
		if v.Error != nil {
			m.Notice = v.Error.Error()
		} else {
			m.Notice = v.Launch.String()
		}
		return m, m.poll()
	case tea.WindowSizeMsg:
		m.Width = max(20, v.Width)
		m.Height = max(10, v.Height)
		m.Input.SetWidth(max(10, m.Width-4))
		m.Detail.SetWidth(max(10, m.Width-5))
		m.Detail.SetHeight(max(3, m.Height-13))
	case tea.KeyPressMsg:
		key := v.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.Screen == "setup" {
			switch key {
			case "up", "k":
				m.Selected = max(0, m.Selected-1)
			case "down", "j":
				m.Selected = min(len(m.Names)-1, m.Selected+1)
			case "enter":
				if len(m.Names) > 0 && !m.Loading {
					m.Loading = true
					m.Notice = "계정과 런타임 확인 중…"
					return m, m.start(true)
				}
			case "q", "esc":
				return m, tea.Quit
			}
			return m, nil
		}
		if key == "esc" && len(m.commandMatches()) > 0 {
			m.PaletteDismissed = true
			return m, nil
		}
		if key == "esc" {
			m.ViewEpoch++
			m.AutoFollow = true
			m.LogsOpen = false
			m.Panel = ""
			m.DetailText = ""
			if m.Screen == "new" {
				m.Input.Reset()
			}
			m.Focus = "input"
			m.Screen = "agents"
			m.Input.Focus()
			m.PaletteDismissed = true
			return m, nil
		}
		if m.Screen == "import-account" {
			if key == "enter" && len(m.Threads) > 0 {
				account := strings.TrimSpace(m.Input.Value())
				m.Input.Reset()
				m.WantOpen = true
				return m, m.request("resume", supervisor.JobArgs{ThreadID: m.Threads[m.ThreadSelected].ID, Account: account})
			}
			m.Input, cmd = m.Input.Update(msg)
			return m, cmd
		}
		if m.Screen == "new" {
			if key == "enter" {
				return m, m.wizardNext()
			}
			m.Input, cmd = m.Input.Update(msg)
			return m, cmd
		}
		if m.Focus == "input" {
			if cs := m.commandMatches(); len(cs) > 0 {
				switch key {
				case "up", "ctrl+p":
					m.PaletteIndex = (m.PaletteIndex + len(cs) - 1) % len(cs)
					return m, nil
				case "down", "ctrl+n":
					m.PaletteIndex = (m.PaletteIndex + 1) % len(cs)
					return m, nil
				case "tab":
					return m, m.chooseCommand(false)
				case "enter":
					text := strings.TrimPrefix(m.Input.Value(), "/")
					for i, c := range cs {
						if c.Name == text {
							if c.NeedsArg {
								m.PaletteIndex = i
								return m, m.chooseCommand(false)
							}
							m.Input.Reset()
							return m, m.slash("/" + text)
						}
					}
					return m, m.chooseCommand(true)
				}
			}
			if key == "pgup" || key == "pgdown" {
				m.AutoFollow = false
				m.Detail, cmd = m.Detail.Update(msg)
				if m.Detail.AtBottom() {
					m.AutoFollow = true
				}
				return m, cmd
			}
			if key == "enter" || key == "ctrl+enter" {
				text := m.Input.Value()
				m.Input.Reset()
				return m, m.submit(text)
			}
			if key == "alt+enter" || key == "shift+enter" {
				m.Input.InsertString("\n")
				return m, nil
			}
			if key == "tab" {
				m.Focus = "list"
				m.Input.Blur()
				return m, nil
			}
			if key == "shift+tab" {
				m.Focus = "detail"
				m.Input.Blur()
				return m, nil
			}
			before := m.Input.Value()
			m.Input, cmd = m.Input.Update(msg)
			if m.Input.Value() != before {
				m.PaletteIndex = 0
				m.PaletteDismissed = false
			}
			return m, cmd
		}
		if m.Screen == "resume" {
			switch key {
			case "up", "k":
				m.ThreadSelected = max(0, m.ThreadSelected-1)
			case "down", "j":
				m.ThreadSelected = min(len(m.Threads)-1, m.ThreadSelected+1)
			case "enter":
				if len(m.Threads) > 0 {
					thread := m.Threads[m.ThreadSelected].ID
					for _, j := range m.Status.Jobs {
						if j.ThreadID == thread {
							m.SelectedID = j.ID
							m.Screen = "agents"
							m.returnInput()
							return m, m.openID(j.ID)
						}
					}
					m.Screen = "import-account"
					m.focusInput(m.Config.WorkerAccounts.Priority[0])
					m.Notice = "가져올 대화의 작업 계정 · Enter로 새 터미널 열기"
					return m, nil
				}
			case "v":
				if len(m.Threads) > 0 {
					m.WantRead = true
					return m, m.request("resume", supervisor.JobArgs{ThreadID: m.Threads[m.ThreadSelected].ID})
				}
			case "/":
				m.focusInput("/chats ")
			case "n":
				return m, m.request("threads", supervisor.JobArgs{Cursor: m.Cursor})
			}
			return m, nil
		}
		if m.Focus == "detail" {
			if key == "/" {
				m.focusInput("/")
				return m, nil
			}
			if key == "shift+tab" {
				m.Focus = "list"
				return m, nil
			}
			if key == "tab" {
				m.returnInput()
				return m, nil
			}
			m.Detail, cmd = m.Detail.Update(msg)
			return m, cmd
		}
		switch key {
		case "q":
			return m, tea.Quit
		case "up", "k":
			m.selectIndex(-1)
		case "down", "j":
			m.selectIndex(1)
		case "tab", "i":
			m.returnInput()
		case "shift+tab":
			m.Focus = "detail"
		case "enter", "a":
			if j, ok := m.selected(); ok {
				return m, m.openID(j.ID)
			}
		case "v":
			if j, ok := m.selected(); ok && j.ThreadID != "" {
				m.Focus = "detail"
				return m, m.request("read", supervisor.JobArgs{ID: j.ID})
			}
		case "/":
			m.focusInput("/")
		case "l":
			m.ViewEpoch++
			m.Panel = "logs"
			m.LogsJob = ""
			m.LogsOpen = true
			m.Focus = "detail"
			return m, m.request("logs", nil)
		case "n":
			m.beginNew()
		case "r":
			m.ViewEpoch++
			return m, m.request("threads", supervisor.JobArgs{})
		case "R":
			m.Notice = "재연결 중…"
			return m, m.start(false)
		case "m":
			m.Target = "manager"
			m.focusInput("")
			m.Notice = "관리자에게 지시합니다. 모델 호출이 발생합니다."
		case "p", "s", "c":
			j, ok := m.selected()
			if ok {
				command := map[string]string{"p": "pause", "s": "resume-job", "c": "complete"}[key]
				return m, m.request(command, supervisor.JobArgs{ID: j.ID})
			}
		case "f":
			m.focusInput("/filter ")
		}
	default:
		if m.Focus == "input" || m.Screen == "new" {
			before := m.Input.Value()
			m.Input, cmd = m.Input.Update(msg)
			if m.Input.Value() != before {
				m.PaletteIndex = 0
				m.PaletteDismissed = false
			}
			return m, cmd
		}
	}
	return m, nil
}
func (m *Model) beginNew() {
	m.Screen = "new"
	m.Target = "search"
	m.Wizard = []string{"cwd", "account", "title"}
	m.WizardIndex = 0
	m.New = supervisor.NewArgs{Kind: "chat"}
	cwd, _ := os.Getwd()
	m.focusInput(cwd)
	m.Notice = "새 채팅 · 작업 폴더 (Enter 다음, Esc 취소)"
}
func (m *Model) wizardNext() tea.Cmd {
	value := strings.TrimSpace(m.Input.Value())
	switch m.Wizard[m.WizardIndex] {
	case "cwd":
		if info, e := os.Stat(config.Expand(value)); e != nil || !info.IsDir() {
			m.Notice = "사용할 작업 폴더를 입력하세요."
			return nil
		}
		m.New.Cwd = value
	case "account":
		if !slices.Contains(m.Config.WorkerAccounts.Priority, value) {
			m.Notice = "작업 계정: " + strings.Join(m.Config.WorkerAccounts.Priority, ", ")
			return nil
		}
		m.New.Account = value
	case "title":
		m.New.Title = value
	}
	m.WizardIndex++
	if m.WizardIndex >= len(m.Wizard) {
		m.Loading = true
		m.WantOpen = true
		m.Input.Reset()
		m.Notice = "새 채팅 등록 중… 첫 지시는 공식 Codex 화면에서 입력하세요."
		return m.request("new", m.New)
	}
	next := m.Wizard[m.WizardIndex]
	defaults := map[string]string{"account": m.Config.WorkerAccounts.Priority[0], "title": filepath.Base(m.New.Cwd)}
	labels := map[string]string{"account": "최초 작업 계정", "title": "채팅 제목"}
	m.Input.SetValue(defaults[next])
	m.Notice = "새 채팅 · " + labels[next]
	return nil
}

func (m Model) openID(id string) tea.Cmd {
	c, self := m.Config, m.Self
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		opened, e := terminal.Open(ctx, c, self, id)
		return openMsg{opened, e}
	}
}

func (m *Model) submit(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if strings.HasPrefix(text, "/") {
		return m.slash(text)
	}
	if m.Target == "manager" {
		j, _ := m.selected()
		m.Panel = ""
		m.DetailText = ""
		m.LogsOpen = false
		m.ViewEpoch++
		m.Notice = "관리 지시 등록됨 · 모델 판단 대기"
		return m.request("manager", supervisor.JobArgs{ID: j.ID, Text: text})
	}
	m.Filter = text
	m.Selected = 0
	m.SelectedID = ""
	m.Panel = ""
	m.DetailText = ""
	m.LogsOpen = false
	m.ViewEpoch++
	m.Notice = "작업 검색: " + text + " · /filter 로 검색 해제 · 대화는 /open"
	return m.poll()
}
func (m *Model) slash(text string) tea.Cmd {
	parts := strings.SplitN(strings.TrimSpace(text), " ", 2)
	command := strings.TrimPrefix(parts[0], "/")
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}
	j, _ := m.selected()
	m.ViewEpoch++
	m.LogsOpen = false
	m.Panel = ""
	m.DetailText = ""
	m.Screen = "agents"
	m.PaletteDismissed = true
	m.Detail.GotoTop()
	switch command {
	case "quit", "exit":
		return tea.Quit
	case "agents":
		m.Target = "search"
		m.Focus = "list"
		m.Input.Blur()
	case "new":
		m.beginNew()
	case "chats":
		m.Loading = true
		return m.request("threads", supervisor.JobArgs{Search: arg})
	case "status":
		m.Panel = "manager-status"
		return m.poll()
	case "info":
		m.Panel = "status"
		return m.poll()
	case "open", "attach":
		if j.ID == "" {
			m.Notice = "작업을 선택하세요."
			return nil
		}
		m.Notice = "새 터미널에 공식 Codex 화면 연결 중…"
		return m.openID(j.ID)
	case "manager":
		m.Target = "manager"
		m.Notice = "입력 대상: 관리자 · 모델 호출 · /filter 로 검색 입력 복귀"
		if arg != "" {
			return m.request("manager", supervisor.JobArgs{ID: j.ID, Text: arg})
		}
	case "accounts":
		m.Notice = "계정 한도 조회 중… (모델 호출 없음)"
		return m.request("accounts", nil)
	case "manager-account":
		if arg == "" {
			m.Notice = "저장된 관리자 계정: " + m.Config.Manager.Account
			m.Panel = "manager-status"
			return nil
		}
		return m.request("set-manager", supervisor.JobArgs{Account: arg})
	case "settings":
		return m.request("settings", nil)
	case "usage":
		m.Panel = "usage"
		return tea.Batch(m.poll(), m.request("usage", nil))
	case "logs":
		if arg != "" && arg != "all" {
			m.Notice = "/logs 또는 /logs all"
			return nil
		}
		m.LogsJob = ""
		m.LogsOpen = true
		if arg != "all" {
			if j.ID == "" {
				m.LogsOpen = false
				m.Notice = "작업을 선택하거나 /logs all을 사용하세요."
				return nil
			}
			m.LogsJob = j.ID
		}
		m.Panel = "logs"
		return m.request("logs", supervisor.JobArgs{ID: m.LogsJob})
	case "older":
		if j.ID != "" && m.ReadCursor != "" {
			return m.request("read", supervisor.JobArgs{ID: j.ID, Cursor: m.ReadCursor})
		}
		m.Notice = "이전 대화 페이지가 없습니다."
	case "pause", "resume", "resume-job", "manage", "stop", "complete", "events", "read":
		if j.ID == "" {
			m.Notice = "작업을 선택하세요."
			return nil
		}
		api := command
		if api == "resume" {
			api = "resume-job"
		}
		return m.request(api, supervisor.JobArgs{ID: j.ID})
	case "filter":
		m.Target = "search"
		m.Filter = arg
		m.Selected = 0
		m.SelectedID = ""
		m.returnInput()
		return m.poll()
	case "state":
		m.StateFilter = arg
		m.Selected = 0
		m.SelectedID = ""
		return m.poll()
	case "shutdown":
		return m.request("shutdown", nil)
	case "help":
		m.DetailText = commandHelp()
	default:
		m.Notice = "codexm 관리 명령: /help · Codex 대화·명령: /open"
	}
	return nil
}

func transcript(t cr.Thread) string {
	var b strings.Builder
	b.WriteString("대화 " + t.ID + "\n" + t.Cwd + "\n\n")
	for _, turn := range t.Turns {
		for _, item := range turn.Items {
			switch item.Type {
			case "userMessage":
				b.WriteString("사용자\n")
				for _, c := range item.Content {
					if c.Type == "text" {
						b.WriteString(c.Text + "\n")
					}
				}
				b.WriteString("\n")
			case "agentMessage":
				b.WriteString("Codex\n" + item.Text + "\n\n")
			}
		}
	}
	return b.String()
}

var accent = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
var dim = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

func plain(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 32 && r != 127 {
			return r
		}
		return -1
	}, ansi.Strip(s))
}
func (m Model) View() tea.View {
	w := max(20, m.Width)
	var b strings.Builder
	if m.Screen == "setup" {
		b.WriteString(accent.Render("codexm · 최초 관리자 설정") + "\n\n" + plain(m.Notice) + "\n\n")
		if len(m.Names) == 0 {
			b.WriteString("등록된 계정이 없습니다. codex account NAME으로 로그인한 뒤 다시 실행하세요.\n")
		}
		for i, name := range m.Names {
			prefix := "  "
			if i == m.Selected {
				prefix = "› "
			}
			b.WriteString(prefix + name + "\n")
		}
		b.WriteString("\n↑↓ 선택 · Enter 확인 및 저장 · q 종료\n")
	} else {
		running := 0
		for _, j := range m.Status.Jobs {
			if jobs.Busy(j.State) {
				running++
			}
		}
		b.WriteString(ansi.Truncate(accent.Render("codexm")+fmt.Sprintf("  관리자 %s  · %s  · 실행 %d/%d", m.Config.Manager.Account, m.Status.Health, running, m.Config.Execution.MaxParallelJobs), w, "…") + "\n")
		b.WriteString(dim.Render(ansi.Truncate(plain(m.Notice), w, "…")) + "\n")
		if m.Screen == "new" || m.Screen == "import-account" {
			b.WriteString("\n" + m.Notice + "\n\n" + m.Input.View() + "\n\nEnter 다음 · Alt+Enter 줄바꿈 · Esc 취소\n")
		} else if m.Screen == "resume" {
			b.WriteString("기존 대화 · Enter 새 터미널 열기 · v 미리보기 · /chats 검색어 · n 다음 페이지\n")
			start := max(0, m.ThreadSelected-max(3, m.Height-10)+1)
			for i := start; i < len(m.Threads) && i < start+max(3, m.Height-9); i++ {
				t := m.Threads[i]
				prefix := "  "
				if i == m.ThreadSelected {
					prefix = "› "
				}
				b.WriteString(ansi.Truncate(prefix+t.ID+" "+plain(t.Name+" "+t.Preview), w, "…") + "\n")
			}
			if len(m.Threads) == 0 {
				b.WriteString("검색 결과 없음\n")
			}
			if m.Focus == "input" {
				b.WriteString(m.Input.View())
			}
		} else {
			js := m.filtered()
			listHeight := m.listHeight()
			start := max(0, m.Selected-listHeight+1)
			b.WriteString(dim.Render(ansi.Truncate("작업 목록 · Tab 선택 · Enter 새 터미널 · /filter 검색 · /state 상태", w, "…")) + "\n")
			for i := start; i < len(js) && i < start+listHeight; i++ {
				j := js[i]
				prefix := "  "
				if j.ID == m.SelectedID {
					prefix = "› "
				}
				indent := ""
				if j.ParentID != "" {
					indent = "  ↳ "
				}
				mode := ""
				if !j.Managed {
					mode = " [열람]"
				}
				if n := m.Status.Terminals[j.ID]; n > 0 {
					mode += fmt.Sprintf(" [터미널 %d]", n)
				}
				line := fmt.Sprintf("%s%s%s  %s · %s%s", prefix, indent, j.Title, display.State(j.State), j.Account, mode)
				if j.ID == m.SelectedID {
					line = accent.Render(ansi.Truncate(plain(line), w, "…"))
				}
				b.WriteString(ansi.Truncate(line, w, "…") + "\n")
			}
			if len(js) == 0 {
				b.WriteString("등록된 작업 없음 · n으로 새 작업, r로 기존 대화\n")
			}
			text := m.content()
			width := max(10, w-2)
			height := m.detailHeight()
			vp := m.Detail
			vp.SetWidth(width)
			vp.SetHeight(height)
			vp.SetContent(ansi.Wrap(plain(text), width, ""))
			b.WriteString("\n" + vp.View() + "\n")
			target := "작업 검색 (모델 호출 없음)"
			if m.Target == "manager" {
				target = "관리자 " + m.Config.Manager.Account + " (모델 호출)"
			}
			b.WriteString(m.paletteView())
			b.WriteString(accent.Render(ansi.Truncate("입력 대상: "+plain(target), w, "…")) + "\n" + m.Input.View() + "\n")
			b.WriteString(dim.Render(ansi.Truncate("/ 명령 목록 · Tab 영역 이동 · Enter 검색/실행 · Alt+Enter 줄바꿈 · PgUp/PgDn 스크롤 · /quit", w, "…")))
		}
	}
	view := tea.NewView(b.String())
	view.AltScreen = true
	return view
}
func (m Model) content() string {
	switch m.Panel {
	case "status":
		if j, ok := m.selected(); ok {
			return display.Session(j, m.Status)
		}
	case "manager-status":
		return display.Manager(m.Status, m.Config)
	case "usage":
		return display.Usage(m.Usage, m.Status, m.Config)
	case "accounts":
		return display.Accounts(m.Status, m.Config) + "\n관리자 변경: /manager-account NAME\n"
	}
	if m.DetailText != "" {
		return m.DetailText
	}
	if m.Target == "manager" {
		text := "관리자에게 관리 지시를 보내면 짧은 모델 호출이 발생합니다.\n정상 감시와 계정 전환의 모델 호출: 0\n\n"
		if d := m.Status.ManagerDecision; d != nil {
			text += "최근 판단 · " + d.State + "\n"
			if d.Decision != nil {
				text += d.Decision.Summary
			} else {
				text += d.Error
			}
		}
		return text
	}
	j, ok := m.selected()
	if !ok {
		return ""
	}
	text := display.Session(j, m.Status)
	if j.LastError != "" {
		text += "\n상태: " + j.LastError
	}
	if len(j.PendingRequests) > 0 {
		text += fmt.Sprintf("\n응답/승인 요청 %d개 · /open 으로 공식 화면에서 응답", len(j.PendingRequests))
	}

	return text
}
func (m *Model) prepareDetail() {
	width := max(10, m.Width-2)
	height := m.detailHeight()
	m.Detail.SetWidth(width)
	m.Detail.SetHeight(height)
	m.Detail.SetContent(ansi.Wrap(plain(m.content()), width, ""))
	if m.AutoFollow && m.Focus != "detail" && m.Panel == "" && m.DetailText == "" {
		m.Detail.GotoBottom()
	}
}
func (m Model) detailHeight() int {
	shown := max(1, min(len(m.filtered()), m.listHeight()))
	return max(2, m.Height-shown-10-m.paletteHeight())
}
func (m Model) listHeight() int {
	if m.paletteHeight() > 0 && m.Height < 26 {
		return 1
	}
	return max(2, min(6, m.Height/4))
}
