package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/panelcommand"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

// Test-only hook process: run the real parser/block response, replacing the OS
// opener with a receipt file. Never opens the user's GUI or uses credentials.
func TestPromptHookProcess(t *testing.T) {
	if os.Getenv("CODEXM_TEST_HOOK_PROCESS") != "1" {
		return
	}
	receipt := os.Getenv("CODEXM_TEST_HOOK_RECEIPT")
	err := panelcommand.Serve(context.Background(), os.Stdin, os.Stdout, "local-personal", func(_ context.Context, link string) error {
		if err := os.WriteFile(receipt, []byte(link), 0600); err != nil {
			return err
		}
		if _, err := os.Stat(receipt + ".fail"); err == nil {
			return errors.New("fixture opener unavailable")
		}
		return nil
	})
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestOfficialPromptCommandsPreventInference(t *testing.T) {
	if os.Getenv("CODEXM_PLUGIN_TEST") != "1" {
		t.Skip("opt-in isolated official hook integration")
	}
	i := fixture(t)
	binary, native := os.Getenv("CODEXM_BINARY"), os.Getenv("CODEXM_NATIVE_RUNTIME")
	if !filepath.IsAbs(binary) || !filepath.IsAbs(native) {
		t.Fatal("built and official binaries required")
	}
	i.Config.Runtime.CodexBinary = native
	if err := os.MkdirAll(i.Config.Runtime.SharedCodexHome, 0700); err != nil {
		t.Fatal(err)
	}
	if err := i.Config.Save(); err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(i.Home, "opened-link")
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	i.Self = filepath.Join(i.Home, "codexm-hook-fixture")
	launcher := "#!/bin/sh\nif [ \"$1\" = __prompt-command ]; then\n  export CODEXM_TEST_HOOK_PROCESS=1\n  exec " + cr.ShellQuote(testBinary) + " -test.run '^TestPromptHookProcess$'\nfi\nexec " + cr.ShellQuote(binary) + " \"$@\"\n"
	if err = config.AtomicWrite(i.Self, []byte(launcher), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = i.Install(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	env := pluginEnvironment(i.Home, i.Config.Runtime.SharedCodexHome)
	clean := env[:0]
	for _, value := range env {
		if !strings.HasPrefix(value, "OPENAI_API_KEY=") && !strings.HasPrefix(value, "CODEX_API_KEY=") {
			clean = append(clean, value)
		}
	}
	env = append(clean, "CODEXM_TEST_HOOK_RECEIPT="+receipt)
	brokenConfig := filepath.Join(i.Home, "broken-manager.toml")
	if err = os.WriteFile(brokenConfig, []byte("not valid toml ["), 0600); err != nil {
		t.Fatal(err)
	}
	ordinary := exec.CommandContext(ctx, binary, "--config", brokenConfig, "__prompt-command")
	ordinary.Env = env
	ordinary.Stdin = strings.NewReader(`{"hook_event_name":"UserPromptSubmit","prompt":"ordinary input"}`)
	if output, err := ordinary.CombinedOutput(); err != nil || string(output) != "{}\n" {
		t.Fatalf("manager configuration affected ordinary prompt: %v %q", err, output)
	}
	install := exec.CommandContext(ctx, native, "--no-daemon", "plugin", "add", "codexm@local-personal", "--json")
	install.Dir, install.Env = i.Home, env
	if output, err := install.CombinedOutput(); err != nil {
		t.Fatalf("fixture plugin install: %v %s", err, cr.SafeText(string(output)))
	}
	discovery, err := i.Discovery(ctx)
	if err != nil || discovery["prompt_command_hooks_discovered"] != 1 || discovery["prompt_command_hook_trust"] != "untrusted" {
		t.Fatal("untrusted hook discovery", discovery, err)
	}
	var inference atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "responses") {
			inference.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"isolated inference sentinel","type":"invalid_request_error"}}`))
	}))
	defer provider.Close()
	providerURL, _ := json.Marshal(provider.URL)
	// Trust exactly the reviewed test fixture inside its temporary HOME.
	// No production trust records or settings are read or changed. The CLI's
	// --dangerously-bypass-hook-trust is not forwarded by these app-server builds.
	key, _ := json.Marshal(discovery["prompt_command_hook_key"])
	hash, _ := json.Marshal(discovery["prompt_command_hook_hash"])
	fixtureConfig := filepath.Join(i.Config.Runtime.SharedCodexHome, "config.toml")
	data, err := os.ReadFile(fixtureConfig)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("\n[hooks.state."+string(key)+"]\ntrusted_hash="+string(hash)+"\n")...)
	if err = os.WriteFile(fixtureConfig, data, 0600); err != nil {
		t.Fatal(err)
	}
	trusted, err := i.Discovery(ctx)
	if err != nil || trusted["prompt_command_hook_trust"] != "trusted" {
		t.Fatal("fixture trust", trusted, err)
	}
	cmd := exec.CommandContext(ctx, native, "--no-daemon", "-c", "features.hooks=true", "-c", `model_provider="fixture"`, "-c", `model_providers.fixture.name="Isolated test"`, "-c", "model_providers.fixture.base_url="+string(providerURL), "-c", `model_providers.fixture.wire_api="responses"`, "-c", "model_providers.fixture.requires_openai_auth=false", "-c", "model_providers.fixture.supports_websockets=false", "-c", "model_providers.fixture.request_max_retries=0", "-c", "model_providers.fixture.stream_max_retries=0", "app-server", "--listen", "stdio://")
	cmd.Dir, cmd.Env = i.Home, env
	c, err := cr.StartStdio(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = c.Close()
		select {
		case <-c.ProcessDone:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-c.ProcessDone
		}
	}()
	var thread cr.ResumeResponse
	if err = c.Call(ctx, "thread/start", map[string]any{"cwd": i.Home, "sandbox": "read-only", "approvalPolicy": "never", "model": "gpt-5.4", "modelProvider": "fixture"}, &thread); err != nil {
		t.Fatal(err)
	}
	run := func(prompt string, extra map[string]any) bool {
		t.Helper()
		input := cr.TextInput(prompt)
		if extra != nil {
			input = append(input, extra)
		}
		var turn struct {
			Turn cr.Turn `json:"turn"`
		}
		if err := c.Call(ctx, "turn/start", map[string]any{"threadId": thread.Thread.ID, "input": input}, &turn); err != nil {
			t.Fatal(err)
		}
		hookSeen := false
		for {
			select {
			case event := <-c.Events:
				if event.Method == "hook/completed" && bytes.Contains(event.Params, []byte("userPromptSubmit")) {
					hookSeen = true
				}
				if event.Method == "turn/completed" {
					var end struct {
						Turn cr.Turn `json:"turn"`
					}
					_ = json.Unmarshal(event.Params, &end)
					if end.Turn.ID == turn.Turn.ID {
						return hookSeen
					}
				}
			case <-ctx.Done():
				t.Fatal("hook integration timed out")
			}
		}
	}
	cases := []struct {
		prompt string
		extra  map[string]any
	}{
		{"@codexm manage", nil},
		{"@codexm-manage", nil},
		{"[@codexm](plugin://codexm@local-personal) manage", map[string]any{"type": "mention", "name": "codexm", "path": "plugin://codexm@local-personal"}},
		{"$codexm-accounts", nil},
		{"$codexm-manage", map[string]any{"type": "skill", "name": "codexm-manage", "path": filepath.Join(i.Config.Runtime.SharedCodexHome, "plugins", "cache", "local-personal", "codexm", config.Version, "skills", "codexm-manage", "SKILL.md")}},
	}
	for _, tc := range cases {
		prompt := tc.prompt
		if !run(prompt, tc.extra) {
			t.Fatal("no prompt hook", prompt, "inference count", inference.Load())
		}
		if inference.Load() != 0 {
			t.Fatal("management command reached inference", prompt)
		}
		link, err := os.ReadFile(receipt)
		want, _ := panelcommand.Link("local-personal", panelcommand.Target(prompt, "local-personal"))
		if err != nil || string(link) != want {
			t.Fatal("wrong OS link request", string(link), err)
		}
	}
	if err = os.WriteFile(receipt+".fail", []byte("fail"), 0600); err != nil {
		t.Fatal(err)
	}
	if !run("@codexm manage", nil) || inference.Load() != 0 {
		t.Fatal("failed opener fell through to inference")
	}
	if !run("normal prompt for the local inference sentinel", nil) || inference.Load() != 1 {
		t.Fatal("ordinary prompt did not reach the local sentinel", inference.Load())
	}
	t.Log("five command forms including structured plugin/skill input and opener failure: 0 inference requests; ordinary prompt: 1 local sentinel request; live account and GUI calls: 0")
}
