//go:build gui_experiment

package nativecli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/codeasy-digix/codexm/internal/config"
)

func TestDaemonIdentityRequiresLiveUserAndWorkspace(t *testing.T) {
	c := toolbarFixture(t)
	saveToolbarAuth(t, c, "alpha", "alice", "team")
	for _, s := range []string{
		`{"account":null}`,
		`{"account":{"type":"apiKey"}}`,
		`{"account":{"type":"chatgpt","email":"bob@example.invalid"},"workspaceRouting":{"chatgptAccountId":"team"}}`,
		`{"account":{"type":"chatgpt","email":"alice@example.invalid"},"workspaceRouting":{"chatgptAccountId":"other-team"}}`,
		`{"account":{"type":"chatgpt","email":"alice@example.invalid"}}`,
	} {
		var live daemonAccount
		if err := json.Unmarshal([]byte(s), &live); err != nil {
			t.Fatal(err)
		}
		if _, err := daemonLiveIdentity(c, "alpha", live); err == nil {
			t.Fatal("unknown/different live login accepted")
		}
	}
	var live daemonAccount
	_ = json.Unmarshal([]byte(`{"account":{"type":"chatgpt","email":"alice@example.invalid"},"workspaceRouting":{"chatgptAccountId":"team"}}`), &live)
	key, err := daemonLiveIdentity(c, "alpha", live)
	if err != nil || key == "" {
		t.Fatal("matching login refused", err)
	}
	saveToolbarAuth(t, c, "alpha", "bob", "team")
	if _, err := daemonLiveIdentity(c, "alpha", live); err == nil {
		t.Fatal("replacement disk login accepted for cached daemon")
	}
}

func TestDaemonExperimentNeverReplaysUnknownTransfers(t *testing.T) {
	c := toolbarFixture(t)
	r := DaemonTransfer{Request: "00000000-0000-4000-8000-000000000031", Thread: fixtureID, From: "alpha", To: "beta"}
	if err := saveDaemonTransfer(c, &r, "unknown"); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(r.Request)
	if err := config.AtomicWrite(filepath.Join(daemonRoot(c), "chats", r.Thread+".json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := TransferDaemonChat(context.Background(), c, r.Request, r.Thread, r.From, r.To, false)
	if err != nil || got.State != "unknown" || !got.Updated.Equal(r.Updated) {
		t.Fatal("unknown receipt replayed", got, err)
	}
	if _, err := TransferDaemonChat(context.Background(), c, "00000000-0000-4000-8000-000000000032", r.Thread, r.From, r.To, false); err == nil || !strings.Contains(err.Error(), "needs inspection") {
		t.Fatal("new request bypassed unresolved transfer", err)
	}
	if _, err := TransferDaemonChat(context.Background(), c, r.Request, r.Thread, "beta", "alpha", false); err == nil {
		t.Fatal("request parameter mismatch accepted")
	}
	st, err := os.Stat(daemonTransferPath(c, r.Request))
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("receipt permissions", err)
	}
	for _, name := range []string{"default", "", "../alpha"} {
		if _, err := daemonAccountHome(c, name); err == nil {
			t.Fatal("invalid/default daemon accepted", name)
		}
	}
}

func TestDaemonFrontendUsesRemoteWithoutEmbeddedOwnerOrPrompt(t *testing.T) {
	args := daemonResumeArgs("/fixture/codex", "/fixture/control.sock", fixtureID)
	want := []string{"/fixture/codex", "--remote", "unix:///fixture/control.sock", "--no-alt-screen", "resume", fixtureID}
	if !slices.Equal(args, want) {
		t.Fatal(args)
	}
	if slices.Contains(args, "--no-daemon") || slices.Contains(args, ContinuePrompt) {
		t.Fatal("embedded owner or implicit continuation introduced")
	}
	if len(daemonResumeArgs("codex", "/fixture/control.sock", "")) != 4 {
		t.Fatal("new frontend invented a chat or prompt")
	}
}

func TestDaemonTransfersSerializeAcrossThreadsAndRequestIDs(t *testing.T) {
	c := toolbarFixture(t)
	unlock, err := lockDaemonTransfers(c)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if release, err := lockDaemonTransfers(c); err == nil {
		release()
		t.Fatal("concurrent request could overwrite receipt")
	}
}

type daemonPreflightFixture struct {
	child    bool
	archived bool
	queue    bool
	failed   bool
	calls    []handoffCall
}

func (f *daemonPreflightFixture) Call(_ context.Context, method string, params, out any) error {
	p := params.(map[string]any)
	f.calls = append(f.calls, handoffCall{method, p})
	if f.failed {
		return errors.New("inspection unavailable")
	}
	if method != "thread/list" && method != "thread/queue/list" {
		return errors.New("unexpected mutation")
	}
	data := []any{}
	if method == "thread/list" && f.child && p["archived"] == f.archived || method == "thread/queue/list" && f.queue {
		data = append(data, map[string]string{"id": "fixture"})
	}
	b, _ := json.Marshal(map[string]any{"data": data})
	return json.Unmarshal(b, out)
}

func TestDaemonTransferProtectsDescendantsAndQueuedInput(t *testing.T) {
	for _, f := range []*daemonPreflightFixture{{child: true}, {child: true, archived: true}, {queue: true}, {failed: true}, {}} {
		err := daemonTransferPreflight(context.Background(), f, fixtureID)
		blocked := f.child || f.queue || f.failed
		if (err != nil) != blocked {
			t.Fatalf("preflight %#v: %v", f, err)
		}
		for _, call := range f.calls {
			if call.method == "thread/list" && (call.params["ancestorThreadId"] != fixtureID || call.params["useStateDbOnly"] != true) {
				t.Fatal("descendant scope lost", call)
			}
			if call.method == "thread/queue/list" && call.params["threadId"] != fixtureID {
				t.Fatal("queue scope lost", call)
			}
		}
	}
}
