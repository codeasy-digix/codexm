package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFormDraftEditingCancelAndKeyboardActions(t *testing.T) {
	f := newForm("설정", []formField{
		{Key: "title", Label: "제목", Value: "원래 값", Kind: "text"},
		{Key: "account", Label: "계정", Value: "a", Kind: "select", Options: []formOption{{"a", "첫 계정"}, {"b", "다음 계정"}}},
		{Key: "notify", Label: "알림", Kind: "toggle", Value: "false"},
	}, []formAction{{"save", "저장"}})
	f.activate(0)
	f.editor.SetValue("")
	if _, _, handled := f.update(tea.PasteMsg{Content: "작성 중인 한글"}); !handled || f.value("title") != "작성 중인 한글" {
		t.Fatal("Korean paste was not retained in the local field draft")
	}
	f.update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if f.Editing || f.value("title") != "원래 값" {
		t.Fatal("Escape did not restore the field before editing")
	}
	f.update(tea.KeyPressMsg{Code: tea.KeyTab})
	f.update(tea.KeyPressMsg{Code: tea.KeyRight})
	if f.value("account") != "b" {
		t.Fatal("account choice did not remain in the local form")
	}
	f.update(tea.KeyPressMsg{Code: tea.KeyTab})
	f.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if f.value("notify") != "true" {
		t.Fatal("toggle was not keyboard accessible")
	}
	f.update(tea.KeyPressMsg{Code: tea.KeyTab})
	action, _, _ := f.update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if action != "save" {
		t.Fatal("save button did not return its explicit action")
	}
}

func TestFormReadOnlyInfoCanBePagedAndButtonsRemainVisible(t *testing.T) {
	details := "first command line\n" + strings.Repeat("long command detail\n", 12) + "last command line"
	f := newForm("승인 요청", []formField{{Key: "details", Label: "요청 내용", Kind: "info", Value: details}}, []formAction{{"accept", "이번 실행 허용"}, {"cancel", "취소"}})
	if action := f.activate(0); action != "" || f.Editing {
		t.Fatal("read-only request details became editable")
	}
	view, _ := f.render(24, 5, "request:")
	if !strings.Contains(view, "first command line") {
		t.Fatal("long request details did not start at their first line")
	}
	f.update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	view, _ = f.render(24, 5, "request:")
	if strings.Contains(view, "first command line") || f.value("details") != details {
		t.Fatal("paging edited or failed to scroll request details")
	}
	f.update(tea.KeyPressMsg{Code: tea.KeyTab})
	view, hits := f.render(20, 3, "request:")
	if !strings.Contains(view, "이번 실행 허용") || len(strings.Split(view, "\n")) > 3 {
		t.Fatal("selected approval button is hidden in a short terminal")
	}
	found := false
	for _, hit := range hits {
		if hit.Action == "request:1" {
			found = true
		}
		if hit.X < 0 || hit.Y < 0 || hit.Y+hit.H > 3 || hit.X+hit.W > 20 {
			t.Fatal("form produced hit coordinates outside the visible terminal")
		}
	}
	if !found {
		t.Fatal("selected approval button is not clickable")
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > 20 {
			t.Fatal("wide text overflowed the form")
		}
	}
}
