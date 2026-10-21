package supervisor

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
)

func TestAccountRemovalIsDurableAndHiddenFromLiveQuotaRefresh(t *testing.T) {
	s := fixture(t)
	if err := s.Config.Save(); err != nil {
		t.Fatal(err)
	}
	_, rev, _ := config.Current(s.Config.Path)
	request := WebOperation{ID: "remove-account-operation", Command: "account-remove", Args: mustJSON(accounts.RemoveArgs{Account: "account-secondary", Revision: rev})}
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			_, err := s.webOperation(context.Background(), mustJSON(request))
			if err != nil && !strings.Contains(err.Error(), "outcome_unknown") {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	result, err := s.webOperation(context.Background(), mustJSON(request))
	if err != nil {
		t.Fatal(err)
	}
	var removed accounts.Removal
	if err = json.Unmarshal(result.(json.RawMessage), &removed); err != nil || removed.Account != "account-secondary" {
		t.Fatal("bad replay result", err)
	}
	loaded, _ := config.Load(s.Config.Path)
	if !slices.Equal(loaded.AccountRegistry.Removed, []string{"account-secondary"}) {
		t.Fatal("removal repeated or lost")
	}
	observed, err := s.refreshAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range observed {
		if a.Account == "account-secondary" {
			t.Fatal("cached config resurrected removed account")
		}
	}
	request.Args = mustJSON(accounts.RemoveArgs{Account: "account-main", Revision: rev})
	if _, err = s.webOperation(context.Background(), mustJSON(request)); err == nil || !strings.Contains(err.Error(), "operation_conflict") {
		t.Fatal("operation id reused for another alias", err)
	}
}

func TestAccountRemovalAndJobCreationCannotPassEachOther(t *testing.T) {
	for range 10 {
		s := fixture(t)
		if err := s.Config.Save(); err != nil {
			t.Fatal(err)
		}
		_, rev, _ := config.Current(s.Config.Path)
		var wg sync.WaitGroup
		wg.Go(func() { _, _ = s.removeAccount(accounts.RemoveArgs{Account: "account-secondary", Revision: rev}) })
		for range 3 {
			wg.Go(func() { _, _ = s.newJob(NewArgs{Cwd: t.TempDir(), Kind: "chat", Account: "account-secondary"}) })
		}
		wg.Wait()
		loaded, _ := config.Load(s.Config.Path)
		if slices.Contains(loaded.AccountRegistry.Removed, "account-secondary") {
			js, err := s.Store.Jobs()
			if err != nil {
				t.Fatal(err)
			}
			for _, j := range js {
				if j.Account == "account-secondary" {
					t.Fatal("new work retained a removed account")
				}
			}
		}
	}
}

type removedAccountReader struct{ calls int }

func (r *removedAccountReader) Read(context.Context, string) accounts.Snapshot {
	r.calls++
	return accounts.Snapshot{}
}

func TestRemovedAliasIsExcludedFromExplicitQuotaNotifications(t *testing.T) {
	s := fixture(t)
	s.Config.Notifications.FiveHourDiscord = true
	s.Config.Notifications.Accounts = []string{"account-secondary"}
	if err := s.Config.Save(); err != nil {
		t.Fatal(err)
	}
	_, rev, _ := config.Current(s.Config.Path)
	if _, err := s.removeAccount(accounts.RemoveArgs{Account: "account-secondary", Revision: rev}); err != nil {
		t.Fatal(err)
	}
	latest, _ := config.Load(s.Config.Path)
	reader := &removedAccountReader{}
	s.Reader = reader
	s.notifyQuotas(latest)
	if reader.calls != 0 {
		t.Fatal("explicit notification selection still probed removed alias")
	}
	if !slices.Equal(latest.Notifications.Accounts, []string{"account-secondary"}) {
		t.Fatal("notification scope was expanded to all accounts")
	}
}

func TestRemovalAndAutomaticQuotaSelectionCannotAssignAnUnregisteredAlias(t *testing.T) {
	for range 10 {
		s := fixture(t)
		if err := s.Config.Save(); err != nil {
			t.Fatal(err)
		}
		j := jobs.Job{ID: "starting", Account: "account-work", AutoAccount: true, State: "starting", Managed: true}
		if err := s.Store.PutJob(j); err != nil {
			t.Fatal(err)
		}
		_, rev, _ := config.Current(s.Config.Path)
		var wg sync.WaitGroup
		wg.Go(func() { _, _ = s.removeAccount(accounts.RemoveArgs{Account: "account-secondary", Revision: rev}) })
		wg.Go(func() { _ = s.changeAccount(j.ID, "account-secondary", "previous quota selection") })
		wg.Wait()
		c, _ := config.Load(s.Config.Path)
		got, err := s.Store.Job(j.ID)
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(c.AccountRegistry.Removed, "account-secondary") && got.Account == "account-secondary" {
			t.Fatal("stale quota selection assigned an unregistered account")
		}
	}
}
