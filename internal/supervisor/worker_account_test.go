package supervisor

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func TestAutomaticJobsFollowPanelPriorityWithoutChangingExistingJobs(t *testing.T) {
	s := fixture(t)
	if e := s.Config.Save(); e != nil {
		t.Fatal(e)
	}
	explicit, e := s.newJob(NewArgs{Cwd: t.TempDir(), Kind: "chat", Account: "account-secondary"})
	if e != nil {
		t.Fatal(e)
	}
	auto, e := s.newJob(NewArgs{Cwd: t.TempDir(), Kind: "chat"})
	if e != nil || !auto.AutoAccount || explicit.AutoAccount {
		t.Fatal("selection policy was lost", e)
	}
	if _, _, e = config.Update(s.Config.Path, "", func(c *config.Config) error {
		c.WorkerAccounts.Priority = []string{"account-main", "account-secondary", "account-work", "account-backup"}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	yes := true
	s.Reader = fakeReader{"account-work": {Authenticated: true, Ordinary: &yes}, "account-main": {Authenticated: true, Ordinary: &yes}, "account-secondary": {Authenticated: true, Ordinary: &yes}}
	choice, e := s.initialAccount(context.Background(), auto)
	if e != nil || choice != "account-main" {
		t.Fatal("automatic job ignored the panel's current order", choice, e)
	}
	choice, e = s.initialAccount(context.Background(), explicit)
	if e != nil || choice != "account-secondary" {
		t.Fatal("existing explicit job was changed", choice, e)
	}
	if _, e = s.Handle(context.Background(), Call{Command: "set-worker-account", Args: mustJSON(JobArgs{Account: "account-work"})}); e == nil {
		t.Fatal("retired default worker command was accepted")
	}
	saved, _ := config.Load(s.Config.Path)
	if saved.FirstPriority() != "account-main" || saved.Manager.Account != "account-backup" {
		t.Fatal("unrelated account selection changed")
	}
	got, _ := s.Store.Job(explicit.ID)
	if got.Account != "account-secondary" || got.AutoAccount {
		t.Fatal("existing job was rewritten")
	}
}

func TestUnavailableManualChoicePreservesAutomaticPolicy(t *testing.T) {
	s := fixture(t)
	j, e := s.newJob(NewArgs{Cwd: t.TempDir(), Kind: "chat"})
	if e != nil {
		t.Fatal(e)
	}
	no := false
	s.Reader = fakeReader{"account-secondary": {Authenticated: true, Ordinary: &no}}
	a, e := s.requestAccountChange(JobArgs{ID: j.ID, Account: "account-secondary"})
	if e != nil {
		t.Fatal(e)
	}
	s.execute(a)
	got, _ := s.Store.Job(j.ID)
	if !got.AutoAccount || got.Account != j.Account || got.TargetAccount != "" {
		t.Fatal("failed manual choice changed automatic account policy")
	}
}

func TestOfficialAutomaticPriorityStartsAvailableAccountAndKeepsLiveWorker(t *testing.T) {
	if os.Getenv("CODEXM_NATIVE_TEST") != "1" {
		t.Skip("requires isolated official runtime")
	}
	s := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	s.Config.Runtime.CodexBinary = os.Getenv("CODEXM_NATIVE_RUNTIME")
	s.Self = os.Getenv("CODEXM_BINARY")
	if s.Self == "" {
		t.Fatal("test codexm executable missing")
	}
	if e := s.Config.Save(); e != nil {
		t.Fatal(e)
	}
	r, e := cr.Resolve(ctx, s.Config)
	if e != nil {
		t.Fatal(e)
	}
	s.Resolver = r
	yes, no := true, false
	s.Reader = fakeReader{"account-work": {Authenticated: true, Ordinary: &no}, "account-secondary": {Authenticated: true, Ordinary: &yes}, "account-main": {Authenticated: true, Ordinary: &yes}}
	j, e := s.newJob(NewArgs{Cwd: t.TempDir(), Kind: "chat", ReadOnly: true, Model: "gpt-6.1-sol"})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = s.park(context.Background(), j.ID, false); s.cancel(); s.wg.Wait() }()
	if e = s.ensureRuntime(ctx, j.ID, false); e != nil {
		t.Fatal(e)
	}
	loaded, e := s.Store.Job(j.ID)
	if e != nil || loaded.Account != "account-secondary" || !loaded.AutoAccount || loaded.ThreadID == "" {
		t.Fatal("first exhausted account was not skipped", e)
	}
	if _, _, e = config.Update(s.Config.Path, "", func(c *config.Config) error {
		c.WorkerAccounts.Priority = []string{"account-main", "account-work", "account-secondary", "account-backup"}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e = s.ensureRuntime(ctx, j.ID, false); e != nil {
		t.Fatal(e)
	}
	after, _ := s.Store.Job(j.ID)
	if after.RunID != loaded.RunID || after.ThreadID != loaded.ThreadID || after.Account != "account-secondary" {
		t.Fatal("editing priority replaced a live conversation")
	}
	count, _, _ := s.Store.DecisionCounts("")
	if count != 0 {
		t.Fatal("automatic selection invoked a manager model")
	}
}

func TestAllAccountsExhaustedKeepsBasicCommandsAndQueuedWorkWithoutModel(t *testing.T) {
	s := fixture(t)
	if e := s.Config.Save(); e != nil {
		t.Fatal(e)
	}
	no := false
	s.Reader = fakeReader{}
	for _, name := range s.Config.WorkerAccounts.Priority {
		s.Reader.(fakeReader)[name] = accounts.Snapshot{Authenticated: true, Ordinary: &no}
	}
	j, e := s.newJob(NewArgs{Cwd: t.TempDir(), Kind: "goal", Goal: "fixture objective", Prompt: "fixture input", Model: "gpt-6.1-sol"})
	if e != nil {
		t.Fatal(e)
	}
	budget := int64(30000)
	j.Goal.TokenBudget, j.Goal.TokensUsed = &budget, 123
	if e = s.Store.PutJob(j); e != nil {
		t.Fatal(e)
	}
	choice, e := s.initialAccount(context.Background(), j)
	var p *postpone
	if choice != "" || !errors.As(e, &p) {
		t.Fatal("exhausted accounts attempted a worker", choice, e)
	}
	for _, command := range []string{"status", "logs", "usage", "settings-edit"} {
		if _, e = s.Handle(context.Background(), Call{Command: command}); e != nil {
			t.Fatal(command, e)
		}
	}
	if _, e = s.Handle(context.Background(), Call{Command: "send", Args: mustJSON(JobArgs{ID: j.ID, Text: "queued followup"})}); e != nil {
		t.Fatal(e)
	}
	got, _ := s.Store.Job(j.ID)
	if got.State != "quota_wait" || got.ThreadID != j.ThreadID || got.Goal.TokenBudget == nil || *got.Goal.TokenBudget != budget || got.Goal.TokensUsed != 123 || got.Model != j.Model || got.LastInput != "queued followup" {
		t.Fatal("waiting work was changed")
	}
	count, _, _ := s.Store.DecisionCounts("")
	if count != 0 {
		t.Fatal("quota check consumed a manager call")
	}
	before, _ := s.Store.Actions("pending")
	if len(before) != 3 {
		t.Fatal("work queue was lost", len(before))
	}
	yes := true
	if e = s.recordAccount(accounts.Snapshot{Account: "account-secondary", Authenticated: true, Ordinary: &yes, CheckedAt: time.Now()}); e != nil {
		t.Fatal(e)
	}
	choice, e = s.initialAccount(context.Background(), got)
	if e != nil || choice != "account-secondary" {
		t.Fatal("recovered account was not selected", choice, e)
	}
	after, _ := s.Store.Actions("pending")
	if len(after) != len(before) {
		t.Fatal("recovery changed work or used a model")
	}
}

func TestManualSessionChoiceDisablesAutomaticAccountSelection(t *testing.T) {
	s := fixture(t)
	j, e := s.newJob(NewArgs{Cwd: t.TempDir(), Kind: "chat"})
	if e != nil || !j.AutoAccount {
		t.Fatal(e)
	}
	yes := true
	s.Reader = fakeReader{"account-work": {Authenticated: true, Ordinary: &yes}}
	a, e := s.requestAccountChange(JobArgs{ID: j.ID, Account: j.Account})
	if e != nil {
		t.Fatal(e)
	}
	s.execute(a)
	got, _ := s.Store.Job(j.ID)
	if got.AutoAccount {
		t.Fatal("manual same-account selection retained automatic policy")
	}
}
