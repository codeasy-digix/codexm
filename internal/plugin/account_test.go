package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
)

const testLoginID = "0123456789abcdef0123456789abcdef"

type testLoginBroker struct {
	closed chan struct{}
	once   sync.Once
	calls  atomic.Int32
	block  bool
	fail   bool
}

func (b *testLoginBroker) Start(context.Context, string, string) (accounts.LoginStatus, error) {
	b.calls.Add(1)
	if b.block {
		<-b.closed
	}
	if b.fail {
		return accounts.LoginStatus{}, errors.New("PRIVATE_PROVIDER_URL https://auth.openai.com/?PRIVATE_CODE=SECRET")
	}
	return accounts.LoginStatus{Account: "saved", LoginID: testLoginID, Mode: "device", Status: "pending", VerificationURL: "https://auth.openai.com/codex/device", UserCode: "PRIVATE_DEVICE_CODE"}, nil
}
func (b *testLoginBroker) Status(string) (accounts.LoginStatus, error) {
	b.calls.Add(1)
	return accounts.LoginStatus{Account: "saved", LoginID: testLoginID, Status: "succeeded"}, nil
}
func (b *testLoginBroker) Cancel(context.Context, string) (accounts.LoginStatus, error) {
	b.calls.Add(1)
	return accounts.LoginStatus{Account: "saved", LoginID: testLoginID, Status: "canceled"}, nil
}
func (b *testLoginBroker) Close() { b.once.Do(func() { close(b.closed) }) }

func loginTestMCP(t *testing.T) (MCP, *testLoginBroker) {
	t.Helper()
	i := fixture(t)
	b := &testLoginBroker{closed: make(chan struct{})}
	t.Cleanup(b.Close)
	return MCP{Config: i.Config, Self: i.Self, Logins: b}, b
}

func TestAccountLoginMetadataIsNeverModelContent(t *testing.T) {
	m, b := loginTestMCP(t)
	args := json.RawMessage(`{"name":"codexm_account","arguments":{"command":"start","account":"saved","mode":"device"}}`)
	v, rpcErr := m.handle(context.Background(), rpcRequest{Method: "tools/call", Params: args})
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	r := v.(map[string]any)
	public, _ := json.Marshal([]any{r["content"], r["structuredContent"]})
	private, _ := json.Marshal(r["_meta"])
	for _, value := range []string{"auth.openai.com", "PRIVATE_DEVICE_CODE", testLoginID} {
		if bytes.Contains(public, []byte(value)) || !bytes.Contains(private, []byte(value)) {
			t.Fatal("authentication data crossed the UI boundary", value)
		}
	}
	b.fail = true
	v, rpcErr = m.handle(context.Background(), rpcRequest{Method: "tools/call", Params: args})
	failure, _ := json.Marshal(v)
	if rpcErr != nil || bytes.Contains(failure, []byte("PRIVATE_")) || bytes.Contains(failure, []byte("auth.openai.com")) || v.(map[string]any)["isError"] != true {
		t.Fatal("provider error was exposed outside the login UI")
	}
}

func TestAccountActionsValidateTheirExactTarget(t *testing.T) {
	m, b := loginTestMCP(t)
	for _, raw := range []string{
		`null`, `[]`, `{}`, `{"command":"anything"}`,
		`{"command":"start","account":"saved","mode":"browser","login_id":""}`,
		`{"command":"start","account":"default","mode":"browser"}`,
		`{"command":"start","account":"../other","mode":"browser"}`,
		`{"command":"start","account":"saved","mode":"shell"}`,
		`{"command":"start","account":"saved","mode":"device","PRIVATE":true}`,
		`{"command":"status","login_id":"unknown"}`,
		`{"command":"cancel","login_id":"0123456789abcdef0123456789abcdef","account":"saved"}`,
		`{"command":"status","login_id":"0123456789abcdef0123456789ABCDEF"}`,
		`{"command":"status","login_id":null}`,
	} {
		if _, e := m.call(context.Background(), "codexm_account", json.RawMessage(raw)); e == nil {
			t.Fatal("invalid login request accepted", raw)
		}
	}
	if b.calls.Load() != 0 {
		t.Fatal("invalid fields reached the login backend")
	}
	for _, command := range []string{"status", "cancel"} {
		raw, _ := json.Marshal(map[string]string{"command": command, "login_id": testLoginID})
		if _, e := m.call(context.Background(), "codexm_account", raw); e != nil {
			t.Fatal(e)
		}
	}
	if b.calls.Load() != 2 {
		t.Fatal("valid login request lost its target")
	}
}

func TestAccountPanelCanBootstrapWithoutManagerOrSupervisor(t *testing.T) {
	m, b := loginTestMCP(t)
	if e := config.AtomicWrite(filepath.Join(m.Config.Runtime.AccountsDir, "outside", "auth.json"), []byte(`{}`), 0600); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(m.Config.Path)
	v, e := m.call(context.Background(), "codexm_accounts_open", json.RawMessage(`{}`))
	if e != nil {
		t.Fatal(e)
	}
	s := v.(map[string]any)
	if s["connected"] != false || s["setup_required"] != true || s["login_available"] != true || s["initial_view"] != "accounts" || s["default_worker_account"] != nil || !strings.Contains(strings.Join(s["account_names"].([]string), ","), "outside") || b.calls.Load() != 0 {
		t.Fatal("offline account setup requires an existing manager")
	}
	after, _ := os.ReadFile(m.Config.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("opening account management changed settings")
	}
	for _, name := range []string{"services", "state.sqlite"} {
		if _, e := os.Stat(filepath.Join(m.Config.StateDir(), name)); !os.IsNotExist(e) {
			t.Fatal("opening account management started supervision", name)
		}
	}
	if e := os.RemoveAll(m.Config.Runtime.AccountsDir); e != nil {
		t.Fatal(e)
	}
	settings, e := m.readPluginSettings()
	encoded, _ := json.Marshal(settings)
	if e != nil || !bytes.Contains(encoded, []byte("codexm_accounts_open")) {
		t.Fatal("first installation cannot reach the sign-in button", e)
	}
}

func TestLoginActionsAreAppOnlyAndButtonsHaveConsistentEntrypoints(t *testing.T) {
	found := map[string]bool{}
	for _, raw := range toolList() {
		entry := raw.(map[string]any)
		name := entry["name"].(string)
		if name != "codexm_account" && name != "codexm_accounts_open" && name != "codexm_open" {
			continue
		}
		found[name] = true
		encoded, _ := json.Marshal(entry)
		if name == "codexm_account" && !bytes.Contains(encoded, []byte(`"visibility":["app"]`)) {
			t.Fatal("sign-in action became model-visible", name)
		}
		if name == "codexm_accounts_open" {
			if bytes.Contains(encoded, []byte(`"visibility":["app"]`)) {
				t.Fatal("account command skill cannot call its read-only panel opener")
			}
			if entry["title"] != "계정 관리" || !bytes.Contains(encoded, []byte(`"type":"settings"`)) || bytes.Contains(encoded, []byte(`"type":"global"`)) || !bytes.Contains(encoded, []byte(ResourceURI)) {
				t.Fatal("account settings button does not target the shared panel")
			}
		}
		if name == "codexm_open" && entry["title"] != "codexm 관리" {
			t.Fatal("management entrypoint title changed")
		}
	}
	if len(found) != 3 {
		t.Fatal("account entrypoint or action is missing")
	}
}

func TestMCPEOFClosesLoginBeforeWaitingForRequests(t *testing.T) {
	m, b := loginTestMCP(t)
	b.block = true
	done := make(chan error, 1)
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"codexm_account","arguments":{"command":"start","account":"saved","mode":"browser"}}}` + "\n"
	go func() { done <- m.Serve(context.Background(), strings.NewReader(input), &bytes.Buffer{}) }()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal("normal EOF became a cancellation error", e)
		}
	case <-time.After(time.Second):
		t.Fatal("MCP EOF waited for a pending provider login")
	}
	select {
	case <-b.closed:
	default:
		t.Fatal("MCP left its login helper alive after EOF")
	}
}
