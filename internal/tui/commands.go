package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type slashCommand struct {
	Name, Args, Description string
	NeedsArg                bool
}

// codexm owns management commands. Chat commands belong to the official TUI
// opened in its own terminal, so no native command emulation is necessary.
var slashCommands = []slashCommand{
	{Name: "agents", Description: "전체 Codex 대화·관리 작업 목록"},
	{Name: "status", Description: "전체 작업·관리자·감시 상태"},
	{Name: "info", Description: "선택한 세션의 계정·모델·목표·토큰"},
	{Name: "usage", Description: "전체 계정 한도·관리자 호출·세션별 토큰"},
	{Name: "logs", Args: "[all]", Description: "선택 작업 로그 · all은 전체 세션"},
	{Name: "open", Description: "선택한 작업을 새 터미널의 공식 Codex로 열기"},
	{Name: "new", Description: "새 채팅을 만들고 새 터미널에서 열기"},
	{Name: "chats", Args: "[검색어]", Description: "전체 대화 목록 검색"},
	{Name: "accounts", Description: "모든 계정 한도를 새로 조회 (모델 호출 없음)"},
	{Name: "account", Args: "[이름]", Description: "선택 세션의 계정 변경 · 이름 생략 시 선택 메뉴"},
	{Name: "manager-account", Args: "[이름]", Description: "저장된 관리자 계정 조회 또는 변경"},
	{Name: "pause", Description: "선택 작업 일시 중지"},
	{Name: "resume", Description: "선택 작업 실행 재개"},
	{Name: "stop", Description: "선택 작업 실행 종료"},
	{Name: "complete", Description: "선택 작업 완료 처리"},
	{Name: "manage", Description: "기존 대화를 관리 목록에 보관"},
	{Name: "read", Description: "선택 작업의 대화 미리보기"},
	{Name: "older", Description: "이전 대화 미리보기 페이지"},
	{Name: "events", Description: "선택 작업의 사건 기록"},
	{Name: "filter", Args: "[검색어]", Description: "작업 목록 검색 · 빈 값은 해제"},
	{Name: "state", Args: "[상태]", Description: "작업 상태 필터 · 빈 값은 해제"},
	{Name: "manager", Args: "[지시]", Description: "관리자 모델에게 관리 지시 (모델 호출)"},
	{Name: "settings", Description: "계정 정책·저장 위치·감시 설정"},
	{Name: "shutdown", Description: "관리 작업과 감시 서비스 종료"},
	{Name: "help", Description: "관리 명령과 키보드 도움말"},
	{Name: "quit", Description: "관리 화면 접속 종료 (작업 유지)"},
}

func (m Model) commandMatches() []slashCommand {
	text := m.Input.Value()
	if m.Focus != "input" || m.Screen == "setup" || m.Screen == "new" || m.PaletteDismissed || !strings.HasPrefix(text, "/") || strings.ContainsAny(text, " \t\n") {
		return nil
	}
	query := strings.ToLower(strings.TrimPrefix(text, "/"))
	result := []slashCommand{}
	for _, c := range slashCommands {
		if strings.Contains(c.Name, query) {
			result = append(result, c)
		}
	}
	return result
}

func (m Model) paletteHeight() int {
	if len(m.commandMatches()) == 0 {
		return 0
	}
	return min(len(m.commandMatches()), max(1, min(7, m.Height/4))) + 2
}

func (m Model) paletteView() string {
	cs := m.commandMatches()
	if len(cs) == 0 {
		return ""
	}
	height := m.paletteHeight() - 2
	selected := min(m.PaletteIndex, len(cs)-1)
	start := max(0, selected-height+1)
	var b strings.Builder
	b.WriteString(dim.Render(ansi.Truncate(fmt.Sprintf("명령 목록 · %d/%d · ↑↓ 선택 · Tab 완성 · Enter 실행 · Esc 닫기", selected+1, len(cs)), m.Width, "…")) + "\n")
	for i := start; i < len(cs) && i < start+height; i++ {
		prefix := "  "
		if i == selected {
			prefix = "› "
		}
		line := ansi.Truncate(prefix+"/"+cs[i].Name+" "+cs[i].Args+" · "+cs[i].Description, m.Width, "…")
		if i == selected {
			line = accent.Render(line)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func (m *Model) chooseCommand(execute bool) tea.Cmd {
	cs := m.commandMatches()
	if len(cs) == 0 {
		return nil
	}
	c := cs[min(m.PaletteIndex, len(cs)-1)]
	text := "/" + c.Name
	if !execute || c.NeedsArg {
		if c.Args != "" {
			text += " "
		}
		m.Input.SetValue(text)
		m.Input.CursorEnd()
		m.PaletteDismissed = true
		m.Notice = c.Description
		return nil
	}
	m.Input.Reset()
	m.rememberInput(text)
	return m.slash(text)
}

func commandHelp() string {
	var b strings.Builder
	b.WriteString("codexm은 작업 관리자입니다. 대화와 Codex 명령은 /open으로 연 공식 화면에서 사용합니다.\n\n/ 을 입력하면 관리 명령 목록이 열립니다.\n↑↓ 선택 · Tab 완성 · Enter 실행 · Esc 닫기\n\n")
	for _, c := range slashCommands {
		b.WriteString("/" + c.Name + " " + c.Args + " · " + c.Description + "\n")
	}
	b.WriteString("\n입력창 기본 포커스 · 일반 텍스트 + Enter: 작업 검색 (모델 호출 없음)\n빈 입력 + Enter: 선택 대화 열기 · 입력 ↑↓/Ctrl+P,N: 입력 기록 · Alt+↑↓: 대화 선택\nTab 영역 이동 · Shift+Tab 반대 이동 · PgUp/PgDn 미리보기 스크롤\n목록: ↑↓ 선택 · Enter 새 터미널 열기 · v 미리보기 · l 전체 로그\n새 터미널을 닫아도 작업 감시는 계속됩니다. /quit는 관리 화면만 닫습니다.\n")
	return b.String()
}
