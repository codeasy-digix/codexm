package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

func fixture(t *testing.T) Installer {
	t.Helper()
	root := t.TempDir()
	c := config.Defaults()
	c.Path = filepath.Join(root, "config.toml")
	c.Storage.StateDir = filepath.Join(root, "state")
	c.Runtime.AccountsDir = filepath.Join(root, "accounts")
	c.Runtime.SharedCodexHome = filepath.Join(root, "codex")
	c.Runtime.CodexBinary = "/bin/echo" // Locate only; this test must not execute Codex.
	if e := c.Save(); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(c.Runtime.AccountsDir, 0700); e != nil {
		t.Fatal(e)
	}
	return Installer{Config: c, Self: filepath.Join(root, "codexm"), Home: root}
}

func TestInstallPreservesPersonalMarketplaceAndUninstallsOnlyOwnedEntry(t *testing.T) {
	i := fixture(t)
	original := []byte(`{"name":"my-plugins","custom":{"kept":true},"plugins":[{"name":"one","source":"./one"},{"name":"two","source":{"source":"local","path":"./two"}}]}`)
	if e := config.AtomicWrite(i.catalog(), original, 0600); e != nil {
		t.Fatal(e)
	}
	r, e := i.Install()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = i.Install(); e != nil {
		t.Fatal("idempotent install", e)
	}
	var doc map[string]json.RawMessage
	b, _ := os.ReadFile(i.catalog())
	if e = json.Unmarshal(b, &doc); e != nil {
		t.Fatal(e)
	}
	var entries []json.RawMessage
	_ = json.Unmarshal(doc["plugins"], &entries)
	if len(entries) != 3 || string(doc["custom"]) == "" || string(doc["name"]) != `"my-plugins"` {
		t.Fatal("other registrations changed")
	}
	for _, name := range []string{"mcp.json", ".mcp.json"} {
		b, e := os.ReadFile(filepath.Join(r.Root, name))
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(b), "auth.json") || strings.Contains(string(b), "OPENAI_API_KEY") || strings.Contains(string(b), "http://") {
			t.Fatal("unexpected credential or network transport")
		}
		var v struct {
			Servers map[string]struct {
				Command string
				Args    []string
			} `json:"mcpServers"`
		}
		if e = json.Unmarshal(b, &v); e != nil {
			t.Fatal(e)
		}
		wantCommand := i.Self
		if name == "mcp.json" {
			wantCommand = "./bin/codexm"
		}
		if v.Servers["codexm"].Command != wantCommand || strings.Join(v.Servers["codexm"].Args, "|") != "--config|"+i.Config.Path+"|plugin|mcp" {
			t.Fatal("wrong MCP invocation")
		}
	}
	if st, e := os.Stat(filepath.Join(r.Root, "bin", "codexm")); e != nil || st.Mode().Perm() != 0700 {
		t.Fatal("portable launcher is not executable")
	}
	if _, e := os.Stat(filepath.Join(r.Root, "skills", "codexm", "SKILL.md")); e != nil {
		t.Fatal("registration skill not packaged", e)
	}
	if e = i.Uninstall(); e != nil {
		t.Fatal(e)
	}
	b, _ = os.ReadFile(i.catalog())
	_ = json.Unmarshal(b, &doc)
	_ = json.Unmarshal(doc["plugins"], &entries)
	if len(entries) != 2 || !bytes.Contains(doc["plugins"], []byte(`"one"`)) || !bytes.Contains(doc["plugins"], []byte(`"two"`)) {
		t.Fatal("uninstall changed unrelated entries")
	}
	if _, e = os.Stat(r.Root); e != nil {
		t.Fatal("recoverable assets removed")
	}
}

func TestInstallRefusesUnownedEntryAndChangedOwnedEntry(t *testing.T) {
	i := fixture(t)
	original := []byte(`{"plugins":[{"name":"codexm","source":"./someone-else"}]}`)
	if e := config.AtomicWrite(i.catalog(), original, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := i.Install(); e == nil {
		t.Fatal("unowned entry overwritten")
	}
	b, _ := os.ReadFile(i.catalog())
	if !bytes.Equal(b, original) {
		t.Fatal("failed install changed catalog")
	}
	if e := os.Remove(i.catalog()); e != nil {
		t.Fatal(e)
	}
	if _, e := i.Install(); e != nil {
		t.Fatal(e)
	}
	if e := config.AtomicWrite(i.catalog(), original, 0600); e != nil {
		t.Fatal(e)
	}
	if e := i.Uninstall(); e == nil {
		t.Fatal("externally edited entry removed")
	}
	b, _ = os.ReadFile(i.catalog())
	if !bytes.Equal(b, original) {
		t.Fatal("failed uninstall changed catalog")
	}
}

func TestMCPStdioDiscoveryAndOfflineUIHaveNoSideEffects(t *testing.T) {
	i := fixture(t)
	m := MCP{Config: i.Config, Self: i.Self}
	var out bytes.Buffer
	requests := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"ui://codexm/manager-v2.html"}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"codexm_open","arguments":{}}}
`
	if e := m.Serve(context.Background(), strings.NewReader(requests), &out); e != nil {
		t.Fatal(e)
	}
	results := map[int]map[string]json.RawMessage{}
	s := bufio.NewScanner(&out)
	s.Buffer(make([]byte, 4096), 2*1024*1024)
	for s.Scan() {
		var r struct {
			ID     int
			Result map[string]json.RawMessage
			Error  any
		}
		if e := json.Unmarshal(s.Bytes(), &r); e != nil {
			t.Fatal(e)
		}
		if r.Error != nil {
			t.Fatal(r.Error)
		}
		results[r.ID] = r.Result
	}
	if s.Err() != nil || len(results) != 4 {
		t.Fatal("missing protocol responses", s.Err())
	}
	if !bytes.Contains(results[2]["tools"], []byte(`"visibility":["app"]`)) || !bytes.Contains(results[2]["tools"], []byte(`"openai/ui"`)) {
		t.Fatal("UI tool discovery missing")
	}
	if bytes.Contains(results[3]["contents"], []byte("CODEXM_JS")) || !bytes.Contains(results[3]["contents"], []byte("ui/initialize")) {
		t.Fatal("UI resource was not bundled")
	}
	if bytes.Contains(results[4]["structuredContent"], []byte("accounts")) || !bytes.Contains(results[4]["_meta"], []byte(`"connected":false`)) {
		t.Fatal("UI data boundary failed")
	}
	if _, e := os.Stat(filepath.Join(i.Config.StateDir(), "services")); !os.IsNotExist(e) {
		t.Fatal("opening UI installed service")
	}
	for _, args := range []string{`{"command":"shutdown","args":{}}`, `{"command":"new","args":{"completion_command":"arbitrary-shell"}}`} {
		if _, e := m.call(context.Background(), "codexm_control", json.RawMessage(args)); e == nil {
			t.Fatal("unexposed operation accepted")
		}
	}
}

func TestMCPCancellationUnblocksIdleStdio(t *testing.T) {
	i := fixture(t)
	r, w := io.Pipe()
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- (MCP{Config: i.Config}).Serve(ctx, r, io.Discard) }()
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("idle MCP ignored cancellation")
	}
}

func TestSnapshotRedactsConversationAndControlRetainsTarget(t *testing.T) {
	i := fixture(t)
	socket := supervisor.Socket(i.Config)
	if e := os.MkdirAll(filepath.Dir(socket), 0700); e != nil {
		t.Fatal(e)
	}
	l, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	var received supervisor.Call
	s := http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c supervisor.Call
		if e := json.NewDecoder(r.Body).Decode(&c); e != nil {
			t.Error(e)
			return
		}
		var result any = map[string]any{}
		switch c.Command {
		case "ping":
			result = map[string]int{"manager_api": 1}
		case "status":
			result = json.RawMessage(`{"jobs":[{"id":"job-a","last_input":"PRIVATE_INPUT","last_output":"PRIVATE_OUTPUT","pending_requests":[{"id":"PRIVATE_REQUEST"}],"goal":{"objective":"PRIVATE_OBJECTIVE"}}]}`)
		case "sessions":
			result = map[string]any{"sessions": []any{}}
		case "switch-account":
			received = c
			result = map[string]bool{"accepted": true}
		default:
			t.Errorf("unexpected RPC: %s", c.Command)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": result})
	})}
	go s.Serve(l)
	defer s.Close()
	m := MCP{Config: i.Config}
	v, e := m.snapshot(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(v)
	if bytes.Contains(b, []byte("PRIVATE_")) {
		t.Fatal("snapshot leaked conversation content")
	}
	args := `{"command":"switch-account","args":{"id":"job-a","account":"account-secondary","expected_account":"account-work","operation_id":"once"}}`
	if _, e = m.call(context.Background(), "codexm_control", json.RawMessage(args)); e != nil {
		t.Fatal(e)
	}
	var target struct {
		ID, Account string
		Expected    string `json:"expected_account"`
		Operation   string `json:"operation_id"`
	}
	_ = json.Unmarshal(received.Args, &target)
	if target.ID != "job-a" || target.Account != "account-secondary" || target.Expected != "account-work" || target.Operation != "once" {
		t.Fatal("target preconditions lost")
	}
}
