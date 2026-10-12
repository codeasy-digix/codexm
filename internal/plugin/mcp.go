// Package plugin exposes only codexm management operations through local stdio.
// It does not serve a website, listen on TCP, or proxy arbitrary native RPC.
package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/service"
	"github.com/codeasy-digix/codexm/internal/supervisor"
	assets "github.com/codeasy-digix/codexm/plugin"
)

const ResourceURI = "ui://codexm/manager-v4.html"
const previousResourceURI = "ui://codexm/manager-v3.html"
const olderResourceURI = "ui://codexm/manager-v2.html"
const legacyResourceURI = "ui://codexm/manager-v1.html"
const ResourceMIME = "text/html;profile=mcp-app"

type MCP struct {
	Config config.Config
	Self   string
	Logins loginBroker
}
type loginBroker interface {
	Start(context.Context, string, string) (accounts.LoginStatus, error)
	Status(string) (accounts.LoginStatus, error)
	Cancel(context.Context, string) (accounts.LoginStatus, error)
	Close()
}
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (m MCP) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if m.Logins == nil {
		m.Logins = accounts.NewLoginBroker(ctx, m.Config)
	}
	defer m.Logins.Close()
	// Stdio may be idle when the host terminates the server. Closing its reader
	// unblocks Scan so cancellation can finish without another input line.
	finished := make(chan struct{})
	defer close(finished)
	if closer, ok := in.(io.ReadCloser); ok {
		go func() {
			select {
			case <-ctx.Done():
				_ = closer.Close()
			case <-finished:
			}
		}()
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 2*1024*1024)
	var writes sync.Mutex
	var active sync.WaitGroup
	semaphore := make(chan struct{}, 8)
	respond := func(id json.RawMessage, result any, err *rpcError) {
		reply := map[string]any{"jsonrpc": "2.0", "id": id}
		if err != nil {
			reply["error"] = err
		} else {
			reply["result"] = result
		}
		writes.Lock()
		defer writes.Unlock()
		_ = json.NewEncoder(out).Encode(reply)
	}
	for scanner.Scan() {
		var req rpcRequest
		if json.Unmarshal(scanner.Bytes(), &req) != nil || req.JSONRPC != "2.0" {
			respond(json.RawMessage("null"), nil, &rpcError{-32600, "invalid JSON-RPC request"})
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		select {
		case semaphore <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		active.Add(1)
		go func(req rpcRequest) {
			defer active.Done()
			defer func() { <-semaphore }()
			call, done := context.WithTimeout(ctx, 120*time.Second)
			defer done()
			result, err := m.handle(call, req)
			respond(req.ID, result, err)
		}(req)
	}
	// EOF ends ownership of login helpers even while another request is active.
	// Do not wait for a login's provider deadline or cancel unrelated RPCs merely
	// because a client closed stdin normally.
	m.Logins.Close()
	active.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return scanner.Err()
}

func HTML() (string, error) {
	html, _, e := uiResource()
	return html, e
}

func uiResource() (string, string, error) {
	b, e := assets.Files.ReadFile("ui/index.html")
	if e != nil {
		return "", "", e
	}
	css, e := assets.Files.ReadFile("ui/app.css")
	if e != nil {
		return "", "", e
	}
	js, e := assets.Files.ReadFile("ui/app.mjs")
	if e != nil {
		return "", "", e
	}
	html := strings.NewReplacer("/* CODEXM_CSS */", string(css), "/* CODEXM_JS */", string(js)).Replace(string(b))
	build := uiBuild(html)
	return strings.ReplaceAll(html, "__CODEXM_UI_BUILD__", build), build, nil
}

func object(properties map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func property(kind string) map[string]any    { return map[string]any{"type": kind} }
func choice(values ...string) map[string]any { return map[string]any{"type": "string", "enum": values} }

func toolList() []any {
	tools := []any{}
	add := func(name, desc string, readOnly bool, schema map[string]any, render bool) {
		meta := map[string]any{"ui": map[string]any{"visibility": []string{"app"}}}
		if render {
			meta = map[string]any{"ui": map[string]any{"resourceUri": ResourceURI}, "openai/outputTemplate": ResourceURI, "openai/ui": map[string]any{"entrypoints": []any{map[string]any{"type": "global"}, map[string]any{"type": "thread"}, map[string]any{"type": "settings"}}}}
			if name == "codexm_accounts_open" {
				meta["ui"].(map[string]any)["visibility"] = []string{"app"}
				meta["openai/ui"] = map[string]any{"entrypoints": []any{map[string]any{"type": "settings"}}}
			}
		}
		title := name
		if name == "codexm_open" {
			title = "codexm 관리"
		} else if name == "codexm_accounts_open" {
			title = "계정 관리"
		}
		tools = append(tools, map[string]any{"name": name, "title": title, "description": desc, "inputSchema": schema, "annotations": map[string]any{"readOnlyHint": readOnly, "destructiveHint": false, "openWorldHint": false}, "_meta": meta})
		tools[len(tools)-1].(map[string]any)["outputSchema"] = object(map[string]any{"ok": property("boolean")}, "ok")
	}
	add("codexm_open", "Open the local codexm account and task manager. Merely opening it never starts a model or changes a worker account.", true, object(map[string]any{}), true)
	add("codexm_accounts_open", "Open account management directly from plugin settings, including first account setup. Opening the panel does not sign in or start the manager.", true, object(map[string]any{}), true)
	add("codexm_snapshot", "Refresh account observations and task list without a model call. Cached quotas include their observation time.", true, object(map[string]any{}), false)
	add("codexm_account", "Explicit account sign-in buttons only. Start official browser or device sign-in, inspect its status or cancel this connection's request. Authentication details are private UI metadata; existing credentials change only after verification.", false, object(map[string]any{"command": choice("start", "status", "cancel"), "account": property("string"), "mode": choice("browser", "device"), "login_id": map[string]any{"type": "string", "pattern": "^[a-f0-9]{32}$"}}, "command"), false)
	add("codexm_ui_ready", "Record an optional panel bridge observation. Never starts supervision or a model, and does not prove the host displayed the UI.", false, object(map[string]any{"ui_build": map[string]any{"type": "string", "pattern": "^[a-f0-9]{64}$"}, "bridge": choice("mcp-app", "legacy"), "visible": property("boolean")}, "ui_build", "bridge", "visible"), false)
	add("codexm_read", "Read selected conversation, logs, settings or models. Account refresh uses authenticated quota APIs without generating a turn.", true, object(map[string]any{"command": choice("read", "details", "logs", "events", "accounts", "settings-edit", "models"), "id": property("string"), "thread_id": property("string"), "cursor": property("string"), "limit": property("integer")}, "command"), false)
	add("codexm_control", "Explicit user action on a selected managed conversation. Register preserves the existing chat and goal without generating a turn. New/send/resume may start work. Account switching obeys fallback restrictions.", false, object(map[string]any{"command": choice("new", "resume", "manage", "register", "unregister", "send", "pause", "stop", "resume-job", "complete", "reply", "switch-account"), "args": object(map[string]any{"id": property("string"), "thread_id": property("string"), "account": property("string"), "expected_account": property("string"), "operation_id": property("string"), "run_id": property("string"), "text": property("string"), "steer": property("boolean"), "manage": property("boolean"), "kind": choice("chat", "task", "goal"), "cwd": property("string"), "title": property("string"), "model": property("string"), "effort": property("string"), "prompt": property("string"), "goal": property("string"), "read_only": property("boolean"), "request_id": map[string]any{}, "result": map[string]any{}})}, "command", "args"), false)
	add("codexm_settings_save", "Save editable manager/account policy with the exact revision returned by settings-edit. Never changes credentials or runtime paths.", false, object(map[string]any{"revision": property("string"), "values": property("object")}, "revision", "values"), false)
	add("codexm_start", "Explicitly start the local manager service. At first setup a saved manager account must be selected and verified.", false, object(map[string]any{"manager_account": property("string")}), false)
	add("codexm_settings_read", "Read codexm's native plugin settings and management-panel action. Does not start the service or a model.", true, object(map[string]any{}), false)
	tools[len(tools)-1].(map[string]any)["outputSchema"] = object(map[string]any{"schema": property("object"), "values": property("object"), "layout": map[string]any{"type": "array", "items": property("object")}}, "schema", "values")
	add("codexm_settings_update", "Save only changed plugin settings; preserve other settings and existing session accounts.", false, object(map[string]any{"set": map[string]any{"type": "object", "properties": settingProperties(nil), "minProperties": 1, "additionalProperties": false}}, "set"), false)
	tools[len(tools)-1].(map[string]any)["outputSchema"] = object(map[string]any{"values": property("object")}, "values")
	return tools
}

func settingsCapability() map[string]any {
	return map[string]any{"openai/settings": map[string]any{"readTool": "codexm_settings_read", "updateTool": "codexm_settings_update"}}
}

func (m MCP) handle(ctx context.Context, req rpcRequest) (any, *rpcError) {
	bad := func(e error) (any, *rpcError) { return nil, &rpcError{-32602, cr.SafeText(e.Error())} }
	switch req.Method {
	case "initialize":
		_ = m.observePanel("mcp_initialized", nil)
		return map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}, "resources": map[string]any{}, "extensions": settingsCapability(), "experimental": settingsCapability()}, "serverInfo": map[string]any{"name": "codexm", "title": "codexm", "version": config.Version}, "instructions": "Open codexm from the sidebar, conversation panel or plugin settings. Routine UI actions call tools directly and need no manager model turn."}, nil
	case "server/discover":
		return map[string]any{"resultType": "complete", "supportedVersions": []string{"2025-11-25"}, "_meta": map[string]any{"io.modelcontextprotocol/serverInfo": map[string]any{"name": "codexm", "title": "codexm", "version": config.Version}}, "capabilities": map[string]any{"tools": map[string]any{}, "resources": map[string]any{}, "extensions": settingsCapability()}}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolList()}, nil
	case "resources/list":
		return map[string]any{"resources": []any{map[string]any{"uri": ResourceURI, "name": "codexm", "mimeType": ResourceMIME, "description": "Local account and task management"}}}, nil
	case "resources/read":
		var p struct {
			URI string `json:"uri"`
		}
		if e := json.Unmarshal(req.Params, &p); e != nil {
			return bad(e)
		}
		if p.URI != ResourceURI && p.URI != previousResourceURI && p.URI != olderResourceURI && p.URI != legacyResourceURI {
			return bad(errors.New("unknown UI resource"))
		}
		html, e := HTML()
		if e != nil {
			return bad(e)
		}
		meta := map[string]any{"ui": map[string]any{"prefersBorder": false, "csp": map[string]any{"connectDomains": []string{}, "resourceDomains": []string{}}}, "openai/ui": map[string]any{"availableDisplayModes": []string{"inline", "fullscreen"}}, "openai/widgetDescription": "Manage local Codex accounts, task handoffs, quota observations and settings."}
		_ = m.observePanel("resource_served", nil)
		return map[string]any{"contents": []any{map[string]any{"uri": p.URI, "mimeType": ResourceMIME, "text": html, "_meta": meta}}}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if e := json.Unmarshal(req.Params, &p); e != nil {
			return bad(e)
		}
		if len(p.Arguments) == 0 {
			p.Arguments = json.RawMessage("{}")
		}
		result, e := m.call(ctx, p.Name, p.Arguments)
		if e != nil {
			if p.Name == "codexm_account" {
				// Even unexpected implementation errors must not inject provider
				// URLs, one-time codes or account identifiers into model context.
				return map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "계정 로그인 요청을 처리하지 못했습니다. 별칭과 진행 중인 로그인을 확인하고 다시 시도하세요."}}}, nil
			}
			return map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": cr.SafeText(e.Error())}}}, nil
		}
		if p.Name == "codexm_settings_read" || p.Name == "codexm_settings_update" {
			return map[string]any{"content": []any{}, "structuredContent": result}, nil
		}
		// Conversation contents are for the user-facing iframe, not injected into
		// the surrounding model context on every refresh.
		message := "codexm 관리 데이터를 조회했습니다."
		meta := map[string]any{"codexm_data": result}
		if p.Name == "codexm_open" || p.Name == "codexm_accounts_open" {
			message = "codexm 관리 화면 열기를 요청했습니다. 실제 화면 표시는 호스트에서 확인해야 합니다."
			// Some hosts resolve the completed result before their tool catalog.
			meta["ui"] = map[string]any{"resourceUri": ResourceURI}
			meta["openai/outputTemplate"] = ResourceURI
		} else if p.Name == "codexm_account" {
			message = "계정 로그인 요청을 처리했습니다. 상태는 계정 관리 화면에서 확인하세요."
		}
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": message}}, "structuredContent": map[string]any{"ok": true}, "_meta": meta}, nil
	default:
		return nil, &rpcError{-32601, "method not found"}
	}
}

func (m MCP) call(ctx context.Context, name string, args json.RawMessage) (any, error) {
	if name == "codexm_ui_ready" {
		return m.panelReady(args)
	}
	if name == "codexm_settings_read" {
		return m.readPluginSettings()
	}
	if name == "codexm_settings_update" {
		return m.updatePluginSettings(ctx, args)
	}
	if name == "codexm_account" {
		return m.accountLogin(ctx, args)
	}
	if name == "codexm_open" || name == "codexm_accounts_open" || name == "codexm_snapshot" {
		if name != "codexm_snapshot" {
			_ = m.observePanel("open_requested", nil)
		}
		snapshot, e := m.snapshot(ctx)
		if e == nil && name == "codexm_accounts_open" {
			snapshot.(map[string]any)["initial_view"] = "accounts"
		}
		return snapshot, e
	}
	if name == "codexm_start" {
		var a struct {
			Account string `json:"manager_account"`
		}
		if e := json.Unmarshal(args, &a); e != nil {
			return nil, e
		}
		c, e := config.Load(m.Config.Path)
		if e != nil {
			return nil, e
		}
		if c.Manager.Account == "" {
			if !config.Nickname.MatchString(a.Account) {
				return nil, errors.New("select a saved manager account first")
			}
			r, e := cr.Resolve(ctx, c)
			if e != nil {
				return nil, e
			}
			snap := accounts.New(r).Read(ctx, a.Account)
			if !snap.Authenticated || snap.Error != "" {
				return nil, errors.New("manager login could not be verified")
			}
			c, _, e = config.Update(c.Path, "", func(c *config.Config) error {
				if c.Manager.Account != "" {
					return errors.New("manager was selected elsewhere; refresh")
				}
				c.Manager.Account = a.Account
				return nil
			})
			if e != nil {
				return nil, e
			}
		}
		if supervisor.Ping(ctx, c) != nil {
			svc := service.Manager{Config: c, Self: m.Self}
			if svc.Registered() {
				e = svc.Start(ctx)
			} else {
				e = svc.Install(ctx, true)
			}
			if e != nil {
				return nil, e
			}
			if e = service.WaitReady(ctx, func(ctx context.Context) error { return supervisor.Ping(ctx, c) }); e != nil {
				return nil, e
			}
		}
		return m.snapshot(ctx)
	}
	var call struct {
		Command string          `json:"command"`
		Args    json.RawMessage `json:"args"`
	}
	if e := json.Unmarshal(args, &call); e != nil {
		return nil, e
	}
	command, payload := call.Command, args
	switch name {
	case "codexm_read":
		if !contains([]string{"read", "details", "logs", "events", "accounts", "settings-edit", "models"}, command) {
			return nil, errors.New("unsupported read command")
		}
	case "codexm_control":
		if !contains([]string{"new", "resume", "manage", "register", "unregister", "send", "pause", "stop", "resume-job", "complete", "reply", "switch-account"}, command) {
			return nil, errors.New("unsupported management command")
		}
		var keys map[string]json.RawMessage
		if e := json.Unmarshal(call.Args, &keys); e != nil {
			return nil, e
		}
		// Enforce the narrower MCP surface even in clients that do not validate schemas.
		for k := range keys {
			if !contains([]string{"id", "thread_id", "account", "expected_account", "operation_id", "run_id", "text", "steer", "manage", "kind", "cwd", "title", "model", "effort", "prompt", "goal", "read_only", "request_id", "result"}, k) {
				return nil, fmt.Errorf("unsupported control field %q", k)
			}
		}
		payload = call.Args
	case "codexm_settings_save":
		command = "settings-save"
	default:
		return nil, errors.New("unknown codexm tool")
	}
	var result json.RawMessage
	if e := supervisor.Request(ctx, m.Config, command, payload, &result); e != nil {
		return nil, e
	}
	return result, nil
}
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (m MCP) accountLogin(ctx context.Context, raw json.RawMessage) (any, error) {
	if m.Logins == nil {
		return nil, errors.New("account sign-in connection is unavailable")
	}
	var args struct {
		Command string `json:"command"`
		Account string `json:"account"`
		Mode    string `json:"mode"`
		LoginID string `json:"login_id"`
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, errors.New("invalid account sign-in request")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&args) != nil {
		return nil, errors.New("invalid account sign-in request")
	}
	if args.Command == "start" {
		if len(fields) != 3 || fields["account"] == nil || fields["mode"] == nil || args.LoginID != "" || !config.Nickname.MatchString(args.Account) || args.Account == "default" || (args.Mode != "browser" && args.Mode != "device") {
			return nil, errors.New("choose an account nickname and sign-in mode")
		}
		return m.Logins.Start(ctx, args.Account, args.Mode)
	}
	if (args.Command != "status" && args.Command != "cancel") || len(fields) != 2 || fields["login_id"] == nil || args.Account != "" || args.Mode != "" || len(args.LoginID) != 32 || strings.Trim(args.LoginID, "0123456789abcdef") != "" {
		return nil, errors.New("select this connection's sign-in request")
	}
	if args.Command == "cancel" {
		return m.Logins.Cancel(ctx, args.LoginID)
	}
	return m.Logins.Status(args.LoginID)
}

func (m MCP) snapshot(ctx context.Context) (any, error) {
	c, e := config.Load(m.Config.Path)
	if e != nil {
		return nil, e
	}
	names, e := accounts.List(c)
	if e != nil && c.Manager.Account != "" {
		return nil, e
	}
	if names == nil {
		names = []string{}
	}
	snapshot := map[string]any{"connected": false, "setup_required": c.Manager.Account == "", "account_names": names, "login_available": m.Logins != nil, "manager_account": c.Manager.Account, "default_worker_account": c.DefaultWorker(), "priority": c.WorkerAccounts.Priority, "fallback_account": c.WorkerAccounts.FallbackAccount, "preferred_recheck_seconds": c.WorkerAccounts.PreferredRecheckSeconds, "at": time.Now()}
	if supervisor.Ping(ctx, c) != nil {
		snapshot["message"] = "관리 서비스가 연결되지 않았습니다. 계정을 준비한 뒤 시작 버튼으로 연결하세요."
		return snapshot, nil
	}
	var capability struct {
		API int `json:"manager_api"`
	}
	if e = supervisor.Request(ctx, c, "ping", nil, &capability); e != nil {
		return nil, e
	}
	var st supervisor.Status
	if e = supervisor.Request(ctx, c, "status", nil, &st); e != nil {
		return nil, e
	}
	var list catalog.List
	if e = supervisor.Request(ctx, c, "sessions", nil, &list); e != nil {
		return nil, e
	}
	for i := range st.Jobs {
		st.Jobs[i].LastInput = ""
		st.Jobs[i].LastOutput = ""
		st.Jobs[i].PendingRequests = nil
		if st.Jobs[i].Goal != nil {
			st.Jobs[i].Goal.Objective = ""
		}
	}
	st.ManagerDecision = nil
	snapshot["connected"], snapshot["manager_api"], snapshot["status"], snapshot["catalog"] = true, capability.API, st, list
	return snapshot, nil
}
