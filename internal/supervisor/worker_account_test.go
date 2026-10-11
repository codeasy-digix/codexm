package supervisor

import (
	"context"
	"testing"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
)

func TestWorkerSelectionPersistsWithoutChangingExistingJobsOrManager(t *testing.T) {
	s := fixture(t)
	if e := s.Config.Save(); e != nil {
		t.Fatal(e)
	}
	j, e := s.newJob(NewArgs{Cwd: t.TempDir(), Kind: "chat", Account: "account-secondary"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Handle(context.Background(), Call{Command: "set-worker-account", Args: mustJSON(JobArgs{Account: "new-login"})}); e != nil {
		t.Fatal(e)
	}
	if s.defaultWorker() != "new-login" || s.workerConfig().WorkerAccounts.Priority[3] != "new-login" {
		t.Fatal("running supervisor did not observe the saved selection")
	}
	got, _ := s.Store.Job(j.ID)
	if got.Account != "account-secondary" || s.manager() != "account-backup" {
		t.Fatal("selection changed an existing job or manager")
	}
	yes := true
	s.Reader = fakeReader{"account-work": accounts.Snapshot{Authenticated: true, Ordinary: &yes}}
	if _, e = s.Handle(context.Background(), Call{Command: "set-manager", Args: mustJSON(JobArgs{Account: "account-work"})}); e != nil {
		t.Fatal(e)
	}
	saved, e := config.Load(s.Config.Path)
	if e != nil || saved.DefaultWorker() != "new-login" || saved.Manager.Account != "account-work" {
		t.Fatal("manager save overwrote worker selection", e)
	}
	next, e := s.newJob(NewArgs{Cwd: t.TempDir(), Kind: "chat"})
	if e != nil || next.Account != "new-login" {
		t.Fatal("new job did not use saved worker selection", e)
	}
}
