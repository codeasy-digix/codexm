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

// Direct conversation input uses the shared worker API. Management commands stay
// deterministic; native-only Codex commands remain available through /open.
var slashCommands = []slashCommand{
	{Name: "agents", Description: "전체 Codex 대화·관리 작업 목록"},
	{Name: "status", Description: "전체 작업·관리자·감시 상태"},
	{Name: "info", Description: "선택한 세션의 계정·모델·목표·토큰"},
	{Name: "usage", Description: "전체 계정 한도·관리자 호출·세션별 토큰"},
	{Name: "logs", Args: "[all]", Description: "선택 작업 로그 · all은 전체 세션"},
	{Name: "open", Description: "선택한 작업을 새 터미널의 공식 Codex로 열기"},
	{Name: "chat", Description: "선택한 대화를 이 화면의 채팅 모드로 열기"},
	{Name: "new", Description: "새 작업 생성 · 기본은 이 화면에서 채팅"},
	{Name: "chats", Args: "[검색어]", Description: "전체 대화 목록 검색"},
	{Name: "accounts", Description: "계정 추가·로그인·등록 삭제·기본 로그인 관리"},
	{Name: "account", Args: "[이름]", Description: "선택 세션의 계정 변경 · 이름 생략 시 선택 메뉴"},
	{Name: "manager-account", Args: "[이름]", Description: "저장된 관리자 계정 조회 또는 변경"},
	{Name: "pause", Description: "선택 작업 일시 중지"},
	{Name: "resume", Description: "선택 작업 실행 재개"},
	{Name: "stop", Description: "선택 작업 실행 종료"},
	{Name: "complete", Description: "선택 작업 완료 처리"},
	{Name: "manage", Description: "기존 대화를 관리 목록에 보관"},
	{Name: "unmanage", Description: "선택 대화의 관리 등록 해제 · 대화 보존"},
	{Name: "send", Description: "선택 작업에 추가 지시 작성 · 전송 전 확인"},
	{Name: "goal", Description: "선택 작업의 목표·예산 편집"},
	{Name: "requests", Description: "선택 작업의 질문·승인 요청에 응답"},
	{Name: "read", Description: "선택 작업의 대화 미리보기"},
	{Name: "older", Description: "이전 대화 미리보기 페이지"},
	{Name: "events", Description: "선택 작업의 사건 기록"},
	{Name: "filter", Args: "[검색어]", Description: "작업 목록 검색 · 빈 값은 해제"},
	{Name: "state", Args: "[상태]", Description: "작업 상태 필터 · 빈 값은 해제"},
	{Name: "filters", Description: "프로젝트·계정·상태·관리 여부 필터"},
	{Name: "log-filter", Description: "기록의 계정·기간·변경 사유 필터"},
	{Name: "manager", Args: "[지시]", Description: "관리자 모델에게 관리 지시 (모델 호출)"},
	{Name: "settings", Description: "계정 정책·저장 위치·감시 설정"},
	{Name: "resolve-action", Description: "대화에서 결과를 확인한 작업 복구"},
	{Name: "mouse", Description: "마우스 클릭·휠 사용 전환 · Ctrl+G"},
	{Name: "actions", Description: "선택 작업의 모든 관리 동작 메뉴 · F9"},
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
	b.WriteString("대화를 선택하면 이 화면에서 채팅합니다. 관리 화면은 F1–F6, 공식 Codex 전용 명령은 /open으로 연 별도 터미널에서 사용합니다.\n\n/ 을 입력하면 관리 명령 목록이 열립니다.\n↑↓ 선택 · Tab 완성 · Enter 실행 · Esc 닫기\n\n")
	for _, c := range slashCommands {
		b.WriteString("/" + c.Name + " " + c.Args + " · " + c.Description + "\n")
	}
	b.WriteString("\n기본 포커스는 입력창 · 목록의 일반 텍스트 + Enter: 검색 (모델 호출 없음)\n목록 클릭/Enter 또는 /chat: 해당 대화의 채팅 모드 · 채팅 Enter: 지시 전송\n채팅 빈 입력 Enter: 동작 없음 · Shift/Alt+Enter: 줄바꿈 · Esc 또는 /agents: 목록\n입력 ↑↓/Ctrl+P,N: 입력 기록 · Alt+↑↓: 대화 전환 (초안은 대화별 보존)\nF1 작업 · F2 계정 · F3 사용량 · F4 기록 · F5 설정 · F6 관리자 · F7 새 작업 · F8 필터 · F9 작업 동작\nTab 영역 이동 · Shift+Tab 반대 이동 · PgUp/PgDn 대화 스크롤\n마우스: 탭·목록·버튼 클릭, 휠 스크롤 · Ctrl+G 또는 /mouse: 보고 해제 후 터미널 기본 선택/복사\n폼: Tab 이동 · Enter 편집/선택 · ←→ 옵션 변경 · Ctrl+S 저장 · Esc 취소\n목록: ↑↓ 선택 · Enter 채팅 모드 · v 미리보기 · l 전체 로그\n화면을 닫아도 작업 감시는 계속됩니다. /quit는 화면만 닫습니다.\n")
	return b.String()
}
