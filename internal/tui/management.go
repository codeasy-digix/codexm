package tui

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/cursor"
	tea "charm.land/bubbletea/v2"
	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/settings"
)

type managementSnapshot struct {
	AccountNames         []string `json:"account_names"`
	Priority             []string `json:"priority"`
	ManagerAccount       string   `json:"manager_account"`
	NativeDefaultAccount string   `json:"native_default_account"`
	FallbackAccount      string   `json:"fallback_account"`
	ConfigRevision       string   `json:"config_revision"`
	Connected            bool     `json:"connected"`
	RemovalAvailable     bool     `json:"account_removal_available"`
	State                struct {
		Accounts []accounts.Snapshot `json:"accounts"`
	} `json:"state"`
}

type managementState struct {
	Mode, Account, Error, Message string
	Snapshot                      managementSnapshot
	Selected, Scroll              int
	ManualScroll                  bool
	Form, SettingsForm            formState
	Settings                      settings.Document
	KeepDraftOnLoad               bool
	Login                         accounts.LoginStatus
	LoginPollID                   string
	Busy                          bool
	ConfirmAccount                string
	ConfirmRevision               string
}

type managementLoginTick struct{ LoginID string }
type managementBrowserMsg struct{ Error error }
type managementControl struct{ Label, Action string }

func managementLoginPending(login accounts.LoginStatus) bool {
	return login.Status == "pending" || login.Status == "verifying"
}

func managementLoginPoll(id string) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return managementLoginTick{LoginID: id} })
}

func (m *Model) scheduleManagementLoginPoll() tea.Cmd {
	s := &m.Management
	if !managementLoginPending(s.Login) || s.Login.LoginID == "" || s.LoginPollID == s.Login.LoginID {
		return nil
	}
	s.LoginPollID = s.Login.LoginID
	return managementLoginPoll(s.Login.LoginID)
}

func (m *Model) managementVisible() bool { return m.Management.Mode != "" }

func (m *Model) openAccountManagement() tea.Cmd {
	m.ViewEpoch++
	m.Management.Mode = "accounts"
	m.Management.Error, m.Management.Message = "", ""
	m.Management.Selected, m.Management.Scroll = 0, 0
	m.Management.ManualScroll = false
	m.Tab = "accounts"
	m.Input.Blur()
	return m.appCall("snapshot", nil)
}

func (m *Model) openSettingsEditor() tea.Cmd {
	m.ViewEpoch++
	m.Management.KeepDraftOnLoad = m.Management.Settings.Revision != ""
	m.Management.Mode, m.Tab = "settings", "settings"
	m.Management.Error, m.Management.Message = "", ""
	m.Input.Blur()
	return m.appCall("settings.read", nil)
}

func (m *Model) closeManagement() {
	m.Management.Mode, m.Tab = "", "tasks"
	m.Management.Error = ""
	if m.Screen == "setup" {
		m.Focus = "list"
	} else {
		m.Focus = "input"
		m.Input.Focus()
	}
}

func (m *Model) accountSnapshot(name string) accounts.Snapshot {
	latest := accounts.Snapshot{Account: name}
	for _, snapshot := range m.Management.Snapshot.State.Accounts {
		if snapshot.Account == name {
			latest = snapshot
		}
	}
	for _, snapshot := range m.Status.Accounts {
		if snapshot.Account == name {
			if latest.CheckedAt.IsZero() || snapshot.CheckedAt.After(latest.CheckedAt) {
				latest = snapshot
			}
		}
	}
	return latest
}

func (m *Model) managementControls() []managementControl {
	s := &m.Management
	switch s.Mode {
	case "accounts":
		controls := []managementControl{{"계정 추가", "mgmt:add"}, {"모든 계정 한도 새로 조회", "mgmt:refresh"}, {"우선순위·관리자·예비 계정 설정", "mgmt:settings"}}
		for _, name := range s.Snapshot.AccountNames {
			controls = append(controls, managementControl{name + " · " + m.accountSummary(name), "mgmt:account:" + name})
		}
		if managementLoginPending(s.Login) {
			controls = append(controls, managementControl{"진행 중인 로그인 확인", "mgmt:login-status"})
		}
		label := "작업 목록으로 돌아가기"
		if m.Screen == "setup" {
			label = "최초 관리자 선택으로 돌아가기"
		}
		return append(controls, managementControl{label, "mgmt:close"})
	case "account":
		return []managementControl{{"재로그인", "mgmt:relogin"}, {"native 기본 계정으로 저장…", "mgmt:default"}, {"계정 등록 삭제…", "mgmt:remove"}, {"계정 정책 설정", "mgmt:settings"}, {"계정 목록", "mgmt:back"}}
	case "login-status":
		if managementLoginPending(s.Login) {
			return []managementControl{{"공식 로그인 페이지 열기", "mgmt:login-open"}, {"로그인 상태 다시 확인", "mgmt:login-check"}, {"로그인 취소", "mgmt:login-cancel"}}
		}
		return []managementControl{{"같은 계정으로 다시 로그인", "mgmt:relogin"}, {"계정 목록", "mgmt:back"}}
	}
	return nil
}

func (m *Model) accountSummary(name string) string {
	s := &m.Management.Snapshot
	var roles []string
	if index := slices.Index(s.Priority, name); index >= 0 {
		roles = append(roles, fmt.Sprintf("우선순위 %d", index+1))
	} else {
		roles = append(roles, "자동 선택 제외")
	}
	if name == s.ManagerAccount {
		roles = append(roles, "관리자")
	}
	if name == s.FallbackAccount {
		roles = append(roles, "예비 계정")
	}
	if name == s.NativeDefaultAccount {
		roles = append(roles, "native 기본")
	}
	a := m.accountSnapshot(name)
	bucket := a.Single
	if b, ok := a.Buckets["codex"]; ok {
		bucket = b
	}
	remaining := func(w *accounts.Window) string {
		if w == nil || a.Error != "" || !a.Authenticated {
			return "미확인"
		}
		return fmt.Sprintf("%d%%", max(0, 100-w.UsedPercent))
	}
	return strings.Join(roles, " · ") + " · 단기 " + remaining(bucket.Primary) + " / 장기 " + remaining(bucket.Secondary)
}

func (m *Model) loginForm(name string) {
	s := &m.Management
	s.Mode, s.Account, s.Selected, s.Scroll, s.Error = "login", name, 0, 0, ""
	s.Form = newForm("계정 추가 · 재로그인", []formField{
		{Key: "account", Label: "계정 별칭", Kind: "text", Value: name, Required: true, Help: "영문·숫자·밑줄·하이픈, 최대 64자. default는 사용할 수 없습니다."},
		{Key: "mode", Label: "로그인 방식", Kind: "select", Value: "device", Options: []formOption{{"device", "기기 코드 (다른 컴퓨터에서도 인증 가능)"}, {"browser", "이 컴퓨터의 브라우저"}}},
	}, []formAction{{"login-start", "공식 로그인 시작"}, {"cancel", "취소"}})
}

func managementOptions(names []string, empty bool) []formOption {
	options := []formOption{}
	if empty {
		options = append(options, formOption{"", "사용하지 않음"})
	}
	for _, name := range names {
		options = append(options, formOption{name, name})
	}
	return options
}

func settingsForm(doc settings.Document) formState {
	v := doc.Values
	return newForm("관리 설정", []formField{
		{Key: "manager_account", Label: "관리자 계정", Kind: "select", Value: v.ManagerAccount, Options: managementOptions(doc.Accounts, false), Required: true},
		{Key: "priority", Label: "작업 계정 우선순위", Kind: "text", Value: strings.Join(v.Priority, ", "), Help: "쉼표로 구분 · 저장된 계정: " + strings.Join(doc.Accounts, ", ")},
		{Key: "fallback_account", Label: "예비 계정", Kind: "select", Value: v.FallbackAccount, Options: managementOptions(doc.Accounts, true), Help: "항상 마지막 순서. 상위 계정 모두 소진이 확인된 경우에만 사용합니다."},
		{Key: "preferred_recheck_seconds", Label: "우선 계정 복귀 확인 (초)", Kind: "number", Value: strconv.Itoa(v.PreferredRecheckSeconds), Required: true, Help: "60–86400"},
		{Key: "reconcile_interval_seconds", Label: "작업 상태 확인 (초)", Kind: "number", Value: strconv.Itoa(v.ReconcileIntervalSeconds), Required: true, Help: "1–300"},
		{Key: "max_parallel_jobs", Label: "동시 실행 작업 수", Kind: "number", Value: strconv.Itoa(v.MaxParallelJobs), Required: true, Help: "1–32"},
		{Key: "usage_sample_seconds", Label: "사용량 관측 간격 (초)", Kind: "number", Value: strconv.Itoa(v.UsageSampleSeconds), Required: true, Help: "10–3600"},
		{Key: "five_hour_discord", Label: "5시간 한도 소진 시 Discord 알림", Kind: "toggle", Value: strconv.FormatBool(v.FiveHourDiscord != nil && *v.FiveHourDiscord)},
	}, []formAction{{"save", "설정 저장"}, {"reload", "최신 설정 확인 · 입력 유지"}, {"reset", "입력한 변경을 저장된 값으로 되돌리기"}, {"cancel", "닫기"}})
}

func settingsDraftChanges(form *formState, doc settings.Document) []string {
	original := settingsForm(doc)
	changed := []string{}
	for _, field := range form.Fields {
		if form.value(field.Key) != original.value(field.Key) {
			changed = append(changed, field.Key)
		}
	}
	return changed
}

func mergeSettingsDraft(form formState, original, next settings.Document) formState {
	changed := settingsDraftChanges(&form, original)
	merged := settingsForm(next)
	for _, key := range changed {
		merged.setValue(key, form.value(key))
	}
	merged.Index = min(form.Index, len(merged.Fields)+len(merged.Actions)-1)
	return merged
}

func settingsFormValues(form *formState) (settings.Values, error) {
	if !form.validate() {
		return settings.Values{}, fmt.Errorf("%s", form.Error)
	}
	v := settings.Values{ManagerAccount: form.value("manager_account"), FallbackAccount: form.value("fallback_account")}
	for _, name := range strings.Split(form.value("priority"), ",") {
		if name = strings.TrimSpace(name); name != "" {
			v.Priority = append(v.Priority, name)
		}
	}
	for _, field := range []struct {
		Key      string
		Min, Max int
		Dest     *int
	}{
		{"preferred_recheck_seconds", 60, 86400, &v.PreferredRecheckSeconds},
		{"reconcile_interval_seconds", 1, 300, &v.ReconcileIntervalSeconds},
		{"max_parallel_jobs", 1, 32, &v.MaxParallelJobs},
		{"usage_sample_seconds", 10, 3600, &v.UsageSampleSeconds},
	} {
		n, err := strconv.Atoi(form.value(field.Key))
		if err != nil || n < field.Min || n > field.Max {
			return settings.Values{}, fmt.Errorf("%s: %d–%d 범위의 정수를 입력하세요", field.Key, field.Min, field.Max)
		}
		*field.Dest = n
	}
	discord := form.value("five_hour_discord") == "true"
	v.FiveHourDiscord = &discord
	return v, nil
}

func (m *Model) managementAction(action string) tea.Cmd {
	action = strings.TrimPrefix(action, "mgmt:")
	s := &m.Management
	if strings.HasPrefix(action, "wheel:") {
		delta, _ := strconv.Atoi(strings.TrimPrefix(action, "wheel:"))
		if s.Mode == "settings" || s.Mode == "login" || strings.HasPrefix(s.Mode, "confirm-") {
			form := &s.Form
			if s.Mode == "settings" {
				form = &s.SettingsForm
			}
			form.scrollBy(delta * 3)
		} else {
			s.Scroll = max(0, s.Scroll+delta*3)
			s.ManualScroll = true
			m.managementView(max(1, m.Width), max(1, m.Height-7))
		}
		return nil
	}
	if action == "accounts" {
		return m.openAccountManagement()
	}
	if action == "settings" {
		return m.openSettingsEditor()
	}
	if strings.HasPrefix(action, "account:") {
		name := strings.TrimPrefix(action, "account:")
		if slices.Contains(s.Snapshot.AccountNames, name) {
			s.Mode, s.Account, s.Selected, s.Scroll, s.Error = "account", name, 0, 0, ""
		}
		return nil
	}
	if strings.HasPrefix(action, "field:") {
		if s.Busy {
			m.Notice = "요청 처리 중입니다."
			return nil
		}
		index, err := strconv.Atoi(strings.TrimPrefix(action, "field:"))
		if err != nil {
			return nil
		}
		form := &s.Form
		if s.Mode == "settings" {
			form = &s.SettingsForm
		}
		return m.managementAction(form.activate(index))
	}
	if s.Busy && action != "login-open" {
		m.Notice = "요청 처리 중입니다."
		return nil
	}
	s.Error, s.Message = "", ""
	switch action {
	case "":
		return nil
	case "add":
		if managementLoginPending(s.Login) {
			s.Mode = "login-status"
		} else {
			m.loginForm("")
		}
	case "relogin":
		if managementLoginPending(s.Login) {
			s.Mode = "login-status"
		} else {
			m.loginForm(s.Account)
		}
	case "refresh":
		return m.appCall("accounts.refresh", nil)
	case "back":
		return m.openAccountManagement()
	case "close":
		m.closeManagement()
	case "cancel":
		if s.Mode == "settings" || s.Mode == "accounts" {
			m.closeManagement()
		} else if s.Mode == "login-status" && managementLoginPending(s.Login) {
			return m.managementAction("login-cancel")
		} else {
			s.Mode, s.Selected, s.Scroll = "accounts", 0, 0
		}
	case "login-start":
		if !s.Form.validate() {
			return nil
		}
		name, mode := strings.TrimSpace(s.Form.value("account")), s.Form.value("mode")
		if !config.Nickname.MatchString(name) || name == "default" {
			s.Form.Error = "계정 별칭은 영문·숫자로 시작하고 영문·숫자·밑줄·하이픈으로 구성하세요. default는 사용할 수 없습니다."
			return nil
		}
		if m.App == nil {
			s.Error = "계정 관리 연결을 준비하지 못했습니다."
			return nil
		}
		s.Account, s.Busy = name, true
		return m.appCall("login.start", map[string]any{"account": name, "mode": mode, "generation": m.App.Generation})
	case "login-status":
		s.Mode, s.Selected, s.Scroll = "login-status", 0, 0
	case "login-check":
		if s.Login.LoginID != "" {
			return m.appCall("login.status", map[string]any{"login_id": s.Login.LoginID})
		}
	case "login-cancel":
		if managementLoginPending(s.Login) && m.App != nil {
			s.Busy = true
			return m.appCall("login.cancel", map[string]any{"login_id": s.Login.LoginID, "generation": m.App.Generation})
		}
		s.Mode = "accounts"
	case "login-open":
		href := s.Login.AuthURL
		if href == "" {
			href = s.Login.VerificationURL
		}
		if !managementLoginPending(s.Login) || !safeManagementLoginURL(href) {
			s.Error = "유효한 공식 로그인 주소가 없습니다. 로그인 상태를 다시 확인하세요."
			return nil
		}
		return openManagementLoginURL(href)
	case "default", "remove":
		if !slices.Contains(s.Snapshot.AccountNames, s.Account) {
			s.Error = "등록된 계정을 선택하세요."
			return nil
		}
		s.ConfirmAccount, s.ConfirmRevision = s.Account, s.Snapshot.ConfigRevision
		if action == "default" {
			s.Mode = "confirm-default"
			s.Form = newForm(s.Account+" 계정을 native 기본 계정으로 저장", nil, []formAction{{"confirm-default", "이 계정을 새 native 기본 로그인으로 저장"}, {"cancel", "취소"}})
			s.Form.Index = 1
		} else {
			if !s.Snapshot.RemovalAvailable || s.ConfirmRevision == "" {
				s.Error = "계정 목록을 새로고침하고 등록 삭제 지원을 확인하세요."
				return nil
			}
			s.Mode = "confirm-remove"
			s.Form = newForm(s.Account+" 계정 등록 삭제 확인", nil, []formAction{{"confirm-remove", "목록과 작업 우선순위에서 등록 삭제"}, {"cancel", "취소"}})
			s.Form.Index = 1
		}
	case "confirm-default":
		if s.Mode == "confirm-default" && s.ConfirmAccount != "" {
			s.Busy = true
			return m.appCall("accounts.default", map[string]any{"account": s.ConfirmAccount})
		}
	case "confirm-remove":
		if s.Mode == "confirm-remove" && s.ConfirmAccount != "" && s.ConfirmRevision != "" {
			s.Busy = true
			return m.appCall("accounts.remove", map[string]any{"account": s.ConfirmAccount, "revision": s.ConfirmRevision})
		}
	case "save":
		if s.Mode == "login" {
			return m.managementAction("login-start")
		}
		if s.Mode != "settings" || s.Settings.Revision == "" {
			return nil
		}
		values, err := settingsFormValues(&s.SettingsForm)
		if err != nil {
			s.Error = err.Error()
			return nil
		}
		changed := settingsDraftChanges(&s.SettingsForm, s.Settings)
		if len(changed) == 0 {
			s.Message = "변경된 설정이 없습니다."
			return nil
		}
		s.Busy = true
		return m.appCall("settings.save", map[string]any{"revision": s.Settings.Revision, "values": values, "changed": changed})
	case "reload":
		if s.Mode == "settings" {
			m.ViewEpoch++
			s.KeepDraftOnLoad = true
			return m.appCall("settings.read", nil)
		}
	case "reset":
		if s.Mode == "settings" {
			s.SettingsForm = settingsForm(s.Settings)
			s.Message = "입력을 현재 불러온 설정으로 되돌렸습니다."
		}
	}
	return nil
}

func (m *Model) managementUpdate(msg tea.Msg) (tea.Cmd, bool) {
	s := &m.Management
	switch v := msg.(type) {
	case managementLoginTick:
		if v.LoginID != s.LoginPollID {
			return nil, true
		}
		s.LoginPollID = ""
		if v.LoginID == s.Login.LoginID && managementLoginPending(s.Login) {
			if s.Busy {
				return m.scheduleManagementLoginPoll(), true
			}
			return m.appCall("login.status", map[string]any{"login_id": v.LoginID}), true
		}
		return nil, true
	case managementBrowserMsg:
		if v.Error != nil {
			s.Error = "브라우저를 열지 못했습니다. 표시된 공식 주소를 직접 열어주세요."
		}
		return nil, true
	case replyMsg:
		if !strings.HasPrefix(v.Command, "app:") {
			return nil, false
		}
		method := strings.TrimPrefix(v.Command, "app:")
		switch method {
		case "snapshot", "accounts.refresh", "settings.read", "settings.save", "login.start", "login.status", "login.cancel", "accounts.default", "accounts.remove":
		default:
			return nil, false
		}
		sameEpoch := v.Epoch == m.ViewEpoch
		if !sameEpoch && (method == "snapshot" || method == "settings.read" || method == "accounts.refresh") {
			return nil, true
		}
		if method != "snapshot" && method != "accounts.refresh" && method != "settings.read" && method != "login.status" {
			s.Busy = false
		}
		if v.Error != nil {
			s.Error = v.Error.Error()
			if method == "settings.save" {
				s.Error += " · 입력은 보존했습니다. 최신 설정 확인 후 다시 저장하세요."
			}
			m.Notice = s.Error
			if (method == "login.status" || method == "login.cancel") && managementLoginPending(s.Login) {
				return m.scheduleManagementLoginPoll(), true
			}
			return nil, true
		}
		switch method {
		case "snapshot":
			var snapshot managementSnapshot
			if err := json.Unmarshal(v.Data, &snapshot); err != nil {
				s.Error = "계정 목록 응답을 읽지 못했습니다."
				return nil, true
			}
			s.Snapshot = snapshot
			m.Names = slices.Clone(snapshot.AccountNames)
			m.Config.WorkerAccounts.Priority = slices.Clone(snapshot.Priority)
			m.Config.WorkerAccounts.FallbackAccount = snapshot.FallbackAccount
			m.Config.Manager.Account = snapshot.ManagerAccount
		case "accounts.refresh":
			var snapshots []accounts.Snapshot
			if err := json.Unmarshal(v.Data, &snapshots); err == nil {
				m.Status.Accounts = snapshots
				s.Snapshot.State.Accounts = snapshots
			}
			s.Message = "계정 한도를 새로 관측했습니다."
			return m.appCall("snapshot", nil), true
		case "settings.read", "settings.save":
			var doc settings.Document
			if err := json.Unmarshal(v.Data, &doc); err != nil {
				s.Error = "설정 응답을 읽지 못했습니다. 입력은 보존했습니다."
				return nil, true
			}
			if method == "settings.read" && s.KeepDraftOnLoad && s.Settings.Revision != "" {
				s.SettingsForm = mergeSettingsDraft(s.SettingsForm, s.Settings, doc)
				s.Message = "최신 설정을 확인했습니다. 입력한 항목은 유지했습니다."
			} else {
				s.SettingsForm = settingsForm(doc)
			}
			s.Settings, s.KeepDraftOnLoad = doc, false
			m.Names = slices.Clone(doc.Accounts)
			s.Error = ""
			if method == "settings.save" {
				s.Message = "설정을 저장했습니다. " + doc.Application
				m.Config.Manager.Account = doc.Values.ManagerAccount
				m.Config.WorkerAccounts.Priority = slices.Clone(doc.Values.Priority)
				m.Config.WorkerAccounts.FallbackAccount = doc.Values.FallbackAccount
				m.Config.WorkerAccounts.PreferredRecheckSeconds = doc.Values.PreferredRecheckSeconds
				m.Config.Supervisor.ReconcileIntervalSeconds = doc.Values.ReconcileIntervalSeconds
				m.Config.Execution.MaxParallelJobs = doc.Values.MaxParallelJobs
				m.Config.Logging.UsageSampleSeconds = doc.Values.UsageSampleSeconds
				if doc.Values.FiveHourDiscord != nil {
					m.Config.Notifications.FiveHourDiscord = *doc.Values.FiveHourDiscord
				}
				if m.Screen == "setup" && doc.Values.ManagerAccount != "" {
					s.Mode = ""
					m.Screen = "agents"
					m.Loading = true
					m.Notice = "저장한 관리자 계정으로 관리 서비스 연결 중…"
					return m.start(false), true
				}
				if m.Screen == "setup" {
					m.Selected = max(0, slices.Index(m.Names, doc.Values.ManagerAccount))
				}
				m.Notice = s.Message
			}
		case "login.start", "login.status", "login.cancel":
			var login accounts.LoginStatus
			if err := json.Unmarshal(v.Data, &login); err != nil {
				s.Error = "로그인 상태를 읽지 못했습니다."
				return nil, true
			}
			if method == "login.status" && s.Login.LoginID != "" && (login.LoginID != s.Login.LoginID || !managementLoginPending(s.Login)) {
				// A status read may finish after cancel or after a new sign-in.
				return nil, true
			}
			if !managementLoginPending(login) {
				login.AuthURL, login.VerificationURL, login.UserCode = "", "", ""
			}
			s.Login, s.Account = login, login.Account
			if method == "login.start" && sameEpoch && s.Mode == "login" {
				s.Mode, s.Selected, s.Scroll = "login-status", 0, 0
			}
			if managementLoginPending(login) {
				return m.scheduleManagementLoginPoll(), true
			}
			s.LoginPollID = ""
			if login.Status == "succeeded" {
				s.Message = login.Account + " 로그인 완료. 관리자와 작업 우선순위는 설정에서 선택하세요."
				m.Notice = s.Message
				return m.appCall("snapshot", nil), true
			}
		case "accounts.default":
			if sameEpoch && s.Mode == "confirm-default" {
				s.Mode, s.Selected, s.Scroll = "account", 0, 0
			}
			s.Message = s.ConfirmAccount + " 계정을 이후 native 실행의 기본 로그인으로 저장했습니다."
			m.Notice = s.Message
			return m.appCall("snapshot", nil), true
		case "accounts.remove":
			if sameEpoch && s.Mode == "confirm-remove" {
				s.Mode, s.Selected, s.Scroll = "accounts", 0, 0
			}
			s.Message = s.ConfirmAccount + " 등록을 삭제했습니다. 로그인 파일과 대화는 보존했습니다."
			m.Notice = s.Message
			return m.appCall("snapshot", nil), true
		}
		return nil, true
	case tea.PasteMsg, cursor.BlinkMsg, tea.FocusMsg, tea.BlurMsg:
		if !m.managementVisible() {
			return nil, false
		}
		form := &s.Form
		if s.Mode == "settings" {
			form = &s.SettingsForm
		}
		if !s.Busy && (s.Mode == "settings" || s.Mode == "login" || strings.HasPrefix(s.Mode, "confirm-")) {
			_, cmd, handled := form.update(msg)
			return cmd, handled
		}
		_, paste := msg.(tea.PasteMsg)
		return nil, paste
	case tea.KeyPressMsg:
		if !m.managementVisible() || v.String() == "ctrl+c" {
			return nil, false
		}
		if slices.Contains([]string{"f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8"}, v.String()) {
			return nil, false
		}
		if s.Busy {
			if v.String() == "pgup" {
				return m.managementAction("wheel:-1"), true
			}
			if v.String() == "pgdown" {
				return m.managementAction("wheel:1"), true
			}
			m.Notice = "요청 처리 중입니다."
			return nil, true
		}
		if s.Mode == "settings" || s.Mode == "login" || s.Mode == "confirm-default" || s.Mode == "confirm-remove" {
			form := &s.Form
			if s.Mode == "settings" {
				form = &s.SettingsForm
			}
			action, cmd, handled := form.update(msg)
			if action != "" {
				return tea.Batch(cmd, m.managementAction(action)), true
			}
			return cmd, handled
		}
		controls := m.managementControls()
		switch v.String() {
		case "tab", "down", "shift+tab", "up":
			s.ManualScroll = false
			delta := 1
			if v.String() == "shift+tab" || v.String() == "up" {
				delta = -1
			}
			if len(controls) > 0 {
				s.Selected = (s.Selected + delta + len(controls)) % len(controls)
			}
		case "enter", " ", "space":
			if len(controls) > 0 {
				return m.managementAction(controls[min(s.Selected, len(controls)-1)].Action), true
			}
		case "esc":
			if s.Mode == "account" || s.Mode == "login-status" && !managementLoginPending(s.Login) {
				return m.managementAction("back"), true
			}
			return m.managementAction("cancel"), true
		case "r":
			return m.managementAction("refresh"), true
		case "pgup":
			s.ManualScroll, s.Scroll = true, max(0, s.Scroll-5)
			m.managementView(max(1, m.Width), max(1, m.Height-7))
		case "pgdown":
			s.ManualScroll, s.Scroll = true, s.Scroll+5
			m.managementView(max(1, m.Width), max(1, m.Height-7))
		case "n":
			return m.managementAction("add"), true
		}
		return nil, true
	}
	return nil, false
}

func safeManagementLoginURL(raw string) bool {
	if len(raw) == 0 || len(raw) > 8192 {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" {
		return false
	}
	return slices.Contains([]string{"auth.openai.com", "auth0.openai.com", "chatgpt.com", "platform.openai.com"}, parsed.Hostname())
}

func openManagementLoginURL(href string) tea.Cmd {
	return func() tea.Msg {
		var command *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			command = exec.Command("open", href)
		case "windows":
			command = exec.Command("rundll32", "url.dll,FileProtocolHandler", href)
		default:
			command = exec.Command("xdg-open", href)
		}
		return managementBrowserMsg{Error: command.Run()}
	}
}

func (m *Model) managementView(width, height int) (string, []hitRegion) {
	s := &m.Management
	if s.Mode == "settings" || s.Mode == "login" || s.Mode == "confirm-default" || s.Mode == "confirm-remove" {
		form := &s.Form
		if s.Mode == "settings" {
			form = &s.SettingsForm
			if s.Settings.Revision == "" {
				return "설정 불러오는 중… " + plain(s.Error), nil
			}
		}
		var notes []string
		if s.Mode == "confirm-default" {
			notes = append(notes, "선택 계정: "+s.ConfirmAccount, "이후 실행하는 공식 CLI·GUI의 기본 로그인에 적용됩니다. 기존 작업의 계정·작업 우선순위·관리자 계정은 유지됩니다.")
		}
		if s.Mode == "confirm-remove" {
			notes = append(notes, "선택 계정: "+s.ConfirmAccount, "목록과 작업 우선순위에서 등록을 삭제합니다. 로그인 파일·공용 기본 로그인·대화 기록은 보존합니다. 사용 중이거나 관리자 계정이면 삭제가 거부됩니다.")
		}
		if s.Mode == "settings" {
			notes = append(notes, s.Settings.Application)
		}
		if s.Busy {
			notes = append(notes, "요청 처리 중…")
		}
		if s.Error != "" {
			notes = append(notes, s.Error)
		}
		if s.Message != "" {
			notes = append(notes, s.Message)
		}
		originalTitle := form.Title
		if len(notes) > 0 {
			form.Title += "\n" + strings.Join(notes, "\n")
		}
		view, hits := form.render(width, height, "mgmt:field:")
		form.Title = originalTitle
		hits = append(hits, hitRegion{X: 0, Y: 0, W: width, H: height, Action: "mgmt:area"})
		return view, hits
	}
	lines := []formLine{}
	add := func(text string) { lines = appendFormLine(lines, text, width, -1, "") }
	switch s.Mode {
	case "accounts":
		add("계정 관리")
		add("작업 우선순위: " + strings.Join(s.Snapshot.Priority, " → "))
		add("관리자: " + managementUnknown(s.Snapshot.ManagerAccount) + " · native 기본: " + managementUnknown(s.Snapshot.NativeDefaultAccount))
		add("계정 선택은 상세만 엽니다. 한도는 계정 전체의 관측값입니다.")
		if len(s.Snapshot.AccountNames) == 0 {
			add("저장된 계정이 없습니다. 계정을 추가하고 관리자와 작업 우선순위를 설정하세요.")
		}
	case "account":
		add("계정 · " + s.Account)
		add(m.accountSummary(s.Account))
		a := m.accountSnapshot(s.Account)
		if !a.CheckedAt.IsZero() {
			add("최근 관측: " + a.CheckedAt.Local().Format("2006-01-02 15:04:05"))
		}
		bucket := a.Single
		if b, ok := a.Buckets["codex"]; ok {
			bucket = b
		}
		for i, window := range []*accounts.Window{bucket.Primary, bucket.Secondary} {
			if window != nil && window.ResetsAt != nil {
				label := "단기"
				if i == 1 {
					label = "장기"
				}
				add(label + " 재설정: " + time.Unix(*window.ResetsAt, 0).Local().Format("2006-01-02 15:04:05"))
			}
		}
		if a.Error != "" {
			add("조회 오류: " + a.Error)
		}
	case "login-status":
		add(s.Login.Account + " · " + managementLoginLabel(s.Login.Status))
		if managementLoginPending(s.Login) {
			add("공식 페이지에서 인증을 마치면 이 화면에서 자동으로 확인합니다.")
			if s.Login.UserCode != "" {
				add("기기 인증 코드: " + s.Login.UserCode)
			}
			href := s.Login.AuthURL
			if href == "" {
				href = s.Login.VerificationURL
			}
			if safeManagementLoginURL(href) {
				add("공식 주소: " + href)
			}
			if !s.Login.Deadline.IsZero() {
				add("이 로그인 요청 종료: " + s.Login.Deadline.Local().Format("15:04:05") + " (codexm 관리 제한)")
			}
		} else if s.Login.Error != "" {
			add(s.Login.Error)
		}
	}
	if s.Busy {
		add("요청 처리 중…")
	}
	if s.Error != "" {
		add(s.Error)
	}
	if s.Message != "" {
		add(s.Message)
	}
	add("Tab/↑↓ 이동 · Enter 선택 · Esc 돌아가기")
	controls := m.managementControls()
	for i, control := range controls {
		marker := "  "
		if i == s.Selected {
			marker = "› "
		}
		lines = appendFormLine(lines, marker+"[ "+control.Label+" ]", width, i, control.Action)
	}
	selected := s.Selected
	if s.ManualScroll {
		selected = -2
	}
	view, hits := clippedLines(lines, width, height, selected, &s.Scroll)
	hits = append(hits, hitRegion{X: 0, Y: 0, W: width, H: height, Action: "mgmt:area"})
	return view, hits
}

func managementUnknown(value string) string {
	if value == "" {
		return "미확인"
	}
	return value
}

func managementLoginLabel(status string) string {
	switch status {
	case "pending":
		return "공식 페이지에서 로그인 대기"
	case "verifying":
		return "인증 확인 중"
	case "succeeded":
		return "로그인 완료"
	case "failed":
		return "로그인 실패"
	case "canceled":
		return "로그인 취소됨"
	case "expired":
		return "로그인 요청 종료"
	}
	return "로그인 상태 확인 중"
}
