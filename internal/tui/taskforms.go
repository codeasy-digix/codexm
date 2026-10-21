package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

type taskFormState struct {
	Kind                                          string
	Form                                          formState
	JobID, ThreadID, RunID, Title, Error, Pending string
	Busy, Open                                    bool
	Request                                       jobs.Request
	RequestIndex                                  int
	NeedsLoad                                     bool
	Uncertain                                     bool
}

func textField(key, label, value string) formField {
	return formField{Key: key, Label: label, Kind: "text", Value: value}
}
func selectField(key, label, value string, options ...formOption) formField {
	return formField{Key: key, Label: label, Kind: "select", Value: value, Options: options}
}
func toggleField(key, label string, value bool) formField {
	return formField{Key: key, Label: label, Kind: "toggle", Value: strconv.FormatBool(value)}
}
func choices(names []string, emptyLabel string) []formOption {
	out := []formOption{}
	if emptyLabel != "" {
		out = append(out, formOption{"", emptyLabel})
	}
	for _, name := range names {
		out = append(out, formOption{name, name})
	}
	return out
}

func (m *Model) beginTaskForm(kind string) {
	j, _ := m.selected()
	if slices.Contains([]string{"send", "goal", "requests", "unmanage"}, kind) && (j.ID == "" || !m.registered(j) || !j.Managed) {
		m.Notice = "관리 중인 대화를 먼저 선택하세요. 기존 대화는 /manage로 등록할 수 있습니다."
		return
	}
	if (kind == "send" || kind == "goal") && j.Kind == "group" {
		m.Notice = "그룹의 하위 대화를 선택하세요."
		return
	}
	m.ViewEpoch++
	m.Management.Mode = ""
	f := taskFormState{Kind: kind, JobID: j.ID, ThreadID: j.ThreadID, RunID: j.RunID, Title: j.Title}
	var fields []formField
	actions := []formAction{{"save", "저장"}, {"cancel", "취소"}}
	title := ""
	switch kind {
	case "actions":
		title = "작업 동작 · " + j.Title
		actions = nil
		for _, b := range m.taskButtons() {
			actions = append(actions, formAction{b.Action, b.Label})
		}
		actions = append(actions, formAction{"cancel", "닫기"})
	case "new":
		cwd, _ := os.Getwd()
		parents := []formOption{{"", "없음"}}
		for _, parent := range m.Status.Jobs {
			if parent.Kind == "group" {
				parents = append(parents, formOption{parent.ID, parent.Title})
			}
		}
		fields = []formField{
			textField("cwd", "작업 폴더", cwd), textField("title", "제목", filepath.Base(cwd)),
			selectField("kind", "작업 종류", "chat", formOption{"chat", "일반 채팅"}, formOption{"task", "단발 작업"}, formOption{"goal", "지속 목표"}, formOption{"group", "작업 그룹"}),
			selectField("account", "작업 계정", "", choices(m.Config.WorkerAccounts.Priority, "우선순위 자동 선택")...),
			textField("model", "모델 · 빈 값은 공식 기본 설정", ""),
			selectField("effort", "추론 강도", "", choices([]string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}, "공식 기본 설정")...),
			selectField("parent_id", "상위 그룹", "", parents...),
			{Key: "prompt", Label: "첫 지시 · 비워두면 입력 대기", Kind: "multiline"},
			{Key: "goal", Label: "목표 · 지속 목표에서만 사용", Kind: "multiline"},
			{Key: "budget", Label: "목표 토큰 예산 · 선택 사항", Kind: "number"},
			toggleField("read_only", "작업 폴더 읽기 전용", false),
			textField("completion", "단발 작업 완료 검사 · 인자 배열 (선택)", ""),
			toggleField("open", "생성 후 공식 Codex 터미널 열기", true),
		}
		fields[0].Required = true
		fields[11].Help = "예: [\"go\",\"test\",\"./...\"]. 저장 시 이 실행을 완료 검사로 허용합니다."
		title = "새 작업 · 만들기를 선택하면 첫 지시/목표가 실행될 수 있습니다."
		actions[0] = formAction{"save", "작업 만들기"}
	case "send":
		fields = []formField{{Key: "text", Label: "추가 지시", Kind: "multiline", Required: true}, toggleField("steer", "진행 중인 턴에 바로 전달", false)}
		title = "지시 보내기 · " + j.Title + " · " + j.Account
		actions[0] = formAction{"save", "지시 전송"}
	case "goal":
		objective, budget := "", ""
		if j.Goal != nil {
			objective = j.Goal.Objective
			if j.Goal.TokenBudget != nil {
				budget = strconv.FormatInt(*j.Goal.TokenBudget, 10)
			}
		}
		fields = []formField{{Key: "text", Label: "목표", Kind: "multiline", Value: objective, Required: true}, {Key: "budget", Label: "토큰 예산 · 비우면 기존 예산 유지", Kind: "number", Value: budget}, selectField("status", "목표 상태", "", formOption{"", "기존 상태 유지"}, formOption{"active", "계속 진행"}, formOption{"paused", "일시 중지"}, formOption{"blocked", "차단됨"}, formOption{"complete", "완료"})}
		title = "목표 편집 · " + j.Title + " · 사용량은 유지합니다."
	case "unmanage":
		title = "관리 등록 해제 · " + j.Title
		fields = []formField{{Key: "info", Label: "변경 내용", Kind: "info", Value: "자동 감시와 계정 전환의 관리 등록을 해제합니다. Codex 대화·목표·인증은 보존합니다. 실행 중인 작업은 백엔드의 안전한 해제 절차를 따릅니다."}}
		actions[0] = formAction{"save", "관리 등록 해제"}
	case "shutdown":
		title = "관리 서비스와 관리 작업 종료"
		fields = []formField{{Key: "info", Label: "변경 내용", Kind: "info", Value: "현재 관리 중인 작업과 감시 서비스를 종료합니다. 화면만 닫으려면 취소 후 /quit를 사용하세요."}}
		actions[0] = formAction{"save", "서비스 종료"}
	case "log-filter":
		fields = []formField{selectField("account", "계정", m.LogFilter.Account, choices(m.Config.WorkerAccounts.Priority, "모든 계정")...), selectField("days", "기간", strconv.Itoa(m.LogFilter.Days), formOption{"0", "모든 기간"}, formOption{"1", "최근 24시간"}, formOption{"7", "최근 7일"}), textField("reason", "계정 변경 사유 검색", m.LogFilter.Reason)}
		title = "사용 기록 필터"
		actions = []formAction{{"save", "필터 적용"}, {"reset", "필터 해제"}, {"cancel", "취소"}}
	case "filters":
		projects := []string{}
		for _, job := range m.allJobs() {
			if job.Cwd != "" && !slices.Contains(projects, job.Cwd) {
				projects = append(projects, job.Cwd)
			}
		}
		sort.Strings(projects)
		fields = []formField{textField("search", "제목·계정·폴더 검색", m.Filter), selectField("project", "프로젝트", m.ProjectFilter, choices(projects, "모든 프로젝트")...), selectField("account", "계정", m.AccountFilter, choices(m.Config.WorkerAccounts.Priority, "모든 계정")...), selectField("state", "상태", m.StateFilter, choices([]string{"running", "idle", "paused", "complete", "stopped", "external_writer", "quota_wait", "waiting_on_user", "waiting_on_approval", "saved"}, "모든 상태")...), toggleField("managed", "관리 중인 작업만", m.ManagedOnly), toggleField("active", "완료·종료 작업 제외", m.ActiveOnly)}
		title = "대화 검색·필터"
		actions = []formAction{{"save", "필터 적용"}, {"reset", "필터 해제"}, {"cancel", "취소"}}
	case "requests":
		if len(j.PendingRequests) == 0 {
			m.Notice = "현재 응답을 기다리는 요청이 없습니다."
			return
		}
		m.TaskForm = f
		m.buildRequestForm(j, 0)
		return
	case "resolve-action":
		title = "결과를 확인할 수 없는 작업 복구"
		fields = []formField{selectField("action", "작업 선택", "", formOption{"", "불러오는 중…"}), selectField("decision", "대화 기록에서 결과 확인 후 선택", "cancelled", formOption{"cancelled", "재전송하지 않고 취소 처리"}, formOption{"done", "이미 완료된 것으로 확인"})}
	default:
		return
	}
	f.NeedsLoad = kind == "new" || kind == "resolve-action"
	f.Form = newForm(title, fields, actions)
	m.TaskForm = f
	m.Notice = "Tab/Shift+Tab 필드 이동 · Enter 편집/선택 · Ctrl+S 저장 · Esc 취소"
}

func (m *Model) taskFormUpdate(msg tea.Msg) (tea.Cmd, bool) {
	if m.TaskForm.Kind == "" {
		return nil, false
	}
	if _, ok := msg.(tickMsg); ok && m.TaskForm.NeedsLoad {
		m.TaskForm.NeedsLoad = false
		method := "models.list"
		if m.TaskForm.Kind == "resolve-action" {
			method = "snapshot"
		}
		call := m.appCall(method, nil)
		load := func() tea.Msg { r := call().(replyMsg); r.Command = "task:" + method; return r }
		return tea.Batch(m.poll(), tick(), load), true
	}
	if reply, ok := msg.(replyMsg); ok {
		if strings.HasPrefix(reply.Command, "task:") && reply.Epoch != m.ViewEpoch {
			return nil, true
		}
		if reply.Command == "task:snapshot" && m.TaskForm.Kind == "resolve-action" {
			if reply.Error != nil {
				m.TaskForm.Error = reply.Error.Error()
				return nil, true
			}
			var snapshot struct {
				State struct {
					Actions []jobs.Action `json:"actions"`
				} `json:"state"`
			}
			if err := json.Unmarshal(reply.Data, &snapshot); err != nil {
				m.TaskForm.Error = err.Error()
				return nil, true
			}
			opts := []formOption{{"", "결과 불명 작업 없음"}}
			for _, a := range snapshot.State.Actions {
				if a.State == "outcome_unknown" {
					opts = append(opts, formOption{a.ID, a.ID + " · " + a.Kind + " · " + m.jobTitle(a.JobID)})
				}
			}
			m.TaskForm.Form.Fields[0].Options = opts
			return nil, true
		}
		if reply.Command == "task:models.list" && m.TaskForm.Kind == "new" {
			if reply.Error == nil {
				var data struct {
					Data []struct {
						ID          string `json:"id"`
						Model       string `json:"model"`
						DisplayName string `json:"displayName"`
					} `json:"data"`
				}
				if json.Unmarshal(reply.Data, &data) == nil {
					for i := range m.TaskForm.Form.Fields {
						if m.TaskForm.Form.Fields[i].Key == "model" {
							opts := []formOption{{"", "공식 기본 설정"}}
							for _, model := range data.Data {
								name := model.Model
								if name == "" {
									name = model.ID
								}
								label := model.DisplayName
								if label == "" {
									label = name
								}
								opts = append(opts, formOption{name, label})
							}
							if len(opts) > 1 {
								m.TaskForm.Form.Fields[i].Options = opts
								m.TaskForm.Form.Fields[i].Kind = "select"
							}
						}
					}
				}
			}
			return nil, true
		}
		if reply.Command != m.TaskForm.Pending || m.TaskForm.Pending == "" {
			return nil, false
		}
		m.TaskForm.Busy = false
		m.Loading = false
		if reply.Error != nil {
			m.TaskForm.Error = reply.Error.Error()
			if ambiguousUIError(reply.Error) {
				m.TaskForm.Uncertain = true
				m.TaskForm.Error += " · 같은 지시를 다시 보내지 않습니다. 닫은 뒤 대화와 복구 메뉴에서 결과를 확인하세요."
				if reply.OperationID != "" {
					m.TaskForm.Error += " · 요청 " + reply.OperationID
				}
			}
			m.TaskForm.Pending = ""
			return nil, true
		}
		kind, open := m.TaskForm.Kind, m.TaskForm.Open
		m.TaskForm = taskFormState{}
		m.Notice = "요청을 접수했습니다."
		if kind == "new" {
			var j jobs.Job
			if err := json.Unmarshal(reply.Data, &j); err != nil {
				m.Notice = err.Error()
				return nil, true
			}
			m.SelectedID = j.ID
			m.Tab = "tasks"
			m.Screen = "agents"
			m.Panel = ""
			m.DetailText = ""
			m.Input.Reset()
			m.returnInput()
			if open && j.Kind != "group" {
				return tea.Batch(m.poll(), m.openID(j.ID)), true
			}
		}
		if kind == "shutdown" {
			return tea.Quit, true
		}
		return m.poll(), true
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if m.TaskForm.Busy {
			m.Notice = "처리 결과를 확인 중입니다. 입력을 다시 전송하지 않습니다."
			return nil, true
		}
		action, cmd, handled := m.TaskForm.Form.update(key)
		if action != "" {
			return m.submitTaskForm(action), true
		}
		return cmd, handled
	}
	// Paste and editor cursor/blink messages belong to the active form.
	if _, cmd, handled := m.TaskForm.Form.update(msg); handled {
		return cmd, true
	}
	return nil, false
}

func (m *Model) taskFormAction(action string) tea.Cmd {
	if m.TaskForm.Kind == "" || m.TaskForm.Busy {
		return nil
	}
	if strings.HasPrefix(action, "task:field:") {
		i, err := strconv.Atoi(strings.TrimPrefix(action, "task:field:"))
		if err != nil {
			return nil
		}
		if a := m.TaskForm.Form.activate(i); a != "" {
			return m.submitTaskForm(a)
		}
	}
	return nil
}
func (m *Model) taskFormWheel(delta int) tea.Cmd {
	m.TaskForm.Form.scrollBy(delta * 3)
	return nil
}
func (m Model) taskFormView(width, height int) (string, []hitRegion) {
	f := m.TaskForm.Form
	errorLines := 0
	if m.TaskForm.Error != "" {
		errorLines = min(3, max(1, height/4))
	}
	body, hits := f.render(width, max(1, height-errorLines), "task:field:")
	if errorLines > 0 {
		body += "\n" + strings.Join(fixedLines(ansi.Wrap(plain("오류: "+m.TaskForm.Error), width, ""), width, errorLines), "\n")
	}
	return body, hits
}

func (m *Model) submitTaskForm(action string) tea.Cmd {
	f := &m.TaskForm
	if f.Kind == "requests" && action == "save" {
		action = "accept"
	}
	if !slices.ContainsFunc(f.Form.Actions, func(a formAction) bool { return a.Key == action }) {
		f.Error = "이 화면에서 지원하는 동작을 선택하세요."
		return nil
	}
	if action == "cancel" {
		m.TaskForm = taskFormState{}
		m.Notice = "취소했습니다. 입력 초안은 유지됩니다."
		return nil
	}
	if f.Kind == "actions" && strings.HasPrefix(action, "command:") {
		target := f.JobID
		m.TaskForm = taskFormState{}
		if target != "" {
			m.SelectedID = target
		}
		return m.slash("/" + strings.TrimPrefix(action, "command:"))
	}
	if f.Uncertain {
		return nil
	}
	if f.Busy {
		return nil
	}
	value := f.Form.value
	if action == "reset" {
		if f.Kind == "filters" {
			m.Filter = ""
			m.ProjectFilter = ""
			m.AccountFilter = ""
			m.StateFilter = ""
			m.ManagedOnly = false
			m.ActiveOnly = false
			m.Selected = 0
			m.SelectedID = ""
		} else {
			m.LogFilter = logFilter{}
		}
		m.TaskForm = taskFormState{}
		m.Notice = "필터를 해제했습니다."
		return m.poll()
	}
	if action == "save" || action == "accept" {
		for _, field := range f.Form.Fields {
			if field.Required && strings.TrimSpace(value(field.Key)) == "" {
				f.Error = field.Label + "을 입력하세요."
				return nil
			}
		}
	}
	command := ""
	var args any
	switch f.Kind {
	case "new":
		kind := value("kind")
		if info, err := os.Stat(config.Expand(value("cwd"))); err != nil || !info.IsDir() {
			f.Error = "사용할 작업 폴더를 선택하세요."
			return nil
		}
		n := supervisor.NewArgs{Title: value("title"), Cwd: value("cwd"), Kind: kind, Account: value("account"), Model: value("model"), Effort: value("effort"), ParentID: value("parent_id"), ReadOnly: value("read_only") == "true", Prompt: value("prompt"), Goal: value("goal")}
		if kind == "goal" && strings.TrimSpace(n.Goal) == "" {
			f.Error = "지속 목표에는 목표가 필요합니다."
			return nil
		}
		if kind != "goal" && (n.Goal != "" || value("budget") != "") {
			f.Error = "목표와 예산을 사용하려면 작업 종류를 지속 목표로 선택하세요."
			return nil
		}
		if kind == "group" && n.Prompt != "" {
			f.Error = "그룹에는 첫 지시를 보낼 수 없습니다. 하위 작업이나 관리자 지시를 사용하세요."
			return nil
		}
		budget, err := positiveBudget(value("budget"))
		if err != nil {
			f.Error = err.Error()
			return nil
		}
		n.TokenBudget = budget
		if completion := strings.TrimSpace(value("completion")); completion != "" {
			if json.Unmarshal([]byte(completion), &n.CompletionCommand) != nil || len(n.CompletionCommand) == 0 || strings.TrimSpace(n.CompletionCommand[0]) == "" {
				f.Error = "완료 검사는 실행 파일과 인자의 배열로 입력하세요."
				return nil
			}
		}
		command, args = "new", n
		f.Open = value("open") == "true"
	case "send":
		command, args = "send", supervisor.JobArgs{ID: f.JobID, Text: value("text"), Steer: value("steer") == "true"}
	case "goal":
		budget, err := positiveBudget(value("budget"))
		if err != nil {
			f.Error = err.Error()
			return nil
		}
		command, args = "goal", supervisor.JobArgs{ID: f.JobID, Text: value("text"), TokenBudget: budget, GoalStatus: value("status")}
	case "unmanage":
		command, args = "unregister", supervisor.JobArgs{ID: f.JobID}
	case "shutdown":
		command = "shutdown"
	case "log-filter":
		days, _ := strconv.Atoi(value("days"))
		m.LogFilter = logFilter{Account: value("account"), Reason: value("reason"), Days: days}
		m.TaskForm = taskFormState{}
		m.Tab = "logs"
		m.Panel = "logs"
		m.Notice = "사용 기록 필터를 적용했습니다."
		m.Detail.GotoTop()
		return nil
	case "filters":
		m.Filter = value("search")
		m.ProjectFilter = value("project")
		m.AccountFilter = value("account")
		m.StateFilter = value("state")
		m.ManagedOnly = value("managed") == "true"
		m.ActiveOnly = value("active") == "true"
		m.TaskForm = taskFormState{}
		m.Selected = 0
		m.SelectedID = ""
		m.Tab = "tasks"
		m.ListOffset = 0
		m.Notice = "대화 필터를 적용했습니다."
		return m.poll()
	case "requests":
		return m.submitRequest(action)
	case "resolve-action":
		if value("action") == "" {
			f.Error = "확인할 작업이 없습니다."
			return nil
		}
		command, args = "resolve-action", map[string]string{"action_id": value("action"), "decision": value("decision")}
	default:
		return nil
	}
	f.Error = ""
	f.Busy = true
	f.Pending = command
	m.Notice = "요청을 접수 중입니다. 중복 전송하지 않습니다."
	return m.request(command, args)
}

func ambiguousUIError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, term := range []string{"outcome_unknown", "deadline exceeded", "eof", "timeout", "connection reset", "broken pipe", "response body"} {
		if strings.Contains(message, term) {
			return true
		}
	}
	return false
}

func positiveBudget(s string) (*int64, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n < 1 {
		return nil, fmt.Errorf("토큰 예산은 양의 정수로 입력하세요.")
	}
	return &n, nil
}

type requestParams struct {
	Command, Reason, Message, Mode string
	Permissions                    json.RawMessage `json:"permissions"`
	Questions                      []struct {
		ID, Header, Question string
		IsSecret             bool `json:"isSecret"`
		Options              []struct{ Label, Description string }
	} `json:"questions"`
	Schema struct {
		Properties map[string]schemaProperty `json:"properties"`
		Required   []string                  `json:"required"`
	} `json:"requestedSchema"`
}
type schemaProperty struct {
	Type, Title          string
	Enum                 []any
	Minimum, Maximum     *float64
	MinLength, MaxLength *int
}

func (m *Model) buildRequestForm(j jobs.Job, index int) {
	if index < 0 || index >= len(j.PendingRequests) {
		return
	}
	r := j.PendingRequests[index]
	var p requestParams
	if json.Unmarshal(r.Params, &p) != nil {
		m.Notice = "요청 내용을 읽을 수 없습니다. 공식 터미널에서 확인하세요."
		m.TaskForm = taskFormState{}
		return
	}
	f := &m.TaskForm
	f.Kind = "requests"
	f.JobID = j.ID
	f.RunID = j.RunID
	f.Title = j.Title
	f.Request = r
	f.RequestIndex = index
	fields := []formField{}
	actions := []formAction{{"cancel", "닫기"}, {"open", "공식 터미널"}}
	switch r.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		detail := p.Command
		if detail == "" {
			detail = p.Reason
		}
		if detail == "" {
			var formatted bytes.Buffer
			_ = json.Indent(&formatted, r.Params, "", "  ")
			detail = formatted.String()
		}
		fields = append(fields, formField{Key: "info", Label: "승인할 요청 · 원문", Kind: "info", Value: detail})
		actions = []formAction{{"accept", "이번 실행 허용"}, {"decline", "거절"}, {"cancel-request", "요청 취소"}, {"open", "공식 터미널"}, {"cancel", "닫기"}}
	case "item/permissions/requestApproval":
		fields = append(fields, formField{Key: "info", Label: "요청한 권한 · 이번 턴에만 적용", Kind: "info", Value: p.Reason + "\n" + string(p.Permissions)})
		actions = []formAction{{"accept", "이번 턴에 허용"}, {"decline", "거절"}, {"open", "공식 터미널"}, {"cancel", "닫기"}}
	case "item/tool/requestUserInput":
		for _, q := range p.Questions {
			if q.IsSecret {
				fields = []formField{{Key: "info", Label: "비공개 입력", Kind: "info", Value: "이 입력은 공식 Codex 터미널에서 응답하세요."}}
				break
			}
			help := []string{}
			for _, o := range q.Options {
				help = append(help, o.Label+" · "+o.Description)
			}
			label := q.Question
			if label == "" {
				label = q.Header
			}
			fields = append(fields, formField{Key: q.ID, Label: label, Kind: "text", Required: true, Help: strings.Join(help, "\n")})
		}
		if len(fields) == len(p.Questions) && len(fields) > 0 && fields[0].Key != "info" {
			actions = append([]formAction{{"accept", "답변 보내기"}}, actions...)
		}
	case "mcpServer/elicitation/request":
		fields = append(fields, formField{Key: "info", Label: "입력 요청", Kind: "info", Value: p.Message})
		supported := p.Mode == "form"
		keys := []string{}
		for key := range p.Schema.Properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			def := p.Schema.Properties[key]
			kind := "text"
			switch def.Type {
			case "string":
			case "number", "integer":
				kind = "number"
			case "boolean":
				kind = "toggle"
			default:
				supported = false
			}
			label := def.Title
			if label == "" {
				label = key
			}
			field := formField{Key: key, Label: label, Kind: kind, Required: slices.Contains(p.Schema.Required, key)}
			if kind == "toggle" {
				field.Value = "false"
			}
			if len(def.Enum) > 0 {
				field.Kind = "select"
				field.Options = []formOption{{"", "선택"}}
				for _, v := range def.Enum {
					value := fmt.Sprint(v)
					field.Options = append(field.Options, formOption{value, value})
				}
			}
			fields = append(fields, field)
		}
		if supported {
			actions = append([]formAction{{"accept", "입력 전달"}}, actions...)
		} else {
			fields = []formField{{Key: "info", Label: "입력 요청", Kind: "info", Value: p.Message + "\n이 형식은 공식 Codex 터미널에서 응답하세요."}}
		}
		actions = append(actions, formAction{"decline", "거절"}, formAction{"cancel-request", "요청 취소"})
	default:
		fields = append(fields, formField{Key: "info", Label: "요청", Kind: "info", Value: "이 요청은 공식 Codex 터미널에서 확인하세요."})
	}
	if len(j.PendingRequests) > 1 {
		actions = append(actions, formAction{"next", "다음 요청"})
	}
	f.Form = newForm(fmt.Sprintf("응답 요청 %d/%d · %s", index+1, len(j.PendingRequests), j.Title), fields, actions)
}

func (m *Model) submitRequest(action string) tea.Cmd {
	f := &m.TaskForm
	if action == "open" {
		return m.openID(f.JobID)
	}
	if action == "next" {
		for _, j := range m.Status.Jobs {
			if j.ID == f.JobID {
				m.buildRequestForm(j, (f.RequestIndex+1)%max(1, len(j.PendingRequests)))
				break
			}
		}
		return nil
	}
	var p requestParams
	if err := json.Unmarshal(f.Request.Params, &p); err != nil {
		f.Error = "요청을 새로 고치세요."
		return nil
	}
	var result any
	switch f.Request.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		decision := action
		if decision == "cancel-request" {
			decision = "cancel"
		}
		result = map[string]any{"decision": decision}
	case "item/permissions/requestApproval":
		permissions := json.RawMessage(`{}`)
		if action == "accept" {
			permissions = p.Permissions
		}
		result = map[string]any{"permissions": permissions, "scope": "turn"}
	case "item/tool/requestUserInput":
		answers := map[string]any{}
		for _, q := range p.Questions {
			answers[q.ID] = map[string]any{"answers": []string{f.Form.value(q.ID)}}
		}
		result = map[string]any{"answers": answers}
	case "mcpServer/elicitation/request":
		a := action
		if a == "cancel-request" {
			a = "cancel"
		}
		r := map[string]any{"action": a}
		if a == "accept" {
			content := map[string]any{}
			for key, def := range p.Schema.Properties {
				v := f.Form.value(key)
				if v == "" && !slices.Contains(p.Schema.Required, key) {
					continue
				}
				switch def.Type {
				case "boolean":
					content[key] = v == "true"
				case "integer", "number":
					n, err := strconv.ParseFloat(v, 64)
					if err != nil || def.Type == "integer" && n != float64(int64(n)) || def.Minimum != nil && n < *def.Minimum || def.Maximum != nil && n > *def.Maximum {
						f.Error = key + ": 값의 범위를 확인하세요."
						return nil
					}
					content[key] = n
				default:
					if def.MinLength != nil && len([]rune(v)) < *def.MinLength || def.MaxLength != nil && len([]rune(v)) > *def.MaxLength {
						f.Error = key + ": 글자 수를 확인하세요."
						return nil
					}
					content[key] = v
				}
			}
			r["content"] = content
		}
		result = r
	default:
		f.Error = "공식 Codex 터미널에서 응답하세요."
		return nil
	}
	raw, err := json.Marshal(result)
	if err == nil {
		err = cr.ValidateReply(f.Request, raw)
	}
	if err != nil {
		f.Error = err.Error()
		return nil
	}
	f.Busy = true
	f.Pending = "reply"
	return m.request("reply", supervisor.JobArgs{ID: f.JobID, RunID: f.RunID, RequestID: f.Request.ID, Result: raw})
}
