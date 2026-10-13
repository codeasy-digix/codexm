package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/store"
	"github.com/codeasy-digix/codexm/internal/supervisor"
	"golang.org/x/sys/unix"
)

const hookThread = "00000000-6ef7-77e5-97fc-94aca5fb1871"
const otherHookThread = "00000000-a1f5-75d3-9158-4d5475ecb75f"

func hookRegistrationConfig(t *testing.T) config.Config {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("CODEX_HOME", filepath.Join(root, "native"))
	t.Setenv("CODEX_ACCOUNT", "")
	t.Setenv("CODEX_THREAD_ID", otherHookThread)
	c := config.Defaults()
	c.Path = filepath.Join(root, "config.toml")
	c.Manager.Account = "account-backup"
	c.Runtime.SharedCodexHome = filepath.Join(root, "native")
	c.Runtime.AccountsDir = filepath.Join(root, "accounts")
	c.Runtime.EnginesDir = filepath.Join(root, "engines")
	c.Storage.StateDir = filepath.Join(root, "state")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	return c
}

// Use the real Unix RPC client, with only ping and registration reachable. This
// never starts a service, a worker or a Codex runtime.
func hookRegistrationServer(t *testing.T, c config.Config, handle func(context.Context, supervisor.Call) (any, error)) {
	t.Helper()
	socket := supervisor.Socket(c)
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var call supervisor.Call
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			http.Error(w, "bad test call", http.StatusBadRequest)
			return
		}
		if call.Command != "ping" && call.Command != "register" {
			t.Errorf("registration issued unexpected command %q", call.Command)
			http.Error(w, "unexpected command", http.StatusBadRequest)
			return
		}
		value, err := handle(r.Context(), call)
		reply := supervisor.Response{}
		if err != nil {
			reply.Error = err.Error()
		} else {
			reply.Result, _ = json.Marshal(value)
		}
		_ = json.NewEncoder(w).Encode(reply)
	})}
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = srv.Close()
		<-done
		_ = os.Remove(filepath.Dir(socket)) // Remove a hashed /tmp socket dir when empty.
	})
}

func hookRegistrationCall(t *testing.T, ctx context.Context, path, id string) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = previous; _ = r.Close(); _ = w.Close() }()
	message, err := registerPrompt(ctx, path, id)
	_ = w.Close()
	stdout, readErr := io.ReadAll(r)
	if readErr != nil || len(stdout) != 0 {
		t.Fatalf("registration wrote into hook stdout: %q (%v)", stdout, readErr)
	}
	return message, err
}

func TestRegisterPromptRejectsMissingOrInvalidHookIDWithoutFallback(t *testing.T) {
	c := hookRegistrationConfig(t)
	// Even a valid environment ID must not be substituted for hook session_id.
	if err := os.WriteFile(c.Path, []byte("not valid TOML [[["), 0600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", "latest", "../thread", hookThread + "\n", strings.ReplaceAll(hookThread, "-", "")} {
		message, err := hookRegistrationCall(t, context.Background(), c.Path, id)
		if err == nil || message != err.Error() || !strings.Contains(message, "대화 ID") {
			t.Fatalf("invalid hook ID accepted or configuration consulted: %q, %q, %v", id, message, err)
		}
	}
	if _, err := os.Stat(c.StateDir()); !os.IsNotExist(err) {
		t.Fatal("invalid session created supervisor state", err)
	}
}

func TestRegisterPromptUsesHookThreadAndVerifiedOriginWithoutOutputOrCredentialChanges(t *testing.T) {
	c := hookRegistrationConfig(t)
	privateAuth := []byte(`{"tokens":{"account_id":"private-account-id","refresh_token":"private-auth-token"}}`)
	paths := []string{filepath.Join(c.Runtime.AccountsDir, "account-secondary", "auth.json"), filepath.Join(c.Runtime.SharedCodexHome, "auth.json")}
	for _, path := range paths {
		if err := config.AtomicWrite(path, privateAuth, 0600); err != nil {
			t.Fatal(err)
		}
	}
	requests := make(chan supervisor.JobArgs, 1)
	hookRegistrationServer(t, c, func(_ context.Context, call supervisor.Call) (any, error) {
		if call.Command == "ping" {
			return true, nil
		}
		var a supervisor.JobArgs
		if err := json.Unmarshal(call.Args, &a); err != nil {
			return nil, err
		}
		requests <- a
		return jobs.Job{ThreadID: a.ThreadID, Account: a.Account, Managed: true, State: "external_writer", Goal: &jobs.Goal{Objective: "private-goal-text", Status: "active"}}, nil
	})
	message, err := hookRegistrationCall(t, context.Background(), c.Path, hookThread)
	if err != nil {
		t.Fatal(err)
	}
	a := <-requests
	if a.ThreadID != hookThread || a.Account != "account-secondary" || a.OriginAccount != "account-secondary" || a.ID != "" || a.Text != "" || a.Manage || a.Steer {
		t.Fatalf("wrong registration target or request: %+v", a)
	}
	for _, want := range []string{"등록", "인계 후 작업 계정: account-secondary", "external_writer", "실행권을 해제한 뒤"} {
		if !strings.Contains(message, want) {
			t.Fatalf("summary omitted %q: %s", want, message)
		}
	}
	for _, private := range []string{"private-goal-text", "private-account-id", "private-auth-token"} {
		if strings.Contains(message, private) {
			t.Fatal("summary disclosed private data")
		}
	}
	for _, path := range paths {
		if after, err := os.ReadFile(path); err != nil || string(after) != string(privateAuth) {
			t.Fatal("registration changed credentials", err)
		}
	}
}

func TestRegisterPromptUsesConfiguredDefaultWhenOriginIsUnknown(t *testing.T) {
	c := hookRegistrationConfig(t)
	c.WorkerAccounts.DefaultAccount = "account-main"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	requests := make(chan supervisor.JobArgs, 1)
	hookRegistrationServer(t, c, func(_ context.Context, call supervisor.Call) (any, error) {
		if call.Command == "ping" {
			return true, nil
		}
		var a supervisor.JobArgs
		if err := json.Unmarshal(call.Args, &a); err != nil {
			return nil, err
		}
		requests <- a
		return jobs.Job{ThreadID: a.ThreadID, Account: a.Account, Managed: true, State: "idle"}, nil
	})
	message, err := hookRegistrationCall(t, context.Background(), c.Path, hookThread)
	if err != nil || !strings.Contains(message, "인계 후 작업 계정: account-main") {
		t.Fatal(message, err)
	}
	a := <-requests
	if a.ThreadID != hookThread || a.Account != "account-main" || a.OriginAccount != "" {
		t.Fatalf("unknown origin was guessed or default ignored: %+v", a)
	}
}

func TestRegisterPromptReusesCorePreservingWriterGoalAndExistingRegistration(t *testing.T) {
	c := hookRegistrationConfig(t)
	st, err := store.Open(c.StateDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	budget := int64(30000)
	original := jobs.Job{ID: "isolated-job", ThreadID: hookThread, Store: c.Runtime.SharedCodexHome, Account: "account-work", InitialAccount: "account-work", State: "observed", Kind: "goal", Model: "existing-model", Effort: "high", Goal: &jobs.Goal{Objective: "private-goal-text", Status: "active", TokenBudget: &budget, TokensUsed: 7654}}
	if err := st.PutJob(original); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(c.Runtime.SharedCodexHome, "thread-writer-locks", hookThread+".lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0700); err != nil {
		t.Fatal(err)
	}
	writer, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Flock(int(writer.Fd()), unix.LOCK_UN); _ = writer.Close() })
	if err := unix.Flock(int(writer.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	core := &supervisor.Server{Config: c, Store: st}
	hookRegistrationServer(t, c, core.Handle)
	message, err := hookRegistrationCall(t, context.Background(), c.Path, hookThread)
	if err != nil || !strings.Contains(message, "external_writer") {
		t.Fatal(message, err)
	}
	first, err := st.Job(original.ID)
	if err != nil || !first.Managed || first.State != "external_writer" || first.Account != original.Account || first.Model != original.Model || first.Effort != original.Effort || !reflect.DeepEqual(first.Goal, original.Goal) {
		t.Fatal("core registration changed existing goal or settings", err)
	}
	// A second request with a changed default must retain the registered job and
	// its account; registration is not an implicit switch or goal restart.
	c.WorkerAccounts.DefaultAccount = "account-secondary"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	message, err = hookRegistrationCall(t, context.Background(), c.Path, hookThread)
	if err != nil || !strings.Contains(message, "인계 후 작업 계정: account-work") {
		t.Fatal(message, err)
	}
	again, err := st.Job(original.ID)
	if err != nil || !reflect.DeepEqual(again, first) {
		t.Fatal("repeat registration mutated existing job", err)
	}
	all, err := st.Jobs()
	if err != nil || len(all) != 1 {
		t.Fatal("repeat registration duplicated a job", err)
	}
	actions, err := st.Actions("")
	if err != nil || len(actions) != 0 {
		t.Fatal("registration scheduled work or account changes", err)
	}
	probe, err := os.OpenFile(lockPath, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal("registration removed writer lock", err)
	}
	defer probe.Close()
	if err := unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB); err == nil {
		_ = unix.Flock(int(probe.Fd()), unix.LOCK_UN)
		t.Fatal("registration released foreign writer lock")
	}
}

func TestRegisterPromptUnavailableSetupAndServiceDoNotStartAnything(t *testing.T) {
	for _, setup := range []bool{false, true} {
		t.Run(map[bool]string{false: "setup", true: "service"}[setup], func(t *testing.T) {
			c := hookRegistrationConfig(t)
			if !setup {
				c.Manager.Account = ""
				if err := c.Save(); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(c.Path)
			if err != nil {
				t.Fatal(err)
			}
			message, err := hookRegistrationCall(t, context.Background(), c.Path, hookThread)
			want := "codexm setup"
			if setup {
				want = "codexm service status"
			}
			if err == nil || message != err.Error() || !strings.Contains(message, want) {
				t.Fatal(message, err)
			}
			after, err := os.ReadFile(c.Path)
			if err != nil || string(before) != string(after) {
				t.Fatal("unavailable registration changed settings", err)
			}
			for _, path := range []string{c.StateDir(), filepath.Join(os.Getenv("HOME"), "Library"), c.Runtime.AccountsDir, c.Runtime.SharedCodexHome} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("registration created runtime/service files", path, err)
				}
			}
		})
	}
}

func TestRegisterPromptDeadlineAndRPCFailureAreBoundedAndPrivate(t *testing.T) {
	for _, stall := range []bool{false, true} {
		t.Run(map[bool]string{false: "private-rpc-error", true: "deadline"}[stall], func(t *testing.T) {
			c := hookRegistrationConfig(t)
			hookRegistrationServer(t, c, func(ctx context.Context, call supervisor.Call) (any, error) {
				if call.Command == "ping" {
					return true, nil
				}
				if stall {
					<-ctx.Done()
				}
				return nil, errors.New("private-goal-text private-auth-token")
			})
			ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
			defer cancel()
			started := time.Now()
			message, err := hookRegistrationCall(t, ctx, c.Path, hookThread)
			if err == nil || message != err.Error() || !strings.Contains(message, "등록 결과") || strings.Contains(message, "private-") {
				t.Fatal(message, err)
			}
			if time.Since(started) > time.Second {
				t.Fatal("registration ignored caller deadline")
			}
		})
	}
}
