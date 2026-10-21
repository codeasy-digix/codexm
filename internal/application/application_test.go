package application

import (
	"context"
	"encoding/json"
	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"path/filepath"
	"testing"
)

type fakeLogin struct{ starts, cancels int }

func (f *fakeLogin) Start(context.Context, string, string) (accounts.LoginStatus, error) {
	f.starts++
	return accounts.LoginStatus{LoginID: "owned", Status: "pending"}, nil
}
func (f *fakeLogin) Status(string) (accounts.LoginStatus, error) {
	return accounts.LoginStatus{LoginID: "owned"}, nil
}
func (f *fakeLogin) Cancel(context.Context, string) (accounts.LoginStatus, error) {
	f.cancels++
	return accounts.LoginStatus{Status: "cancelled"}, nil
}
func (f *fakeLogin) Close() {}
func TestLoginBeforeManagerIsOwnerScopedAndIdempotent(t *testing.T) {
	c := config.Defaults()
	c.Path = filepath.Join(t.TempDir(), "config.toml")
	a := New(context.Background(), c, "")
	defer a.Close()
	fake := &fakeLogin{}
	a.NewLogins = func(context.Context, config.Config) LoginBroker { return fake }
	raw, _ := json.Marshal(map[string]string{"account": "new", "mode": "device", "generation": a.Generation})
	for range 2 {
		if _, err := a.Call(context.Background(), "browser-owner-one", "login.start", "login-operation-id", raw); err != nil {
			t.Fatal(err)
		}
	}
	if fake.starts != 1 {
		t.Fatal("login started twice")
	}
	for _, method := range []string{"login.status", "login.cancel"} {
		if _, err := a.Call(context.Background(), "browser-owner-two", method, "cancel-operation", json.RawMessage(`{"login_id":"owned"}`)); err == nil {
			t.Fatal("login leaked to another browser")
		}
	}
	raw, _ = json.Marshal(map[string]string{"account": "new", "mode": "device", "generation": "previous-bridge"})
	if _, err := a.Call(context.Background(), "browser-owner-one", "login.start", "different-operation", raw); err == nil {
		t.Fatal("login replayed after restart")
	}
}
