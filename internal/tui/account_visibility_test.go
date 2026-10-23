package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestAccountListExposesDeletionForEveryRegisteredAccount(t *testing.T) {
	for _, size := range [][2]int{{40, 10}, {120, 30}} {
		m := managementTestModel()
		m.Management.Mode = "accounts"
		for _, name := range m.Management.Snapshot.AccountNames {
			action := "mgmt:remove-account:" + name
			index := -1
			for i, control := range m.managementControls() {
				if control.Action == action && strings.Contains(control.Label, "계정 삭제") {
					index = i
					break
				}
			}
			if index == -1 {
				t.Fatalf("%s has no deletion control in the account list", name)
			}
			m.Management.Selected = index
			view, hits := m.managementView(size[0], size[1])
			found := false
			for _, hit := range hits {
				if hit.Action == action && hit.X >= 0 && hit.Y >= 0 && hit.Y+hit.H <= size[1] {
					found = true
				}
			}
			if !found || !strings.Contains(view, name+" 계정 삭제") {
				t.Fatalf("%s deletion is not keyboard-scrollable and mouse-clickable at %v", name, size)
			}
		}
	}
}

func TestAccountListDeletionOpensExactConfirmationWithoutMutation(t *testing.T) {
	m := managementTestModel()
	m.Management.Mode = "accounts"
	m.focusInput("보존할 채팅 초안")
	before := m.Config
	if cmd := m.managementAction("mgmt:remove-account:b"); cmd != nil {
		t.Fatal("opening account deletion attempted an application request")
	}
	if m.Management.Mode != "confirm-remove" || m.Management.ConfirmAccount != "b" || m.Management.ConfirmRevision != "original" || m.Management.Form.Index != 1 {
		t.Fatal("account-list deletion did not capture the exact account and revision with cancel selected")
	}
	view, _ := m.managementView(100, 20)
	for _, text := range []string{"선택 계정: b", "로그인 파일", "대화 기록", "작업 우선순위", "가용 계정", "관리자 계정은 역할을 먼저 변경"} {
		if !strings.Contains(view, text) {
			t.Fatalf("deletion confirmation omitted %q", text)
		}
	}
	if cmd, handled := m.managementUpdate(tea.KeyPressMsg{Code: tea.KeyEnter}); !handled || cmd != nil || m.Management.Mode != "accounts" {
		t.Fatal("pressing Enter on the default confirmation did not cancel without a request")
	}
	if !reflect.DeepEqual(before, m.Config) || m.Input.Value() != "보존할 채팅 초안" {
		t.Fatal("canceled account deletion changed configuration or the chat draft")
	}
	if cmd := m.managementAction("mgmt:confirm-remove"); cmd != nil {
		t.Fatal("a stale confirmation action can remove an account after cancel")
	}
}

func TestAccountListDeletionHonorsAvailabilityBusyAndAccountBoundaries(t *testing.T) {
	for _, scenario := range []string{"busy", "unsupported", "missing-revision", "stale-account", "wrong-view"} {
		t.Run(scenario, func(t *testing.T) {
			m := managementTestModel()
			m.Management.Mode = "accounts"
			switch scenario {
			case "busy":
				m.Management.Busy = true
			case "unsupported":
				m.Management.Snapshot.RemovalAvailable = false
			case "missing-revision":
				m.Management.Snapshot.ConfigRevision = ""
			case "stale-account":
				m.Management.Snapshot.AccountNames = []string{"a"}
			case "wrong-view":
				m.Management.Mode = "settings"
			}
			if cmd := m.managementAction("mgmt:remove-account:b"); cmd != nil || m.Management.Mode == "confirm-remove" {
				t.Fatal("unavailable or stale account action reached deletion confirmation")
			}
		})
	}
}
