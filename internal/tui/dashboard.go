package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/codeasy-digix/codexm/internal/display"
	"github.com/codeasy-digix/codexm/internal/jobs"
)

// Both rendering and mouse dispatch use these cell coordinates. Coordinates
// include wrapped controls and wide Korean characters, never byte offsets.
type hitRegion struct {
	X, Y, W, H int
	Action     string
}
type uiButton struct{ Label, Action string }
type dashboardCanvas struct {
	width int
	lines []string
	hits  []hitRegion
}

// Clamp scrolling on the live form using the rendered pane dimensions.
func (m *Model) preparePanelForms() {
	if m.TaskForm.Kind == "" && !m.managementVisible() {
		return
	}
	_, hits := m.dashboard()
	for _, hit := range hits {
		if hit.Action == "task:area" {
			m.TaskForm.Form.render(hit.W, hit.H, "task:field:")
			return
		}
		if hit.Action == "mgmt:area" {
			m.managementView(hit.W, hit.H)
			return
		}
	}
}

func (c *dashboardCanvas) line(s string) {
	c.lines = append(c.lines, ansi.Truncate(s, c.width, "…"))
}
func (c *dashboardCanvas) buttons(bs []uiButton) {
	var line string
	x := 0
	for _, b := range bs {
		label := "[" + b.Label + "]"
		label = ansi.Truncate(label, c.width, "…")
		w := ansi.StringWidth(label)
		if x > 0 && x+1+w > c.width {
			c.line(line)
			line, x = "", 0
		}
		if x > 0 {
			line += " "
			x++
		}
		c.hits = append(c.hits, hitRegion{x, len(c.lines), w, 1, b.Action})
		line += label
		x += w
	}
	if line != "" {
		c.line(line)
	}
}
func fixedLines(text string, width, height int) []string {
	lines := strings.Split(text, "\n")
	out := make([]string, max(0, height))
	for i := range out {
		if i < len(lines) {
			out[i] = ansi.Truncate(lines[i], max(1, width), "…")
		}
	}
	return out
}

var dashboardTabs = []uiButton{
	{"작업 F1", "tab:tasks"}, {"계정 F2", "tab:accounts"},
	{"사용량 F3", "tab:usage"}, {"기록 F4", "tab:logs"},
	{"설정 F5", "tab:settings"}, {"관리자 F6", "tab:manager"},
}

func (m Model) View() tea.View {
	if m.Screen == "new" && !m.managementVisible() && m.TaskForm.Kind == "" {
		// Legacy thread/account pickers remain keyboard-accessible.
		v := m.legacyView()
		if !m.MouseDisabled {
			v.MouseMode = tea.MouseModeCellMotion
		}
		return v
	}
	text, _ := m.dashboard()
	v := tea.NewView(text)
	v.AltScreen = true
	if !m.MouseDisabled {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

func (m Model) dashboard() (string, []hitRegion) {
	w, h := max(20, m.Width), max(10, m.Height)
	c := dashboardCanvas{width: w}
	manager := m.Config.Manager.Account
	if manager == "" {
		manager = "미설정"
	}
	running := 0
	for _, j := range m.Status.Jobs {
		if jobs.Busy(j.State) {
			running++
		}
	}
	c.line(accent.Render(fmt.Sprintf("codexm · 관리자 %s · 실행 %d/%d", plain(manager), running, m.Config.Execution.MaxParallelJobs)))
	c.line(dim.Render(plain(m.Notice)))
	tabs := append([]uiButton(nil), dashboardTabs...)
	for i := range tabs {
		if strings.TrimPrefix(tabs[i].Action, "tab:") == m.Tab {
			tabs[i].Label = "● " + tabs[i].Label
		}
	}
	c.buttons(tabs)
	picker := m.Screen == "account" || m.Screen == "import-account" || m.Screen == "resume"
	modal := m.managementVisible() || m.TaskForm.Kind != "" || m.Screen == "setup" || picker
	footer := []string{dim.Render(ansi.Truncate("Tab 이동 · Enter 선택 · Esc 취소 · Ctrl+S 저장 · Ctrl+G 마우스/복사 · Ctrl+C 종료", w, "…"))}
	if !modal {
		footer = strings.Split(strings.TrimSuffix(m.paletteView(), "\n"), "\n")
		if len(m.commandMatches()) == 0 {
			footer = nil
		}
		target := "작업 검색 (모델 호출 없음)"
		if m.Target == "manager" {
			target = "관리자 " + m.Config.Manager.Account + " (모델 호출)"
			if m.ManagerAll {
				target += " · 전체 작업"
			} else {
				target += " · 선택 작업"
			}
		}
		footer = append(footer, accent.Render(ansi.Truncate("입력 대상: "+target, w, "…")))
		input := m.Input
		input.SetWidth(max(10, w-2))
		input.SetHeight(2)
		footer = append(footer, strings.Split(input.View(), "\n")...)
		footer = append(footer, dim.Render(ansi.Truncate("/ 명령 · ↑↓ 입력 기록 · Alt+↑↓ 작업 · Tab 영역 · PgUp/PgDn · Ctrl+G 마우스/복사", w, "…")))
	}
	if !modal {
		c.buttons(m.panelButtons())
	}
	bodyY := len(c.lines)
	bodyH := max(1, h-bodyY-len(footer))
	if m.managementVisible() {
		body, hits := m.managementView(w, bodyH)
		c.lines = append(c.lines, fixedLines(body, w, bodyH)...)
		for _, hit := range hits {
			hit.Y += bodyY
			if hit.Y < bodyY+bodyH {
				c.hits = append(c.hits, hit)
			}
		}
	} else if m.TaskForm.Kind != "" {
		body, hits := m.taskFormView(w, bodyH)
		c.lines = append(c.lines, fixedLines(body, w, bodyH)...)
		for _, hit := range hits {
			hit.Y += bodyY
			if hit.Y < bodyY+bodyH {
				c.hits = append(c.hits, hit)
			}
		}
		c.hits = append(c.hits, hitRegion{0, bodyY, w, bodyH, "task:area"})
	} else if picker {
		body, hits := m.pickerView(w, bodyH)
		c.lines = append(c.lines, fixedLines(body, w, bodyH)...)
		for _, hit := range hits {
			hit.Y += bodyY
			c.hits = append(c.hits, hit)
		}
	} else if m.Screen == "setup" {
		c.line("최초 설정 · 관리자 계정 선택")
		c.buttons([]uiButton{{"계정 추가·로그인", "mgmt:accounts"}})
		for i, name := range m.Names {
			prefix := "  "
			if i == m.Selected {
				prefix = "› "
			}
			if len(c.lines) >= h-3 {
				break
			}
			c.hits = append(c.hits, hitRegion{0, len(c.lines), w, 1, "setup:" + strconv.Itoa(i)})
			c.line(prefix + plain(name))
		}
		c.buttons([]uiButton{{"선택 계정으로 시작", "setup:start"}})
		if len(m.Names) == 0 {
			c.line("저장된 계정이 없습니다. 위 로그인 메뉴를 사용하세요.")
		}
		for len(c.lines) < h-len(footer) {
			c.line("")
		}
	} else if m.Tab == "tasks" || m.Tab == "" {
		if w >= 96 && bodyH >= 8 {
			leftW := min(40, max(28, w/3))
			left, leftHits := m.sessionLines(leftW, bodyH)
			right := m.detailLines(w-leftW-3, bodyH)
			for i := 0; i < bodyH; i++ {
				pad := max(0, leftW-ansi.StringWidth(left[i]))
				c.line(left[i] + strings.Repeat(" ", pad) + " │ " + right[i])
			}
			for _, hit := range leftHits {
				hit.Y += bodyY
				c.hits = append(c.hits, hit)
			}
			c.hits = append(c.hits, hitRegion{leftW + 3, bodyY, w - leftW - 3, bodyH, "area:detail"})
		} else {
			listH := max(2, min(6, bodyH/3))
			if bodyH < 4 {
				listH = 1
			}
			left, hits := m.sessionLines(w, listH)
			c.lines = append(c.lines, left...)
			for _, hit := range hits {
				hit.Y += bodyY
				c.hits = append(c.hits, hit)
			}
			detailY := len(c.lines)
			detailH := max(0, bodyH-listH)
			c.lines = append(c.lines, m.detailLines(w, detailH)...)
			c.hits = append(c.hits, hitRegion{0, detailY, w, detailH, "area:detail"})
		}
	} else {
		c.lines = append(c.lines, m.detailLines(w, bodyH)...)
		c.hits = append(c.hits, hitRegion{0, bodyY, w, bodyH, "area:detail"})
	}
	for i, line := range footer {
		if !modal {
			paletteRows := 0
			if len(m.commandMatches()) > 0 {
				paletteRows = len(strings.Split(strings.TrimSuffix(m.paletteView(), "\n"), "\n"))
			}
			if i > 0 && i < paletteRows {
				cs := m.commandMatches()
				n := max(0, min(m.PaletteIndex, len(cs)-1)-(m.paletteHeight()-2)+1) + i - 1
				if n < len(cs) {
					c.hits = append(c.hits, hitRegion{0, len(c.lines), w, 1, "palette:" + strconv.Itoa(n)})
				}
			} else if i >= paletteRows+1 && i < len(footer)-1 {
				c.hits = append(c.hits, hitRegion{0, len(c.lines), w, 1, "area:input"})
			}
		}
		c.line(line)
	}
	return strings.Join(fixedLines(strings.Join(c.lines, "\n"), w, h), "\n"), c.hits
}

func (m Model) panelButtons() []uiButton {
	switch m.Tab {
	case "logs":
		return []uiButton{{"전체 작업", "logs:all"}, {"선택 작업", "logs:selected"}, {"필터", "command:log-filter"}, {"새로고침", "logs:refresh"}}
	case "usage":
		return []uiButton{{"한도 새로 조회", "usage:refresh"}, {"전체 상태", "command:status"}}
	case "manager":
		return []uiButton{{"전체 작업에 관리 지시", "manager:all"}, {"선택 작업에 관리 지시", "manager:selected"}, {"전체 상태", "command:status"}, {"복구 확인", "command:resolve-action"}}
	}
	if m.Width < 72 || m.Height < 26 {
		return []uiButton{{"새 작업 F7", "command:new"}, {"작업 동작 F9", "command:actions"}, {"필터 F8", "command:filters"}}
	}
	return m.taskButtons()
}

func (m Model) taskButtons() []uiButton {
	bs := []uiButton{{"새 작업 F7", "command:new"}, {"검색·필터 F8", "command:filters"}}
	j, ok := m.selected()
	if !ok {
		return bs
	}
	bs = append(bs, uiButton{"미리보기", "command:read"}, uiButton{"터미널 열기", "command:open"})
	if !m.registered(j) || !j.Managed {
		return append(bs, uiButton{"관리 등록", "command:manage"})
	}
	if j.Kind != "group" {
		bs = append(bs, uiButton{"지시 보내기", "command:send"}, uiButton{"계정 변경", "command:account"}, uiButton{"목표", "command:goal"})
	}
	if j.Paused {
		bs = append(bs, uiButton{"재개", "command:resume"})
	} else {
		bs = append(bs, uiButton{"일시 중지", "command:pause"})
	}
	bs = append(bs, uiButton{"중지", "command:stop"}, uiButton{"완료", "command:complete"}, uiButton{"관리 해제", "command:unmanage"})
	if len(j.PendingRequests) > 0 {
		bs = append(bs, uiButton{fmt.Sprintf("응답 요청 %d", len(j.PendingRequests)), "command:requests"})
	}
	return bs
}

func (m Model) sessionLines(width, height int) ([]string, []hitRegion) {
	js := m.filtered()
	lines := make([]string, max(0, height))
	var hits []hitRegion
	if height == 0 {
		return lines, hits
	}
	lines[0] = dim.Render(ansi.Truncate(fmt.Sprintf("전체 Codex 대화 %d · 관리 작업 %d", len(m.allJobs()), len(m.Status.Jobs)), width, "…"))
	capacity := max(1, height-1)
	start := max(0, min(m.ListOffset, max(0, len(js)-capacity)))
	if m.Selected < start {
		start = max(0, m.Selected)
	}
	if m.Selected >= start+capacity {
		start = m.Selected - capacity + 1
	}
	for row := 1; row < height && start+row-1 < len(js); row++ {
		i := start + row - 1
		j := js[i]
		prefix := "  "
		if j.ID == m.SelectedID {
			prefix = "› "
		}
		star := ""
		if m.Stars[j.ID] {
			star = "★ "
		}
		account := j.Account
		if account == "" {
			account = "계정 미배정"
		}
		title := j.Title
		if title == "" {
			title = j.ThreadID
		}
		line := prefix + star + plain(title) + " · " + display.State(j.State) + " · " + plain(account)
		line = ansi.Truncate(line, width, "…")
		if j.ID == m.SelectedID {
			line = accent.Render(line)
		}
		lines[row] = line
		hits = append(hits, hitRegion{0, row, width, 1, "session:" + strconv.Itoa(i)})
	}
	if len(js) == 0 && height > 1 {
		lines[1] = ansi.Truncate("표시할 대화 없음 · /filter 해제 · 새 작업", width, "…")
	}
	hits = append(hits, hitRegion{0, 0, width, height, "area:list"})
	return lines, hits
}

func (m Model) detailLines(width, height int) []string {
	if height < 1 {
		return nil
	}
	vp := m.Detail
	vp.SetWidth(max(1, width))
	vp.SetHeight(height)
	text := m.content()
	if m.Tab == "logs" {
		text = m.logsText()
	}
	if strings.TrimSpace(text) == "" {
		text = "대화를 선택하거나 새 작업을 만드세요.\n목록을 클릭하면 선택만 바뀝니다. 실행은 버튼으로 요청하세요."
	}
	vp.SetContent(ansi.Wrap(plain(text), max(1, width), ""))
	return fixedLines(vp.View(), width, height)
}

func (m *Model) activateTab(tab string) tea.Cmd {
	if m.TaskForm.Kind != "" {
		m.Notice = "현재 폼을 저장하거나 Esc로 닫은 후 메뉴를 바꾸세요."
		return nil
	}
	m.ViewEpoch++
	m.Tab = tab
	m.Target = "search"
	if m.Screen != "setup" {
		m.Screen = "agents"
	}
	m.Panel = ""
	m.LogsOpen = false
	m.DetailText = ""
	m.Detail.GotoTop()
	if tab != "accounts" && tab != "settings" {
		m.Management.Mode = ""
	}
	switch tab {
	case "accounts":
		return m.openAccountManagement()
	case "settings":
		return m.openSettingsEditor()
	case "usage":
		m.Panel = "usage"
		return tea.Batch(m.poll(), m.request("usage", nil))
	case "logs":
		return m.slash("/logs all")
	case "manager":
		m.Target = "manager"
		m.returnInput()
		m.Panel = "manager-status"
	default:
		m.Tab = "tasks"
		m.Target = "search"
		m.returnInput()
	}
	return nil
}

func (m *Model) dashboardAction(action string) tea.Cmd {
	switch {
	case strings.HasPrefix(action, "mgmt:"):
		return m.managementAction(action)
	case strings.HasPrefix(action, "task:"):
		return m.taskFormAction(action)
	case strings.HasPrefix(action, "tab:"):
		return m.activateTab(strings.TrimPrefix(action, "tab:"))
	case strings.HasPrefix(action, "command:"):
		cmd := strings.TrimPrefix(action, "command:")
		if cmd == "new" {
			m.Management.Mode = ""
			m.beginNew()
			return nil
		}
		return m.slash("/" + cmd)
	case strings.HasPrefix(action, "session:"):
		i, err := strconv.Atoi(strings.TrimPrefix(action, "session:"))
		if err != nil || i < 0 || i >= len(m.filtered()) {
			return nil
		}
		m.selectIndex(i - m.Selected)
		m.Focus = "list"
		m.Input.Blur()
	case strings.HasPrefix(action, "palette:"):
		i, err := strconv.Atoi(strings.TrimPrefix(action, "palette:"))
		if err == nil {
			m.PaletteIndex = i
			return m.chooseCommand(true)
		}
	case action == "area:input":
		m.returnInput()
	case action == "area:detail":
		m.Focus = "detail"
		m.Input.Blur()
	case action == "area:list":
		m.Focus = "list"
		m.Input.Blur()
	case action == "logs:all":
		return m.slash("/logs all")
	case action == "logs:selected":
		return m.slash("/logs")
	case action == "logs:refresh":
		return m.request("logs", map[string]any{"id": m.LogsJob, "limit": 500})
	case action == "usage:refresh":
		m.Panel = "usage"
		return tea.Batch(m.request("accounts", nil), m.request("usage", nil))
	case action == "manager:all" || action == "manager:selected":
		m.ManagerAll = action == "manager:all"
		m.Target = "manager"
		m.Panel = ""
		m.DetailText = ""
		m.returnInput()
		m.Notice = "관리자에게 지시할 범위를 선택했습니다. 전송 시 모델을 호출합니다."
	case action == "setup:start":
		if len(m.Names) > 0 && !m.Loading {
			m.Loading = true
			return m.start(true)
		}
	case strings.HasPrefix(action, "setup:"):
		i, err := strconv.Atoi(strings.TrimPrefix(action, "setup:"))
		if err == nil && i >= 0 && i < len(m.Names) {
			m.Selected = i
		}
	case strings.HasPrefix(action, "picker:"):
		return m.pickerAction(action)
	}
	return nil
}

func (m *Model) dashboardUpdate(msg tea.Msg) (tea.Cmd, bool) {
	if key, ok := msg.(tea.KeyPressMsg); ok && m.Screen != "new" {
		for i, tab := range dashboardTabs {
			if key.String() == fmt.Sprintf("f%d", i+1) {
				return m.activateTab(strings.TrimPrefix(tab.Action, "tab:")), true
			}
		}
		switch key.String() {
		case "f7":
			m.beginNew()
			return nil, true
		case "f8":
			m.beginTaskForm("filters")
			return nil, true
		case "f9":
			m.beginTaskForm("actions")
			return nil, true
		}
	}
	if m.MouseDisabled {
		return nil, false
	}
	var mouse tea.Mouse
	switch v := msg.(type) {
	case tea.MouseClickMsg:
		if v.Button != tea.MouseLeft {
			return nil, true
		}
		mouse = v.Mouse()
	case tea.MouseWheelMsg:
		mouse = v.Mouse()
	default:
		return nil, false
	}
	_, hits := m.dashboard()
	// First hit wins: a specific row/button precedes its containing region.
	for _, hit := range hits {
		if mouse.X < hit.X || mouse.X >= hit.X+hit.W || mouse.Y < hit.Y || mouse.Y >= hit.Y+hit.H {
			continue
		}
		if wheel, ok := msg.(tea.MouseWheelMsg); ok {
			delta := 1
			if wheel.Button == tea.MouseWheelUp {
				delta = -1
			}
			if strings.HasPrefix(hit.Action, "session:") || hit.Action == "area:list" {
				m.selectIndex(delta * 3)
				return nil, true
			}
			if strings.HasPrefix(hit.Action, "mgmt:") {
				return m.managementAction(fmt.Sprintf("mgmt:wheel:%d", delta)), true
			}
			if strings.HasPrefix(hit.Action, "picker:") {
				if m.Screen == "account" {
					m.AccountSelected = max(0, min(len(m.AccountNames)-1, m.AccountSelected+delta))
				} else if m.Screen == "resume" {
					m.ThreadSelected = max(0, min(len(m.Threads)-1, m.ThreadSelected+delta))
				}
				return nil, true
			}
			if m.TaskForm.Kind != "" {
				return m.taskFormWheel(delta), true
			}
			m.prepareDetail()
			m.AutoFollow = false
			if delta < 0 {
				m.Detail.ScrollUp(3)
			} else {
				m.Detail.ScrollDown(3)
			}
			return nil, true
		}
		return m.dashboardAction(hit.Action), true
	}
	return nil, true
}
