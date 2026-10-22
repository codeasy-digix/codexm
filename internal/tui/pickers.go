package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/codeasy-digix/codexm/internal/accounts"
)

func (m Model) pickerView(width, height int) (string, []hitRegion) {
	c := dashboardCanvas{width: width}
	switch m.Screen {
	case "account":
		c.line(plain(m.AccountTitle) + " · 현재 " + plain(m.AccountFrom))
		c.line("이 대화의 계정 변경 · 선택 후 변경 버튼")
		capacity := max(1, height-5)
		start := max(0, m.AccountSelected-capacity+1)
		for i := start; i < len(m.AccountNames) && i < start+capacity; i++ {
			name := m.AccountNames[i]
			prefix := "  "
			if i == m.AccountSelected {
				prefix = "› "
			}
			detail := "한도 미관측"
			for _, snapshot := range m.Status.Accounts {
				if snapshot.Account == name {
					detail = accounts.Describe(snapshot)
				}
			}
			c.hits = append(c.hits, hitRegion{0, len(c.lines), width, 1, "picker:account:" + strconv.Itoa(i)})
			c.line(prefix + plain(name+" · "+detail))
		}
		c.buttons([]uiButton{{"선택 계정으로 변경", "picker:enter"}, {"취소", "picker:cancel"}})
		c.line("전환 전 한도·실행 상태를 다시 확인합니다.")
	case "import-account":
		c.line("기존 대화를 열 계정 · 비우면 우선순위 자동 선택")
		if len(m.Threads) > m.ThreadSelected {
			c.line(plain(m.Threads[m.ThreadSelected].Name))
		}
		input := m.Input
		input.SetWidth(max(10, width-2))
		input.SetHeight(2)
		for _, line := range strings.Split(input.View(), "\n") {
			c.hits = append(c.hits, hitRegion{0, len(c.lines), width, 1, "area:input"})
			c.line(line)
		}
		c.buttons([]uiButton{{"가져와서 터미널 열기", "picker:enter"}, {"취소", "picker:cancel"}})
	case "resume":
		c.line("기존 Codex 대화 · 선택 후 열기")
		capacity := max(1, height-4)
		start := max(0, m.ThreadSelected-capacity+1)
		for i := start; i < len(m.Threads) && i < start+capacity; i++ {
			prefix := "  "
			if i == m.ThreadSelected {
				prefix = "› "
			}
			c.hits = append(c.hits, hitRegion{0, len(c.lines), width, 1, "picker:thread:" + strconv.Itoa(i)})
			c.line(prefix + plain(m.Threads[i].Name+" "+m.Threads[i].ID))
		}
		c.buttons([]uiButton{{"터미널 열기", "picker:enter"}, {"다음 페이지", "picker:next"}, {"취소", "picker:cancel"}})
	}
	c.hits = append(c.hits, hitRegion{0, 0, width, height, "picker:area"})
	return strings.Join(c.lines, "\n"), c.hits
}

func (m *Model) pickerAction(action string) tea.Cmd {
	switch {
	case action == "picker:cancel":
		next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
		*m = next.(Model)
		return cmd
	case action == "picker:enter":
		next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		*m = next.(Model)
		return cmd
	case action == "picker:next":
		next, cmd := m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
		*m = next.(Model)
		return cmd
	case strings.HasPrefix(action, "picker:account:"):
		i, err := strconv.Atoi(strings.TrimPrefix(action, "picker:account:"))
		if err == nil && i >= 0 && i < len(m.AccountNames) {
			m.AccountSelected = i
		}
	case strings.HasPrefix(action, "picker:thread:"):
		i, err := strconv.Atoi(strings.TrimPrefix(action, "picker:thread:"))
		if err == nil && i >= 0 && i < len(m.Threads) {
			m.ThreadSelected = i
		}
	}
	return nil
}
