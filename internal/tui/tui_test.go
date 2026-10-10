package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"strings"
	"testing"
)

func TestInputTargetAndKoreanMultilinePaste(t *testing.T) {
	c := config.Defaults()
	c.Manager.Account = "account-backup"
	m := New(c, "codexm")
	m.Status.Jobs = []jobs.Job{{ID: "job", Title: "작업", Managed: true, Kind: "chat"}}
	m.SelectedID = "job"
	m.focusInput("")
	text := "한글 입력\n여러 줄 지시"
	next, _ := m.Update(tea.PasteMsg{Content: text})
	m = next.(Model)
	if m.Input.Value() != text {
		t.Fatal(m.Input.Value())
	}
	m.slash("/manager")
	if m.Target != "manager" || !strings.Contains(m.View().Content, "관리자 account-backup (모델 호출)") {
		t.Fatal("manager target hidden")
	}
	m.slash("/job")
	if m.Target != "job" {
		t.Fatal("job target was not restored")
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 40, Height: 20}, {Width: 120, Height: 40}} {
		next, _ = m.Update(size)
		m = next.(Model)
		if !strings.Contains(m.View().Content, "입력 대상:") {
			t.Fatal("input target missing after resize")
		}
	}
}
