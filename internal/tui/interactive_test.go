package tui

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/application"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/settings"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

// This opt-in PTY fixture uses private temporary config/history/account paths and
// a recording RPC endpoint. It cannot start an engine or contact an LLM.
type interactiveFixtureModel struct{ Model }

func (m interactiveFixtureModel) Init() tea.Cmd { return tea.Batch(m.poll(), tick()) }

type interactiveAccountReader struct{}

func (interactiveAccountReader) Read(_ context.Context, name string) accounts.Snapshot {
	return accounts.Snapshot{Account: name, Authenticated: true}
}

func TestInteractiveDashboardFixture(t *testing.T) {
	if os.Getenv("CODEXM_TUI_FIXTURE") != "1" {
		t.Skip("opt-in private PTY fixture")
	}
	f, m := startTUIFixture(t)
	large := os.Getenv("CODEXM_TUI_LARGE_FIXTURE") == "1"
	if large {
		// This database lives only under startTUIFixture's private temporary home.
		if err := os.MkdirAll(m.Config.Runtime.SharedCodexHome, 0700); err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("sqlite", filepath.Join(m.Config.Runtime.SharedCodexHome, "state_5.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		if _, err = db.Exec(`CREATE TABLE threads(id TEXT PRIMARY KEY, title TEXT, source TEXT, updated_at INTEGER)`); err != nil {
			t.Fatal(err)
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		for i := 0; i < 1000; i++ {
			if _, err = tx.Exec(`INSERT INTO threads VALUES(?,?,'cli',1)`, fmt.Sprintf("fixture-native-%04d", i), fmt.Sprintf("대용량 목록 %04d", i)); err != nil {
				t.Fatal(err)
			}
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range m.Config.WorkerAccounts.Priority {
		dir := filepath.Join(m.Config.Runtime.AccountsDir, name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	st := supervisor.Status{Version: "fixture", ManagerAccount: "account-backup", WorkerPriority: m.Config.WorkerAccounts.Priority, NativeDefaultAccount: "account-work", Health: "격리 테스트", ServiceMode: true}
	for _, name := range m.Config.WorkerAccounts.Priority {
		st.Accounts = append(st.Accounts, accounts.Snapshot{Account: name, Authenticated: true, CheckedAt: now, Single: accounts.Bucket{Primary: &accounts.Window{UsedPercent: 25}, Secondary: &accounts.Window{UsedPercent: 40}}})
	}
	for i := 0; i < 35; i++ {
		st.Jobs = append(st.Jobs, jobs.Job{ID: fmt.Sprintf("fixture-job-%02d", i), ThreadID: fmt.Sprintf("fixture-thread-%02d", i), RunID: fmt.Sprintf("fixture-run-%02d", i), Title: fmt.Sprintf("격리 작업 %02d", i), Kind: "chat", Managed: true, State: "idle", Account: "account-work", Cwd: m.Config.Runtime.SharedCodexHome, UpdatedAt: now.Add(-time.Duration(i) * time.Second)})
	}
	log := jobs.WorkLog{At: now, Note: "격리된 모의 기록", Runs: []jobs.Run{{ID: "fixture-run-00", JobID: st.Jobs[0].ID, Account: "account-work", StartedAt: now.Add(-10 * time.Minute), UsageStart: &jobs.AccountUsage{Primary: &jobs.UsageWindow{UsedPercent: 10}}, UsageLatest: &jobs.AccountUsage{Primary: &jobs.UsageWindow{UsedPercent: 25}}}}, Changes: []jobs.WorkChange{{ID: "change", JobID: st.Jobs[0].ID, From: "account-secondary", To: "account-work", At: now.Add(-20 * time.Minute), Reason: "격리 테스트 전환"}}}
	var chatMu sync.Mutex
	inputs := map[string]string{}
	f.Handler = func(call supervisor.Call) (any, error) {
		switch call.Command {
		case "status":
			return st, nil
		case "snapshot":
			return map[string]any{"jobs": st.Jobs, "accounts": st.Accounts}, nil
		case "logs":
			return log, nil
		case "accounts":
			return st.Accounts, nil
		case "usage":
			return map[string]any{"manager": []any{}, "workers": []any{}}, nil
		case "models":
			return map[string]any{"data": []any{map[string]any{"id": "fixture-model", "model": "fixture-model", "displayName": "격리 모의 모델"}}}, nil
		case "settings-save":
			return settings.Save(context.Background(), m.Config, interactiveAccountReader{}, call.Args)
		case "send":
			var args supervisor.JobArgs
			json.Unmarshal(call.Args, &args)
			chatMu.Lock()
			inputs[args.ID] = args.Text
			chatMu.Unlock()
			return jobs.Action{ID: "fake-accepted-" + args.ID}, nil
		case "read":
			var args supervisor.JobArgs
			json.Unmarshal(call.Args, &args)
			thread, jobID := args.ThreadID, args.ID
			for _, job := range st.Jobs {
				if job.ID == jobID || job.ThreadID == thread {
					thread, jobID = job.ThreadID, job.ID
					break
				}
			}
			chatMu.Lock()
			text := inputs[jobID]
			chatMu.Unlock()
			if text == "" {
				text = "격리된 기존 대화"
			}
			conversation := fixtureChatThread(thread, "격리 응답 · "+text)
			if large {
				items := conversation.Turns[0].Items
				command := cr.Item{ID: "large-output", Type: "commandExecution", Command: "fixture-large-output", Status: "completed", AggregatedOutput: strings.Repeat("isolated command output line\n", 30000)}
				conversation.Turns[0].Items = []cr.Item{items[0], command, items[1]}
			}
			return map[string]any{"thread": conversation}, nil
		}
		return true, nil
	}
	m.Ready = true
	m.Width = 120
	m.Height = 40
	m.Status = st
	m.SelectedID = st.Jobs[0].ID
	fake := &managementFakeLogin{}
	m.App.NewLogins = func(_ context.Context, _ config.Config) application.LoginBroker { return fake }
	program := tea.NewProgram(interactiveFixtureModel{m})
	if _, err := program.Run(); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	count := len(f.Operations)
	f.mu.Unlock()
	evidence := map[string]any{"operations": count, "fake_login_starts": fake.Starts, "fake_login_cancels": fake.Cancels, "model_calls": 0, "private_config": true}
	evidence["large_fixture"] = large
	chatMu.Lock()
	evidence["chat_inputs"] = inputs
	chatMu.Unlock()
	if file := os.Getenv("CODEXM_TUI_FIXTURE_REPORT"); file != "" {
		raw, _ := json.MarshalIndent(evidence, "", "  ")
		if err := os.WriteFile(file, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
