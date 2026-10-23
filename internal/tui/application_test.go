package tui

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/application"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

type tuiRPCFixture struct {
	Config        config.Config
	mu            sync.Mutex
	Operations    []supervisor.WebOperation
	BreakDispatch bool
	Handler       func(supervisor.Call) (any, error)
}

func startTUIFixture(t *testing.T) (*tuiRPCFixture, Model) {
	t.Helper()
	dir, err := os.MkdirTemp("", "cm-tui-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	c := config.Defaults()
	c.Manager.Account = "account-backup"
	c.Path = filepath.Join(dir, "config.toml")
	c.Storage.StateDir = filepath.Join(dir, "state")
	c.Runtime.SharedCodexHome = filepath.Join(dir, "native")
	c.Runtime.AccountsDir = filepath.Join(dir, "accounts")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(c.StateDir(), 0700); err != nil {
		t.Fatal(err)
	}
	f := &tuiRPCFixture{Config: c}
	listener, err := net.Listen("unix", supervisor.Socket(c))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var call supervisor.Call
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			http.Error(w, "invalid call", 400)
			return
		}
		var result any = true
		var callErr error
		if call.Command == "ping" {
			result = map[string]any{"web_api": 1, "accounts_api": 1, "account_reassignment_api": 1}
		} else if call.Command == "web-operation" {
			var op supervisor.WebOperation
			if err := json.Unmarshal(call.Args, &op); err != nil {
				http.Error(w, "invalid operation", 400)
				return
			}
			f.mu.Lock()
			f.Operations = append(f.Operations, op)
			broken := f.BreakDispatch
			f.mu.Unlock()
			if broken {
				w.Write([]byte(`{`))
				return
			}
			if f.Handler != nil {
				result, callErr = f.Handler(supervisor.Call{Command: op.Command, Args: op.Args})
			}
		} else if f.Handler != nil {
			result, callErr = f.Handler(call)
		}
		raw, _ := json.Marshal(result)
		reply := supervisor.Response{Result: raw}
		if callErr != nil {
			reply.Error = callErr.Error()
		}
		json.NewEncoder(w).Encode(reply)
	})}
	go server.Serve(listener)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		server.Shutdown(ctx)
	})
	m := New(c, "fixture-codexm")
	m.App = application.New(context.Background(), c, m.Self)
	t.Cleanup(m.App.Close)
	return f, m
}

func TestTUIAccountSwitchUsesDurableOperationAndExactEmptyRun(t *testing.T) {
	f, m := startTUIFixture(t)
	m.Status.Jobs = []jobs.Job{{ID: "job", ThreadID: "thread", Managed: true, Kind: "chat", Account: "account-work", RunID: ""}}
	m.SelectedID = "job"
	cmd := m.slash("/account account-secondary")
	if cmd == nil {
		t.Fatal("no account-change command")
	}
	reply := cmd().(replyMsg)
	if reply.Error != nil {
		t.Fatal(reply.Error)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Operations) != 1 || f.Operations[0].ID == "" || f.Operations[0].Command != "switch-account" {
		t.Fatal("UI bypassed the shared durable operation layer")
	}
	var args map[string]any
	json.Unmarshal(f.Operations[0].Args, &args)
	if run, ok := args["run_id"]; !ok || run != "" || args["expected_account"] != "account-work" || args["account"] != "account-secondary" {
		t.Fatal("account change lost its expected account/run")
	}
}

func TestNewTaskFormPreservesExplicitFieldsAndSubmitsOnlyOnce(t *testing.T) {
	f, m := startTUIFixture(t)
	f.Handler = func(call supervisor.Call) (any, error) {
		return jobs.Job{ID: "created", Kind: "goal", Title: "isolated"}, nil
	}
	m.beginTaskForm("new")
	form := &m.TaskForm.Form
	form.setValue("cwd", t.TempDir())
	form.setValue("kind", "goal")
	form.setValue("goal", "isolated fixture objective")
	form.setValue("budget", "12345")
	form.setValue("model", "fixture-model")
	form.setValue("effort", "high")
	form.setValue("open", "false")
	form.setValue("prompt", "isolated first instruction")
	if len(f.Operations) != 0 {
		t.Fatal("opening a form dispatched work")
	}
	cmd := m.submitTaskForm("save")
	if cmd == nil {
		t.Fatal(m.TaskForm.Error)
	}
	if again := m.submitTaskForm("save"); again != nil {
		t.Fatal("pending creation was submitted twice")
	}
	reply := cmd().(replyMsg)
	if reply.Error != nil {
		t.Fatal(reply.Error)
	}
	f.mu.Lock()
	op := f.Operations[0]
	f.mu.Unlock()
	var args supervisor.NewArgs
	if err := json.Unmarshal(op.Args, &args); err != nil {
		t.Fatal(err)
	}
	if args.Kind != "goal" || args.TokenBudget == nil || *args.TokenBudget != 12345 || args.Model != "fixture-model" || args.Effort != "high" || args.Prompt != "isolated first instruction" {
		t.Fatal("new form lost explicit task settings")
	}
	_, handled := m.taskFormUpdate(reply)
	if !handled || m.TaskForm.Kind != "" || m.SelectedID != "created" || m.Input.Value() != "" {
		t.Fatal("creation did not restore an empty composer and selected job")
	}
}

func TestAmbiguousTransportResultNeverResubmitsInput(t *testing.T) {
	f, m := startTUIFixture(t)
	f.BreakDispatch = true
	m.Status.Jobs = []jobs.Job{{ID: "job", Managed: true, Kind: "chat", Account: "account-work"}}
	m.SelectedID = "job"
	m.beginTaskForm("send")
	m.TaskForm.Form.setValue("text", "isolated instruction")
	cmd := m.submitTaskForm("save")
	if cmd == nil {
		t.Fatal(m.TaskForm.Error)
	}
	reply := cmd().(replyMsg)
	if reply.Error == nil || reply.OperationID == "" {
		t.Fatal("fixture did not produce a tracked ambiguous result")
	}
	m.taskFormUpdate(reply)
	if !m.TaskForm.Uncertain || m.TaskForm.Form.value("text") != "isolated instruction" {
		t.Fatal("uncertain result lost the draft or permitted blind retry")
	}
	if retry := m.submitTaskForm("save"); retry != nil {
		t.Fatal("uncertain input was submitted under a new operation ID")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Operations) != 1 {
		t.Fatal("duplicate dispatch")
	}
}

func TestManagerScopeAndNavigationNeverTurnSearchIntoModelInput(t *testing.T) {
	f, m := startTUIFixture(t)
	m.Status.Jobs = []jobs.Job{{ID: "selected", Kind: "chat", Managed: true, Account: "account-work"}}
	m.SelectedID = "selected"
	m.dashboardAction("manager:all")
	cmd := m.submit("isolated global manager instruction")
	if cmd == nil {
		t.Fatal("explicit global manager instruction missing")
	}
	if reply := cmd().(replyMsg); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	var args supervisor.JobArgs
	f.mu.Lock()
	json.Unmarshal(f.Operations[0].Args, &args)
	f.mu.Unlock()
	if args.ID != "" {
		t.Fatal("global manager instruction targeted a selected job")
	}
	m.activateTab("tasks")
	m.submit("search text")
	if m.Target != "search" || m.Filter != "search text" {
		t.Fatal("returning to tasks left model input active")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Operations) != 1 {
		t.Fatal("search or navigation invoked a model")
	}
}

func TestFirstRunSettingsSaveDoesNotAskForManagerAgain(t *testing.T) {
	m := managementTestModel()
	m.Screen = "setup"
	m.Config.Manager.Account = ""
	m.Management.Mode = "settings"
	doc := managementTestDocument()
	cmd, handled := m.managementUpdate(managementReply(t, "settings.save", doc))
	if !handled || cmd == nil || m.Config.Manager.Account != "a" || m.Screen != "agents" || m.managementVisible() {
		t.Fatal("saving the first manager returned to an unrelated initial choice")
	}
	// The connection command is intentionally not run in this unit test.
}
