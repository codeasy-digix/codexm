package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/supervisor"
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
}
type tickMsg time.Time
type exitNativeMsg struct{ Error error }

type Model struct {
	Config         config.Config
	Self           string
	ClientID       string
	Status         supervisor.Status
	Names          []string
	Selected       int
	SelectedID     string
	Focus          string
	Screen         string
	Filter         string
	StateFilter    string
	Target         string
	Input          textarea.Model
	Detail         viewport.Model
	Threads        []cr.Thread
	Cursor         string
	ThreadSelected int
	Wizard         []string
	WizardIndex    int
	New            supervisor.NewArgs
	Width, Height  int
	Notice         string
	Loading        bool
	Ready          bool
	DetailText     string
	ReadCursor     string
	WantRead       bool
}

func New(c config.Config, self string) Model {
	in := textarea.New()
	in.Placeholder = "추가 지시 또는 /help"
	in.CharLimit = 65536
	in.SetHeight(3)
	in.ShowLineNumbers = false
	m := Model{Config: c, Self: self, ClientID: jobs.ID(), Input: in, Detail: viewport.New(), Width: 100, Height: 30, Focus: "list", Screen: "agents", Target: "job", Notice: "감시 프로그램 연결 중…"}
	if c.Manager.Account == "" {
		m.Screen = "setup"
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
		return replyMsg{command, data, e}
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
	m.Detail.GotoBottom()
}
func (m *Model) focusInput(value string) { m.Focus = "input"; m.Input.SetValue(value); m.Input.Focus() }
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
		m.Notice = "연결됨 · " + m.Config.Supervisor.TmuxSession + " · tmux attach -t " + m.Config.Supervisor.TmuxSession
		return m, tea.Batch(m.poll(), tick())
	case tickMsg:
		if m.Ready {
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
		m.Loading = false
		if v.Error != nil {
			m.Notice = v.Error.Error()
			return m, nil
		}
		m.Notice = v.Command + " 처리됨"
		switch v.Command {
		case "new", "resume":
			var j jobs.Job
			if json.Unmarshal(v.Data, &j) == nil {
				m.SelectedID = j.ID
				m.Screen = "agents"
				m.Focus = "list"
				m.Input.Blur()
				m.DetailText = ""
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
			m.Focus = "detail"
		case "accounts":
			var as []accounts.Snapshot
			_ = json.Unmarshal(v.Data, &as)
			var b strings.Builder
			b.WriteString("관리자: " + m.Config.Manager.Account + "\n작업 계정 순서: " + strings.Join(m.Config.WorkerAccounts.Priority, " → ") + "\n\n")
			for _, a := range as {
				b.WriteString(accounts.Describe(a) + "\n확인: " + a.CheckedAt.Local().Format("01-02 15:04:05") + "\n\n")
			}
			b.WriteString("관리자 변경: /account NAME\n최초 작업 계정은 새 작업 화면에서 선택합니다. account-backup 작업 사용 중 상위 계정 확인: 600초마다.")
			m.DetailText = b.String()
			m.Screen = "agents"
			m.Focus = "detail"
		case "events", "usage", "settings":
			var pretty any
			_ = json.Unmarshal(v.Data, &pretty)
			b, _ := json.MarshalIndent(pretty, "", "  ")
			m.DetailText = string(b)
			m.Screen = "agents"
			m.Focus = "detail"
		case "attach":
			var a struct {
				Binary   string `json:"binary"`
				Socket   string `json:"socket"`
				ThreadID string `json:"thread_id"`
			}
			_ = json.Unmarshal(v.Data, &a)
			cmd := exec.Command(a.Binary, "-c", "check_for_update_on_startup=false", "--remote", "unix://"+a.Socket, "resume", a.ThreadID)
			return m, tea.ExecProcess(cmd, func(e error) tea.Msg { return exitNativeMsg{e} })
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
	case exitNativeMsg:
		if v.Error != nil {
			m.Notice = v.Error.Error()
		} else {
			m.Notice = "Codex 화면 접속을 종료했습니다. 감시는 계속됩니다."
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
		if key == "esc" {
			m.Focus = "list"
			m.Screen = "agents"
			m.Input.Blur()
			m.Input.Reset()
			m.Target = "job"
			return m, nil
		}
		if m.Screen == "new" {
			if key == "enter" {
				return m, m.wizardNext()
			}
			m.Input, cmd = m.Input.Update(msg)
			return m, cmd
		}
		if m.Focus == "input" {
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
			m.Input, cmd = m.Input.Update(msg)
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
					return m, m.request("resume", supervisor.JobArgs{ThreadID: m.Threads[m.ThreadSelected].ID})
				}
			case "v":
				if len(m.Threads) > 0 {
					m.WantRead = true
					return m, m.request("resume", supervisor.JobArgs{ThreadID: m.Threads[m.ThreadSelected].ID})
				}
			case "/":
				m.focusInput("/resume ")
			case "n":
				return m, m.request("threads", supervisor.JobArgs{Cursor: m.Cursor})
			}
			return m, nil
		}
		if m.Focus == "detail" {
			if key == "tab" {
				m.focusInput("")
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
			m.focusInput("")
		case "enter", "v":
			if j, ok := m.selected(); ok && j.ThreadID != "" {
				m.Focus = "detail"
				return m, m.request("read", supervisor.JobArgs{ID: j.ID})
			}
		case "/":
			m.focusInput("/")
		case "n":
			m.beginNew()
		case "r":
			return m, m.request("threads", supervisor.JobArgs{})
		case "R":
			m.Notice = "재연결 중…"
			return m, m.start(false)
		case "m":
			m.Target = "manager"
			m.focusInput("")
			m.Notice = "관리자에게 지시합니다. 모델 호출이 발생합니다."
		case "p", "s", "c", "a":
			j, ok := m.selected()
			if ok {
				command := map[string]string{"p": "pause", "s": "resume-job", "c": "complete", "a": "attach"}[key]
				return m, m.request(command, supervisor.JobArgs{ID: j.ID})
			}
		case "f":
			m.focusInput("/filter ")
		}
	default:
		if m.Focus == "input" || m.Screen == "new" {
			m.Input, cmd = m.Input.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}
func (m *Model) beginNew() {
	m.Screen = "new"
	m.Target = "job"
	m.Wizard = []string{"cwd", "kind", "account", "title", "goal", "prompt"}
	m.WizardIndex = 0
	m.New = supervisor.NewArgs{}
	cwd, _ := os.Getwd()
	m.focusInput(cwd)
	m.Notice = "새 작업 · 작업 폴더 (Enter 다음, Esc 취소)"
}
func (m *Model) wizardNext() tea.Cmd {
	value := strings.TrimSpace(m.Input.Value())
	field := m.Wizard[m.WizardIndex]
	switch field {
	case "cwd":
		m.New.Cwd = value
	case "kind":
		if value == "" {
			value = "chat"
		}
		if !jobs.ValidKind(value) {
			m.Notice = "종류: chat, task, goal, group"
			return nil
		}
		m.New.Kind = value
	case "account":
		m.New.Account = value
	case "title":
		m.New.Title = value
	case "goal":
		m.New.Goal = value
	case "prompt":
		m.New.Prompt = m.Input.Value()
	}
	m.WizardIndex++
	if m.WizardIndex == 4 && m.New.Kind != "goal" {
		m.WizardIndex++
	}
	if m.WizardIndex == 5 && m.New.Kind == "group" {
		m.WizardIndex++
	}
	if m.WizardIndex >= len(m.Wizard) {
		m.Loading = true
		m.Notice = "작업 등록 중…"
		return m.request("new", m.New)
	}
	next := m.Wizard[m.WizardIndex]
	defaults := map[string]string{"kind": "chat", "account": m.Config.WorkerAccounts.Priority[0], "title": filepath.Base(m.New.Cwd)}
	labels := map[string]string{"kind": "종류: chat / task / goal / group", "account": "최초 작업 계정", "title": "작업 제목", "goal": "목표", "prompt": "첫 지시 (여러 줄: Alt+Enter)"}
	m.Input.SetValue(defaults[next])
	m.Notice = "새 작업 · " + labels[next]
	return nil
}
func (m *Model) submit(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if strings.HasPrefix(text, "/") {
		return m.slash(text)
	}
	j, ok := m.selected()
	if m.Target == "manager" {
		m.Notice = "관리 지시 등록됨 · 모델 판단 대기"
		return m.request("manager", supervisor.JobArgs{ID: j.ID, Text: text})
	}
	if !ok {
		m.Notice = "입력을 보낼 작업을 선택하세요."
		return nil
	}
	if !j.Managed {
		m.Notice = "열람 중인 대화입니다. /resume-job으로 관리한 뒤 추가 지시를 보내세요."
		m.Input.SetValue(text)
		return nil
	}
	m.Notice = "선택한 작업에 지시를 큐로 보냈습니다."
	return m.request("send", supervisor.JobArgs{ID: j.ID, Text: text})
}
func (m *Model) slash(text string) tea.Cmd {
	parts := strings.SplitN(text, " ", 2)
	command := strings.TrimPrefix(parts[0], "/")
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(parts[1])
	}
	j, _ := m.selected()
	switch command {
	case "quit", "exit":
		return tea.Quit
	case "agents":
		m.Screen = "agents"
		m.Focus = "list"
		m.Input.Blur()
	case "new":
		m.beginNew()
	case "resume":
		m.Loading = true
		return m.request("threads", supervisor.JobArgs{Search: arg})
	case "manager":
		m.Target = "manager"
		m.Notice = "입력 대상: 관리자 · 모델 호출"
		if arg != "" {
			return m.request("manager", supervisor.JobArgs{ID: j.ID, Text: arg})
		}
	case "job":
		m.Target = "job"
		m.Notice = "입력 대상: 선택한 작업"
	case "account":
		if arg == "" {
			return m.request("accounts", nil)
		}
		return m.request("set-manager", supervisor.JobArgs{Account: arg})
	case "settings", "usage":
		return m.request(command, nil)
	case "older":
		if j.ID != "" && m.ReadCursor != "" {
			return m.request("read", supervisor.JobArgs{ID: j.ID, Cursor: m.ReadCursor})
		}
		m.Notice = "이전 대화 페이지가 없습니다."
	case "pause", "resume-job", "stop", "complete", "events", "attach", "read":
		if j.ID == "" {
			m.Notice = "작업을 선택하세요."
			return nil
		}
		return m.request(command, supervisor.JobArgs{ID: j.ID})
	case "steer":
		return m.request("send", supervisor.JobArgs{ID: j.ID, Text: arg, Steer: true})
	case "goal":
		return m.request("goal", supervisor.JobArgs{ID: j.ID, Text: arg})
	case "filter":
		m.Filter = arg
		m.Selected = 0
		m.SelectedID = ""
		m.Focus = "list"
		m.Input.Blur()
	case "state":
		m.StateFilter = arg
		m.Selected = 0
		m.SelectedID = ""
		m.Focus = "list"
		m.Input.Blur()
	case "answer":
		if len(j.PendingRequests) == 0 {
			m.Notice = "대기 중인 입력 요청이 없습니다."
			return nil
		}
		req := j.PendingRequests[0]
		result, e := answer(req, arg)
		if e != nil {
			m.Notice = e.Error()
			return nil
		}
		return m.request("reply", supervisor.JobArgs{ID: j.ID, RequestID: req.ID, Result: result})
	case "shutdown":
		return m.request("shutdown", nil)
	case "help":
		m.DetailText = "n 새 작업 · r 기존 대화 · m 관리자 지시 · i 입력 · Tab 영역 이동\np 중지 · s 재개 · a 공식 Codex 화면 · Enter 대화 열람 · Esc 목록\n/agents /new /resume 검색어 /manager 지시 /job /account [관리자계정]\n/pause /resume-job /stop /complete /events /usage /read /goal 목표\n/steer 지시: 실행 중인 턴에 개입\n/filter 검색어 /state 상태 /answer 응답 또는 JSON /settings\n/quit 화면 접속 종료 (작업 유지) /shutdown 작업과 감시 종료\nEnter 전송 · Alt+Enter 또는 Shift+Enter 줄바꿈 · 여러 줄 붙여넣기 지원"
		m.Focus = "detail"
	default:
		m.Notice = "지원하지 않는 명령입니다. /help"
	}
	return nil
}

func answer(req jobs.Request, text string) (json.RawMessage, error) {
	if json.Valid([]byte(text)) {
		return json.RawMessage(text), nil
	}
	switch req.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		if text != "accept" && text != "decline" && text != "cancel" && text != "acceptForSession" {
			return nil, fmt.Errorf("/answer accept 또는 decline")
		}
		return json.Marshal(map[string]string{"decision": text})
	case "item/tool/requestUserInput":
		var p struct {
			Questions []struct {
				ID string `json:"id"`
			} `json:"questions"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if len(p.Questions) != 1 {
			return nil, fmt.Errorf("여러 질문은 JSON으로 답하세요: {\"answers\":{\"질문ID\":{\"answers\":[\"답변\"]}}}")
		}
		return json.Marshal(map[string]any{"answers": map[string]any{p.Questions[0].ID: map[string]any{"answers": []string{text}}}})
	default:
		return nil, fmt.Errorf("이 요청은 /answer JSON 또는 원래 Codex 화면에서 처리하세요")
	}
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
		if m.Screen == "new" {
			b.WriteString("\n" + m.Notice + "\n\n" + m.Input.View() + "\n\nEnter 다음 · Alt+Enter 줄바꿈 · Esc 취소\n")
		} else if m.Screen == "resume" {
			b.WriteString("기존 대화 · Enter 가져오기(열람) · v 읽기 · /resume 검색어 · n 다음 페이지\n")
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
			listHeight := max(2, min(6, m.Height/4))
			start := max(0, m.Selected-listHeight+1)
			b.WriteString(dim.Render("작업 목록 · n 새 작업 · r 기존 대화 · /filter 검색 · /state 상태") + "\n")
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
				line := fmt.Sprintf("%s%s%s  %s · %s%s", prefix, indent, j.Title, j.State, j.Account, mode)
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
			height := max(2, m.Height-listHeight-10)
			vp := m.Detail
			vp.SetWidth(width)
			vp.SetHeight(height)
			vp.SetContent(ansi.Wrap(plain(text), width, ""))
			b.WriteString("\n" + vp.View() + "\n")
			target := "선택한 작업"
			if j, ok := m.selected(); ok {
				target = j.Title
			}
			if m.Target == "manager" {
				target = "관리자 " + m.Config.Manager.Account + " (모델 호출)"
			}
			b.WriteString(accent.Render("입력 대상: "+plain(target)) + "\n" + m.Input.View() + "\n")
			b.WriteString(dim.Render(ansi.Truncate("Tab 영역 이동 · Enter 전송 · Alt+Enter 줄바꿈 · Esc 목록 · /help · q 접속 종료", w, "…")))
		}
	}
	view := tea.NewView(b.String())
	view.AltScreen = true
	return view
}
func (m Model) content() string {
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
	text := fmt.Sprintf("%s · %s\n%s\n", j.Title, j.ID, j.Cwd)
	if j.Goal != nil {
		text += fmt.Sprintf("목표 %s · %s · %d tokens\n", j.Goal.Objective, j.Goal.Status, j.Goal.TokensUsed)
	}
	text += "\n최근 지시\n" + j.LastInput + "\n\n최근 응답\n" + j.LastOutput
	if j.LastError != "" {
		text += "\n\n상태: " + j.LastError
	}
	for _, r := range j.PendingRequests {
		text += "\n\n입력 대기 " + r.Method + "\n" + string(r.Params) + "\n/answer 응답 또는 JSON"
	}
	if j.Session != "" {
		text += "\n\ntmux attach -t " + j.Session
	}
	return text
}
func (m *Model) prepareDetail() {
	width := max(10, m.Width-2)
	shown := max(1, min(len(m.filtered()), max(2, min(6, m.Height/4))))
	height := max(2, m.Height-shown-10)
	m.Detail.SetWidth(width)
	m.Detail.SetHeight(height)
	m.Detail.SetContent(ansi.Wrap(plain(m.content()), width, ""))
	if m.Focus != "detail" {
		m.Detail.GotoBottom()
	}
}
