package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type slashCommand struct {
	Name, Args, Description string
	Native                  bool
	NeedsArg                bool
}

// One registry drives completion and help. Manager-only commands always have
// their own namespace, so /stop can never accidentally stop a managed job.
var slashCommands = []slashCommand{
	{Name: "status", Description: "선택한 세션의 모델·권한·토큰·계정 한도"},
	{Name: "usage", Description: "선택한 세션 계정의 잔여 한도·갱신 시각"},
	{Name: "new", Description: "새 Codex 채팅 만들기"},
	{Name: "resume", Args: "[검색어]", Description: "저장된 Codex 대화 찾기"},
	{Name: "goal", Args: "[목표]", Description: "선택한 목표 보기 또는 설정"},
	{Name: "copy", Description: "선택한 세션의 최근 완료 응답 복사"},
	{Name: "model", Description: "모델·추론 수준 선택 · 공식 Codex 화면", Native: true},
	{Name: "permissions", Description: "권한 변경 · 공식 Codex 화면", Native: true},
	{Name: "plan", Args: "[지시]", Description: "계획 모드 · 공식 Codex 화면", Native: true},
	{Name: "review", Description: "변경사항 리뷰 · 공식 Codex 화면", Native: true},
	{Name: "diff", Description: "Git 변경사항 · 공식 Codex 화면", Native: true},
	{Name: "compact", Description: "대화 압축 · 공식 Codex 화면", Native: true},
	{Name: "fork", Description: "대화 분기 · 공식 Codex 화면", Native: true},
	{Name: "rename", Args: "[이름]", Description: "대화 이름 변경 · 공식 Codex 화면", Native: true},
	{Name: "mcp", Description: "MCP 도구 목록 · 공식 Codex 화면", Native: true},
	{Name: "skills", Description: "스킬 목록 · 공식 Codex 화면", Native: true},
	{Name: "plugins", Description: "플러그인 관리 · 공식 Codex 화면", Native: true},
	{Name: "apps", Description: "연결 앱 목록 · 공식 Codex 화면", Native: true},
	{Name: "agent", Description: "Codex 내부 하위 에이전트 · 공식 화면", Native: true},
	{Name: "subagents", Description: "Codex 내부 하위 에이전트 · 공식 화면", Native: true},
	{Name: "ps", Description: "백그라운드 터미널 · 공식 Codex 화면", Native: true},
	{Name: "stop", Description: "백그라운드 터미널 중지 · 공식 Codex 화면", Native: true},
	{Name: "clear", Description: "새 채팅으로 초기화 · 공식 Codex 화면", Native: true},
	{Name: "fast", Description: "Fast 서비스 설정 · 공식 Codex 화면", Native: true},
	{Name: "personality", Description: "응답 스타일 · 공식 Codex 화면", Native: true},
	{Name: "hooks", Description: "수명주기 훅 · 공식 Codex 화면", Native: true},
	{Name: "memories", Description: "메모리 설정 · 공식 Codex 화면", Native: true},
	{Name: "ide", Description: "IDE 문맥 · 공식 Codex 화면", Native: true},
	{Name: "mention", Description: "파일 첨부 · 공식 Codex 화면", Native: true},
	{Name: "init", Description: "AGENTS.md 생성 · 공식 Codex 화면", Native: true},
	{Name: "side", Description: "임시 보조 채팅 · 공식 Codex 화면", Native: true},
	{Name: "btw", Description: "임시 보조 채팅 · 공식 Codex 화면", Native: true},
	{Name: "raw", Description: "원문 스크롤 모드 · 공식 Codex 화면", Native: true},
	{Name: "keymap", Description: "Codex 단축키 설정 · 공식 화면", Native: true},
	{Name: "vim", Description: "Codex Vim 입력 모드 · 공식 화면", Native: true},
	{Name: "theme", Description: "Codex 문법 강조 테마 · 공식 화면", Native: true},
	{Name: "statusline", Description: "Codex 상태줄 설정 · 공식 화면", Native: true},
	{Name: "title", Description: "Codex 터미널 제목 설정 · 공식 화면", Native: true},
	{Name: "pets", Description: "Codex 펫 · 공식 화면", Native: true},
	{Name: "pet", Description: "Codex 펫 · 공식 화면", Native: true},
	{Name: "experimental", Description: "Codex 실험 기능 · 공식 화면", Native: true},
	{Name: "debug-config", Description: "Codex 설정 진단 · 공식 화면", Native: true},
	{Name: "archive", Description: "대화 보관 · 공식 Codex 화면", Native: true},
	{Name: "delete", Description: "대화 영구 삭제 · 공식 Codex 화면", Native: true},
	{Name: "logout", Description: "로그아웃 · 공식 Codex 화면", Native: true},
	{Name: "approve", Description: "자동 검토 거절 재시도 · 공식 화면", Native: true},
	{Name: "feedback", Description: "Codex 피드백 · 공식 화면", Native: true},
	{Name: "import", Description: "외부 설정·대화 가져오기 · 공식 화면", Native: true},
	{Name: "app", Description: "데스크톱 앱으로 계속 · 공식 화면", Native: true},
	{Name: "help", Description: "명령과 키보드 도움말"},
	{Name: "quit", Description: "관리 화면 접속 종료 (작업 유지)"},
	{Name: "exit", Description: "/quit와 동일"},
	{Name: "codexm_agents", Description: "관리 작업 목록"},
	{Name: "codexm_status", Description: "전체 작업·관리자·감시 상태"},
	{Name: "codexm_usage", Description: "전체 계정 한도·관리자 호출·세션별 토큰"},
	{Name: "codexm_logs", Args: "[all]", Description: "선택 작업 로그 · all은 전체 세션"},
	{Name: "codexm_manager", Args: "[지시]", Description: "입력 대상을 관리자로 변경 (모델 호출)"},
	{Name: "codexm_job", Description: "입력 대상을 선택한 작업으로 변경"},
	{Name: "codexm_accounts", Description: "모든 계정의 한도를 새로 조회 (모델 호출 없음)"},
	{Name: "codexm_account", Args: "[이름]", Description: "관리자 계정 조회 또는 변경"},
	{Name: "codexm_pause", Description: "선택 작업 일시 중지"},
	{Name: "codexm_resume", Description: "선택 관리 작업 재개"},
	{Name: "codexm_manage", Description: "기존 대화를 관리 목록에 보관"},
	{Name: "codexm_stop", Description: "선택 관리 작업 종료"},
	{Name: "codexm_complete", Description: "선택 작업 완료 처리"},
	{Name: "codexm_attach", Description: "선택 작업의 공식 Codex 화면 열기"},
	{Name: "codexm_read", Description: "선택 작업의 대화 열람"},
	{Name: "codexm_older", Description: "이전 대화 페이지"},
	{Name: "codexm_events", Description: "선택 작업의 사건 기록"},
	{Name: "codexm_steer", Args: "지시", Description: "현재 실행 중인 턴에 개입", NeedsArg: true},
	{Name: "codexm_filter", Args: "[검색어]", Description: "작업 목록 검색"},
	{Name: "codexm_state", Args: "[상태]", Description: "작업 상태 필터"},
	{Name: "codexm_answer", Args: "응답", Description: "선택 작업의 질문·승인에 응답", NeedsArg: true},
	{Name: "codexm_settings", Description: "관리자·계정 정책·저장 위치"},
	{Name: "codexm_shutdown", Description: "관리 작업과 감시 서비스 종료"},
}

// Unambiguous old manager spellings remain accepted but are not advertised.
// Names with a native Codex meaning, especially /stop, are never aliases.
var managerAliases = map[string]string{
	"agents": "codexm_agents", "manager": "codexm_manager", "job": "codexm_job",
	"account": "codexm_account", "settings": "codexm_settings", "logs": "codexm_logs",
	"pause": "codexm_pause", "resume-job": "codexm_resume", "manage": "codexm_manage",
	"complete": "codexm_complete", "events": "codexm_events", "attach": "codexm_attach",
	"read": "codexm_read", "older": "codexm_older", "steer": "codexm_steer",
	"filter": "codexm_filter", "state": "codexm_state", "answer": "codexm_answer",
	"shutdown": "codexm_shutdown",
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
	return m.slash(text)
}

func commandHelp() string {
	var b strings.Builder
	b.WriteString("/ 을 입력하면 명령 목록이 열립니다.\n↑↓ 선택 · Tab 완성 · Enter 실행 · Esc 닫기\n\n")
	for _, c := range slashCommands {
		b.WriteString("/" + c.Name + " " + c.Args + " · " + c.Description + "\n")
	}
	b.WriteString("\n기본 입력창 포커스 · Tab 영역 이동 · Shift+Tab 반대 이동\nEnter 전송 · Alt+Enter / Shift+Enter 줄바꿈 · 여러 줄 붙여넣기\n목록: ↑↓ 선택 · Enter 선택 후 입력 · v 대화 읽기 · l 전체 로그\n입력창: PgUp/PgDn 대화 스크롤 · Ctrl+O 최근 응답 복사\n공식 화면 기능은 안내에서 Enter로 열고 해당 명령을 입력합니다.\n")
	return b.String()
}
