package nativecli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

// An isolated official app-server and a local model stub for web protocol tests.
type guiControlFixture struct {
	t              *testing.T
	ctx            context.Context
	c              config.Config
	binary         string
	cmd            *exec.Cmd
	client         *cr.Client
	socket         string
	active         chan struct{}
	release        chan struct{}
	releaseForeign chan struct{}
	foreignUpdate  chan struct{}
	once           sync.Once
	mu             sync.Mutex
	bodies         []string
	keys           []string // Synthetic local-provider credentials only.
	responseText   string
	commandFixture bool
	quotaFailure   chan struct{}
	quotaAccount   string
	quotaWindow    string
}

func newGUIControlFixture(t *testing.T, transport string) *guiControlFixture {
	return newGUIControlFixtureDuration(t, transport, 60*time.Second, nil)
}

func newGUIControlFixtureDuration(t *testing.T, transport string, timeout time.Duration, configure func(*guiControlFixture)) *guiControlFixture {
	t.Helper()
	if os.Getenv("CODEXM_GUI_CONTROL_TEST") != "1" {
		t.Skip("opt-in official web protocol tests")
	}
	binary := os.Getenv("CODEXM_NATIVE_RUNTIME")
	if !filepath.IsAbs(binary) {
		t.Fatal("absolute official Codex binary required")
	}
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Fatal("lsof is required to verify that the original writer is preserved")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	f := &guiControlFixture{t: t, ctx: ctx, binary: binary, active: make(chan struct{}), release: make(chan struct{}), releaseForeign: make(chan struct{})}
	if filepath.IsAbs(os.Getenv("CODEXM_WEB_BROWSER_FIXTURE")) {
		f.foreignUpdate = make(chan struct{})
	}
	var activeOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/responses") {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, string(body))
		f.keys = append(f.keys, r.Header.Get("Authorization"))
		responseText := f.responseText
		commandFixture := f.commandFixture
		quotaFailure := f.quotaFailure
		quotaAccount := f.quotaAccount
		f.mu.Unlock()
		if strings.Contains(string(body), "WEB_COMMANDS_BROWSER_PROPOSE_PLAN") {
			responseText = "<proposed_plan>\n## 브라우저 계획 검증\n\n계획 본문입니다.\n</proposed_plan>"
		} else if strings.Contains(string(body), `"overall_correctness"`) {
			responseText = `{"findings":[],"overall_correctness":"patch is correct","overall_explanation":"Synthetic browser review completed","overall_confidence_score":0.95}`
		}
		if quotaFailure != nil && strings.Contains(string(body), "WEB_FIXTURE_QUOTA_429") && fixtureLoginName(r.Header.Get("Authorization")) == quotaAccount {
			select {
			case <-quotaFailure:
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprint(w, "{\"error\":{\"type\":\"usage_limit_reached\",\"message\":\"Fixture account exhausted\",\"resets_in_seconds\":1800}}")
			case <-r.Context().Done():
			}
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"fixture-response\"}}\n\n")
		w.(http.Flusher).Flush()
		if strings.Contains(string(body), "WEB_QUESTION_FIXTURE") {
			if !strings.Contains(string(body), "fixture-web-question") {
				questions := []map[string]any{
					{"id": "approach", "header": "진행 방식", "question": "어떤 방식으로 진행할까요?", "isOther": true,
						"options": []map[string]string{{"label": "기본 방식", "description": "기본 순서대로 진행합니다."}, {"label": "빠른 방식", "description": "먼저 결과를 확인합니다."}}},
					{"id": "confirmation", "header": "확인 값", "question": "확인 값을 입력해 주세요.", "isSecret": true,
						"options": []map[string]string{{"label": "건너뛰기", "description": "값을 생략합니다."}, {"label": "중단", "description": "검증을 중단합니다."}}},
				}
				args, _ := json.Marshal(map[string]any{"questions": questions})
				writeFixtureStreamEvent(w, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": map[string]any{
					"type": "function_call", "name": "request_user_input", "call_id": "fixture-web-question", "arguments": string(args),
				}})
				fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"fixture-response\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
				return
			}
			if strings.Contains(string(body), "fixture-confirmed-answer") &&
				(strings.Contains(string(body), "fixture-own-choice") || strings.Contains(string(body), "기본 방식")) {
				responseText = "웹에서 보낸 답변을 받았습니다."
			}
		}
		if commandFixture && strings.Contains(string(body), "WEB_COMMAND_FIXTURE") && !strings.Contains(string(body), "web-fixture-command") {
			var request struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			}
			_ = json.Unmarshal(body, &request)
			name := "shell_command"
			arguments := map[string]any{"command": "printf '%050000d' 1", "workdir": home, "timeout_ms": 5000}
			for _, tool := range request.Tools {
				if tool.Name == "exec_command" {
					name = "exec_command"
					arguments = map[string]any{"cmd": "printf '%050000d' 1", "workdir": home, "max_output_tokens": 20000}
					break
				}
				if tool.Name == "shell" {
					name = "shell"
					arguments = map[string]any{"command": []string{"/bin/sh", "-c", "printf '%050000d' 1"}, "workdir": home, "timeout_ms": 5000}
				}
			}
			args, _ := json.Marshal(arguments)
			item, _ := json.Marshal(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": map[string]any{"type": "function_call", "name": name, "call_id": "web-fixture-command", "arguments": string(args)}})
			fmt.Fprintf(w, "data: %s\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"fixture-response\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n", item)
			return
		}
		streaming := strings.Contains(string(body), "GUI_FIXTURE_ACTIVE_BROWSER")
		foreignProgress := strings.Contains(string(body), "GUI_FIXTURE_ACTIVE_FOREIGN_BROWSER")
		outputIndex := 0
		if streaming || foreignProgress {
			messageID := "fixture-message"
			phase, text := "final_answer", "완료 전에도 보이는 답변"
			if foreignProgress {
				messageID, phase, text = "fixture-progress", "commentary", "외부 작업이 기록한 진행 안내"
			}
			writeFixtureStreamEvent(w, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{
				"id": messageID, "type": "message", "role": "assistant", "phase": phase, "content": []any{},
			}})
			writeFixtureStreamEvent(w, map[string]any{"type": "response.output_text.delta", "output_index": 0, "content_index": 0, "item_id": messageID, "delta": text})
			if foreignProgress {
				writeFixtureStreamEvent(w, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": map[string]any{
					"id": messageID, "type": "message", "role": "assistant", "phase": phase, "content": []map[string]string{{"type": "output_text", "text": text}},
				}})
				outputIndex = 1
			} else {
				responseText = text + "\n\n" + responseText
			}
			w.(http.Flusher).Flush()
		}
		if strings.Contains(string(body), "GUI_FIXTURE_ACTIVE") {
			activeOnce.Do(func() { close(f.active) })
			release := f.release
			if foreignProgress {
				release = f.releaseForeign
				if f.foreignUpdate != nil {
					select {
					case <-f.foreignUpdate:
						writeFixtureStreamEvent(w, map[string]any{"type": "response.output_item.done", "output_index": outputIndex, "item": map[string]any{
							"id": "fixture-progress-update", "type": "message", "role": "assistant", "phase": "commentary",
							"content": []map[string]string{{"type": "output_text", "text": "작업 도중 새로 저장된 진행 안내"}},
						}})
						outputIndex++
						w.(http.Flusher).Flush()
					case <-f.releaseForeign:
					case <-r.Context().Done():
						return
					}
				}
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		if responseText == "" {
			responseText = "fixture complete"
		}
		writeFixtureStreamEvent(w, map[string]any{"type": "response.output_item.done", "output_index": outputIndex, "item": map[string]any{"id": "fixture-message", "type": "message", "role": "assistant", "content": []map[string]string{{"type": "output_text", "text": responseText}}}})
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"fixture-response\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
	f.c = config.Defaults()
	f.c.Runtime.SharedCodexHome = filepath.Join(home, "shared")
	f.c.Runtime.AccountsDir = filepath.Join(home, "accounts")
	f.c.Storage.StateDir = filepath.Join(home, "state")
	f.c.Runtime.CodexBinary = binary
	if err := os.MkdirAll(f.c.Runtime.SharedCodexHome, 0700); err != nil {
		t.Fatal(err)
	}
	settings := fmt.Sprintf("model='fixture'\nmodel_provider='fixture'\ncli_auth_credentials_store='file'\ncheck_for_update_on_startup=false\napproval_policy='never'\n[model_providers.fixture]\nname='fixture'\nbase_url=%q\nwire_api='responses'\nenv_key='CODEXM_GUI_FIXTURE_KEY'\nrequires_openai_auth=false\n[projects.%q]\ntrust_level='trusted'\n", server.URL, home)
	settings += "\n[features]\nplugins=false\napps=false\nhooks=false\nskip_host_skill_discovery=true\n"
	if err := os.WriteFile(filepath.Join(f.c.Runtime.SharedCodexHome, "config.toml"), []byte(settings), 0600); err != nil {
		t.Fatal(err)
	}
	if configure != nil {
		configure(f)
	}
	endpoint := "stdio://"
	if transport == "unix" {
		short, err := os.MkdirTemp("/tmp", "codexm-gui-rpc-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(short) })
		f.socket = filepath.Join(short, "control.sock")
		endpoint = "unix://" + f.socket
	}
	f.cmd = exec.CommandContext(ctx, binary, "--no-daemon", "--disable", "plugins", "--disable", "apps", "--disable", "hooks", "--enable", "skip_host_skill_discovery", "app-server", "--listen", endpoint)
	f.cmd.Env, f.cmd.Dir = environment(f.c, "default"), home
	f.cmd.Env = append(f.cmd.Env, "CODEXM_GUI_FIXTURE_KEY=fixture-owner-a")
	var err error
	var processDone <-chan struct{}
	if transport == "stdio" {
		f.client, err = cr.StartStdio(ctx, f.cmd)
		if f.client != nil {
			processDone = f.client.ProcessDone
		}
	} else {
		err = f.cmd.Start()
		if err == nil {
			done := make(chan struct{})
			processDone = done
			go func() { _ = f.cmd.Wait(); close(done) }()
			for until := time.Now().Add(8 * time.Second); time.Now().Before(until); {
				f.client, err = cr.DialUnix(ctx, f.socket)
				if err == nil {
					break
				}
				time.Sleep(40 * time.Millisecond)
			}
		}
	}
	t.Cleanup(func() {
		f.once.Do(func() { close(f.release) })
		if f.client != nil {
			f.client.Close()
		}
		if f.cmd.Process != nil {
			cancel() // This fixture's process only; never a user's GUI/server.
			if processDone != nil {
				<-processDone
			}
		}
		server.Close()
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func writeFixtureStreamEvent(w http.ResponseWriter, event map[string]any) {
	data, _ := json.Marshal(event)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func (f *guiControlFixture) thread() string {
	f.t.Helper()
	var r struct{ Thread cr.Thread }
	if err := f.client.Call(f.ctx, "thread/start", map[string]any{"cwd": f.cmd.Dir, "sandbox": "read-only", "approvalPolicy": "never"}, &r); err != nil {
		f.t.Fatal(err)
	}
	return r.Thread.ID
}

func (f *guiControlFixture) start(id, text string) string {
	f.t.Helper()
	var r struct{ Turn cr.Turn }
	if err := f.client.Call(f.ctx, "turn/start", map[string]any{"threadId": id, "input": cr.TextInput(text)}, &r); err != nil {
		f.t.Fatal(err)
	}
	return r.Turn.ID
}

func (f *guiControlFixture) completed(id string, count int) cr.Thread {
	f.t.Helper()
	for until := time.Now().Add(15 * time.Second); time.Now().Before(until); {
		thread, _, err := f.client.Recent(f.ctx, id, 30, "")
		done := 0
		for _, turn := range thread.Turns {
			if turn.Status == "completed" {
				done++
			}
		}
		if err == nil && done == count && len(thread.Turns) == count {
			return thread
		}
		time.Sleep(50 * time.Millisecond)
	}
	f.t.Fatalf("expected %d completed turns", count)
	return cr.Thread{}
}

func (f *guiControlFixture) requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies)
}

func (f *guiControlFixture) assertOwner(key, id, other string) {
	f.t.Helper()
	current, err := cr.NativeProcessIdentity(f.cmd.Process.Pid)
	if err != nil || current != key || !cr.WriterBusy(f.c.Runtime.SharedCodexHome, id) || !cr.WriterBusy(f.c.Runtime.SharedCodexHome, other) {
		f.t.Fatal("original server or one of its writer locks was changed", err)
	}
	for _, thread := range []string{id, other} {
		writers, err := writerProcesses(f.ctx, f.c.Runtime.SharedCodexHome, thread)
		if err != nil || len(writers) != 1 || writers[0] != f.cmd.Process.Pid {
			f.t.Fatal("writer ownership moved away from the original server", err)
		}
	}
}
