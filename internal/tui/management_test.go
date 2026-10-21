package tui

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/application"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/settings"
)

func managementTestDocument() settings.Document {
	discord := true
	return settings.Document{Revision: "original", Accounts: []string{"a", "b"}, Values: settings.Values{
		ManagerAccount: "a", Priority: []string{"a", "b"}, FallbackAccount: "b", PreferredRecheckSeconds: 600,
		ReconcileIntervalSeconds: 2, MaxParallelJobs: 3, UsageSampleSeconds: 60, FiveHourDiscord: &discord,
	}, Application: "새 작업과 다음 계정 선택에 적용됩니다."}
}

func managementTestModel() Model {
	c := config.Defaults()
	c.Manager.Account = "a"
	m := New(c, "codexm")
	m.Management.Snapshot.AccountNames = []string{"a", "b"}
	m.Management.Snapshot.Priority = []string{"a", "b"}
	m.Management.Snapshot.ManagerAccount = "a"
	m.Management.Snapshot.ConfigRevision = "original"
	m.Management.Snapshot.RemovalAvailable = true
	return m
}

func managementReply(t *testing.T, method string, data any) replyMsg {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return replyMsg{Command: "app:" + method, Data: raw}
}

func TestAccountSelectionAndConfirmationsPreserveComposerAndPolicy(t *testing.T) {
	m := managementTestModel()
	m.focusInput("작성 중인 초안")
	m.Management.Mode = "accounts"
	before := m.Config
	if cmd := m.managementAction("mgmt:account:b"); cmd != nil || m.Management.Mode != "account" {
		t.Fatal("account selection attempted an application call")
	}
	if !reflect.DeepEqual(before, m.Config) || m.Input.Value() != "작성 중인 초안" {
		t.Fatal("account selection changed policy or composer")
	}
	if cmd := m.managementAction("mgmt:default"); cmd != nil || m.Management.Mode != "confirm-default" || m.Management.Form.Index != 1 {
		t.Fatal("native default change did not require an explicit confirmation")
	}
	view, _ := m.managementView(80, 20)
	if !strings.Contains(view, "기존 작업의 계정") || !strings.Contains(view, "선택 계정: b") {
		t.Fatal("native default confirmation did not explain its concrete scope")
	}
	if cmd := m.managementAction("mgmt:confirm-default"); cmd == nil {
		t.Fatal("confirmed native default change has no application command")
	}
	// Do not execute that command: this test never touches native authentication.
	m.Management.Busy = false
	m.Management.Mode = "account"
	if cmd := m.managementAction("mgmt:remove"); cmd != nil || m.Management.ConfirmRevision != "original" {
		t.Fatal("removal did not capture an exact registry revision")
	}
	m.Management.Snapshot.ConfigRevision = "newer"
	if m.Management.ConfirmRevision != "original" {
		t.Fatal("removal confirmation silently adopted a newer revision")
	}
	m.managementAction("mgmt:cancel")
	if m.Input.Value() != "작성 중인 초안" || !reflect.DeepEqual(before, m.Config) {
		t.Fatal("canceling account management changed existing work")
	}
}

func TestAccountTabUsesNewestObservedQuota(t *testing.T) {
	m := managementTestModel()
	now := time.Now()
	m.Management.Snapshot.State.Accounts = []accounts.Snapshot{{Account: "a", Authenticated: true, CheckedAt: now.Add(-time.Minute), Single: accounts.Bucket{Primary: &accounts.Window{UsedPercent: 10}}}}
	m.Status.Accounts = []accounts.Snapshot{{Account: "a", Authenticated: true, CheckedAt: now, Single: accounts.Bucket{Primary: &accounts.Window{UsedPercent: 95}}}}
	if text := m.accountSummary("a"); !strings.Contains(text, "단기 5%") {
		t.Fatal("account tab kept an old quota observation", text)
	}
}

func TestSettingsConflictReloadPreservesOnlyEditedFields(t *testing.T) {
	m := managementTestModel()
	original := managementTestDocument()
	m.Management.Mode, m.Management.Settings = "settings", original
	m.Management.SettingsForm = settingsForm(original)
	m.Management.SettingsForm.setValue("priority", "b, a")
	m.Management.SettingsForm.setValue("usage_sample_seconds", "typing")
	m.Management.Busy = true
	_, handled := m.managementUpdate(replyMsg{Command: "app:settings.save", Error: errors.New("revision conflict")})
	if !handled || m.Management.Busy || m.Management.SettingsForm.value("priority") != "b, a" || !strings.Contains(m.Management.Error, "입력은 보존") {
		t.Fatal("save conflict discarded the settings draft")
	}
	latest := managementTestDocument()
	latest.Revision, latest.Values.MaxParallelJobs = "latest", 5
	m.Management.KeepDraftOnLoad = true
	m.managementUpdate(managementReply(t, "settings.read", latest))
	if m.Management.Settings.Revision != "latest" || m.Management.SettingsForm.value("priority") != "b, a" || m.Management.SettingsForm.value("usage_sample_seconds") != "typing" || m.Management.SettingsForm.value("max_parallel_jobs") != "5" {
		t.Fatal("reload failed to merge the latest saved values with local edits")
	}
	m.Management.SettingsForm.setValue("usage_sample_seconds", "60")
	changed := settingsDraftChanges(&m.Management.SettingsForm, latest)
	if !reflect.DeepEqual(changed, []string{"priority"}) {
		t.Fatal("save would overwrite unedited settings changed by another client")
	}
	if cmd := m.managementAction("mgmt:save"); cmd == nil || !m.Management.Busy {
		t.Fatal("valid edited settings did not produce a shared application save")
	}
	// The command remains unexecuted so no real configuration or runtime changes.
}

func TestManagementDoesNotConsumeSupervisorTickOrFunctionTabs(t *testing.T) {
	m := managementTestModel()
	m.Management.Mode = "settings"
	if _, handled := m.managementUpdate(tickMsg(time.Now())); handled {
		t.Fatal("management intercepted the supervisor polling loop")
	}
	if _, handled := m.managementUpdate(tea.KeyPressMsg{Code: tea.KeyF2}); handled {
		t.Fatal("management blocked dashboard function-key navigation")
	}
}

func TestManagementPasteIsScopedToVisibleEditor(t *testing.T) {
	m := managementTestModel()
	m.focusInput("작업 검색 초안")
	m.Management.Mode = "accounts"
	if _, handled := m.managementUpdate(tea.PasteMsg{Content: "숨겨진 입력"}); !handled || m.Input.Value() != "작업 검색 초안" {
		t.Fatal("pasting on account management changed the hidden composer")
	}
	m.loginForm("")
	m.Management.Form.activate(0)
	if _, handled := m.managementUpdate(tea.PasteMsg{Content: "new-account"}); !handled || m.Management.Form.value("account") != "new-account" || m.Input.Value() != "작업 검색 초안" {
		t.Fatal("account form paste did not reach only the active editor")
	}
}

func TestManagementLateRepliesCannotNavigateOrReviveCanceledLogin(t *testing.T) {
	m := managementTestModel()
	m.ViewEpoch = 8
	m.Management.Mode, m.Tab = "settings", "settings"
	m.Management.Settings = managementTestDocument()
	m.Management.SettingsForm = settingsForm(m.Management.Settings)
	m.Management.SettingsForm.setValue("priority", "b, a")
	old := managementTestDocument()
	old.Revision = "older"
	reply := managementReply(t, "settings.read", old)
	reply.Epoch = 7
	m.managementUpdate(reply)
	if m.Management.Settings.Revision != "original" || m.Management.SettingsForm.value("priority") != "b, a" {
		t.Fatal("stale settings read replaced a newer draft")
	}
	staleSnapshot := managementSnapshot{AccountNames: []string{"stale"}}
	reply = managementReply(t, "snapshot", staleSnapshot)
	reply.Epoch = 7
	m.managementUpdate(reply)
	if !reflect.DeepEqual(m.Management.Snapshot.AccountNames, []string{"a", "b"}) {
		t.Fatal("stale account snapshot replaced a newer view")
	}
	m.Management.Login = accounts.LoginStatus{LoginID: "old-login", Status: "canceled"}
	reply = managementReply(t, "login.status", accounts.LoginStatus{LoginID: "old-login", Status: "pending", UserCode: "TEST-ONLY"})
	m.managementUpdate(reply)
	if m.Management.Login.Status != "canceled" || m.Management.Login.UserCode != "" {
		t.Fatal("a late status read revived a canceled sign-in")
	}
	m.Management.Mode = "confirm-default"
	m.Management.ConfirmAccount = "b"
	reply = managementReply(t, "accounts.default", map[string]string{"account": "b"})
	reply.Epoch = 7
	m.managementUpdate(reply)
	if m.Management.Mode != "confirm-default" || m.Tab != "settings" {
		t.Fatal("a late native-default completion navigated the current view")
	}
}

func TestManagementLoginPollingSurvivesOtherManagementRequests(t *testing.T) {
	m := managementTestModel()
	m.Management.Login = accounts.LoginStatus{LoginID: "pending", Status: "pending"}
	if m.scheduleManagementLoginPoll() == nil || m.scheduleManagementLoginPoll() != nil {
		t.Fatal("login polling did not keep a single outstanding timer")
	}
	m.Management.Busy = true
	if cmd, handled := m.managementUpdate(managementLoginTick{LoginID: "pending"}); !handled || cmd == nil || m.Management.LoginPollID != "pending" {
		t.Fatal("another management request permanently stopped login polling")
	}
	m.Management.Busy = false
	if cmd, handled := m.managementUpdate(managementLoginTick{LoginID: "pending"}); !handled || cmd == nil || m.Management.LoginPollID != "" {
		t.Fatal("login status polling did not resume after the other request")
	}
}

type managementFakeLogin struct {
	Starts, Cancels int
	Account, Mode   string
}

func (f *managementFakeLogin) Start(_ context.Context, account, mode string) (accounts.LoginStatus, error) {
	f.Starts++
	f.Account, f.Mode = account, mode
	return accounts.LoginStatus{Account: account, Mode: mode, LoginID: "fake-login", Status: "pending", VerificationURL: "https://auth.openai.com/codex/device", UserCode: "TEST-ONLY"}, nil
}
func (f *managementFakeLogin) Status(string) (accounts.LoginStatus, error) {
	return accounts.LoginStatus{Account: f.Account, Mode: f.Mode, LoginID: "fake-login", Status: "pending"}, nil
}
func (f *managementFakeLogin) Cancel(context.Context, string) (accounts.LoginStatus, error) {
	f.Cancels++
	return accounts.LoginStatus{Account: f.Account, LoginID: "fake-login", Status: "canceled"}, nil
}
func (f *managementFakeLogin) Close() {}

func TestFirstRunLoginUsesOwnerScopedApplicationAndCancel(t *testing.T) {
	dir := t.TempDir()
	c := config.Defaults()
	c.Path = filepath.Join(dir, "config.toml")
	c.Manager.Account = ""
	c.Runtime.AccountsDir = filepath.Join(dir, "accounts")
	c.Runtime.SharedCodexHome = filepath.Join(dir, "codex")
	c.Storage.StateDir = filepath.Join(dir, "state")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	m := New(c, "codexm")
	if m.Screen != "setup" {
		t.Fatal("fixture did not start before manager selection")
	}
	m.App = application.New(context.Background(), c, "codexm")
	defer m.App.Close()
	fake := &managementFakeLogin{}
	m.App.NewLogins = func(context.Context, config.Config) application.LoginBroker { return fake }
	m.Management.Mode = "accounts"
	m.managementAction("mgmt:add")
	m.Management.Form.setValue("account", "new-account")
	cmd := m.managementAction("mgmt:login-start")
	if cmd == nil || fake.Starts != 0 {
		t.Fatal("login started before its explicit application command")
	}
	msg := cmd()
	m.managementUpdate(msg)
	if fake.Starts != 1 || fake.Account != "new-account" || fake.Mode != "device" || m.Management.Mode != "login-status" || m.Screen != "setup" {
		t.Fatal("first-run login did not use the shared owner-scoped broker")
	}
	cmd = m.managementAction("mgmt:login-cancel")
	if cmd == nil {
		t.Fatal("pending sign-in has no cancel action")
	}
	m.managementUpdate(cmd())
	if fake.Cancels != 1 || m.Management.Login.UserCode != "" || m.Management.Login.VerificationURL != "" {
		t.Fatal("cancel did not clear private login details")
	}
}

func TestOfficialLoginLinksRejectUntrustedTargets(t *testing.T) {
	for _, href := range []string{"https://auth.openai.com/codex/device", "https://chatgpt.com/auth"} {
		if !safeManagementLoginURL(href) {
			t.Fatal("official HTTPS URL rejected")
		}
	}
	for _, href := range []string{"http://auth.openai.com/", "https://auth.openai.com.evil.test/", "https://auth.openai.com@evil.test/", "https://auth.openai.com:444/", "file:///tmp/test", "javascript:alert(1)"} {
		if safeManagementLoginURL(href) {
			t.Fatal("untrusted URL accepted")
		}
	}
}
