package nativecli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
)

type fakeBroker struct {
	starts, cancels int
	closed          bool
	value           accounts.LoginStatus
}

func (b *fakeBroker) Start(_ context.Context, account, mode string) (accounts.LoginStatus, error) {
	b.starts++
	b.value = accounts.LoginStatus{Account: account, Mode: mode, LoginID: "fake-login-id", Status: "pending", AuthURL: "https://auth.openai.com/PRIVATE-AUTH-CANARY", UserCode: "PRIVATE-CODE-CANARY"}
	return b.value, nil
}
func (b *fakeBroker) Status(string) (accounts.LoginStatus, error) { return b.value, nil }
func (b *fakeBroker) Cancel(context.Context, string) (accounts.LoginStatus, error) {
	b.cancels++
	b.value.Status = "canceled"
	b.value.AuthURL = ""
	b.value.UserCode = ""
	return b.value, nil
}
func (b *fakeBroker) Close() { b.closed = true }

func TestLoginBridgeScopesSecretsAndDeduplicatesWithoutPersistingCodes(t *testing.T) {
	c := fixture(t)
	b := &fakeBroker{}
	s := newLoginSession(context.Background())
	defer s.Close()
	s.newBroker = func(context.Context, config.Config) loginBroker { return b }
	start := Request{Version: 1, Owner: "browser-a", Method: "login.start", Operation: "operation-login-1234", Params: json.RawMessage(`{"account":"alpha","mode":"device"}`)}
	if _, err := s.Dispatch(c, start); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Dispatch(c, start); err != nil {
		t.Fatal(err)
	}
	if b.starts != 1 {
		t.Fatal("duplicate request launched login twice")
	}
	status := Request{Version: 1, Owner: "browser-b", Method: "login.status", Params: json.RawMessage(`{"login_id":"fake-login-id"}`)}
	if _, err := s.Dispatch(c, status); err == nil {
		t.Fatal("another browser could inspect authentication")
	}
	status.Method = "login.cancel"
	status.Operation = "operation-cancel-1234"
	if _, err := s.Dispatch(c, status); err == nil {
		t.Fatal("another browser could cancel authentication")
	}
	status.Owner = "browser-a"
	if _, err := s.Dispatch(c, status); err != nil {
		t.Fatal(err)
	}
	if b.cancels != 1 {
		t.Fatal("cancel was not forwarded")
	}
	files, _ := filepath.Glob(filepath.Join(root(c), "operations", "*"))
	for _, file := range files {
		data, _ := os.ReadFile(file)
		if strings.Contains(string(data), "CANARY") || strings.Contains(string(data), "auth.openai.com") {
			t.Fatal("auth data leaked into durable receipts")
		}
	}
	restarted := newLoginSession(context.Background())
	defer restarted.Close()
	restarted.newBroker = s.newBroker
	if _, err := restarted.Dispatch(c, start); err == nil {
		t.Fatal("lost login outcome was replayed after restart")
	}
	if b.starts != 1 {
		t.Fatal("restarted bridge repeated sign-in")
	}
	s.Close()
	if !b.closed {
		t.Fatal("owned login helper was not closed")
	}
}

func TestAccountManagementRefreshesRegistryWithoutChangingHistoryOrSettings(t *testing.T) {
	c := fixture(t)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(c.Runtime.AccountsDir, "alpha")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	auth := []byte(`{"auth_mode":"chatgpt","tokens":{"id_token":"fixture-id","access_token":"fixture-access","refresh_token":"fixture-refresh","account_id":"fixture-account"}}`)
	if err := os.WriteFile(filepath.Join(home, "auth.json"), auth, 0600); err != nil {
		t.Fatal(err)
	}
	native := filepath.Join(c.Runtime.SharedCodexHome, "config.toml")
	settings := []byte("model='fixture-model'\nmodel_reasoning_effort='high'\n")
	if err := os.WriteFile(native, settings, 0600); err != nil {
		t.Fatal(err)
	}
	history := filepath.Join(c.Runtime.SharedCodexHome, "state_5.sqlite")
	before, _ := os.ReadFile(history)
	input := strings.NewReader(`{"protocolVersion":1,"id":1,"method":"accounts.setDefault","owner":"browser","operation_id":"set-default-123456","params":{"account":"alpha"}}
{"protocolVersion":1,"id":2,"method":"accounts.remove","owner":"browser","operation_id":"remove-account-12345","params":{"account":"alpha"}}
{"protocolVersion":1,"id":3,"method":"snapshot","params":{}}
`)
	var output strings.Builder
	if err := Serve(context.Background(), c, "", input, &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatal("missing replies")
	}
	for _, line := range lines {
		var reply struct{ Error any }
		json.Unmarshal([]byte(line), &reply)
		if reply.Error != nil {
			t.Fatal("management request failed", line)
		}
	}
	var reply struct{ Result struct{ Accounts []Account } }
	if err := json.Unmarshal([]byte(lines[2]), &reply); err != nil || len(reply.Result.Accounts) != 0 {
		t.Fatal("removed registration stayed in next snapshot")
	}
	for file, want := range map[string][]byte{native: settings, history: before, filepath.Join(home, "auth.json"): auth, filepath.Join(c.Runtime.SharedCodexHome, "auth.json"): auth} {
		got, err := os.ReadFile(file)
		if err != nil || string(got) != string(want) {
			t.Fatal("account management changed preserved data", file)
		}
	}
}
