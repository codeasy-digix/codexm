package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"strings"
	"testing"
)

func TestComposerHistoryRestoresDraftWithoutChangingSelectedSession(t *testing.T) {
	c := config.Defaults()
	c.Manager.Account = "account-backup"
	m := New(c, "codexm")
	m.SelectedID = "selected"
	m.rememberInput("/logs all")
	m.rememberInput("/usage")
	m.focusInput("아직 보내지 않은 초안")
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.Input.Value() != "/usage" || m.SelectedID != "selected" {
		t.Fatal("history changed selection")
	}
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.Input.Value() != "아직 보내지 않은 초안" {
		t.Fatal("draft lost")
	}
}

func TestAccountPickerKeepsOriginalTargetAcrossListRefresh(t *testing.T) {
	c := config.Defaults()
	c.WorkerAccounts.Priority = []string{"account-work", "account-secondary", "account-backup"}
	c.Manager.Account = "account-backup"
	m := New(c, "codexm")
	m.Status.Jobs = []jobs.Job{{ID: "first", Title: "original target", Account: "account-work", Kind: "chat", Managed: true}, {ID: "second", Kind: "chat", Managed: true}}
	m.SelectedID = "first"
	if cmd := m.slash("/account"); cmd != nil {
		t.Fatal("opening picker performed a mutation")
	}
	if m.Screen != "account" || m.AccountJobID != "first" || !strings.Contains(m.View().Content, "original target") {
		t.Fatal("missing selection menu")
	}
	m.SelectedID = "second"
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.AccountJobID != "first" || m.AccountNames[m.AccountSelected] != "account-secondary" {
		t.Fatal("picker retargeted after list refresh")
	}
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.Screen != "agents" || m.Focus != "input" {
		t.Fatal("cancel did not restore input")
	}
}
