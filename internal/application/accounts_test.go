package application

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/store"
	"golang.org/x/sys/unix"
)

func offlineRemovalFixture(t *testing.T) (config.Config, accounts.RemoveArgs) {
	t.Helper()
	r := t.TempDir()
	c := config.Defaults()
	c.Path = filepath.Join(r, "config.toml")
	c.Storage.StateDir = filepath.Join(r, "state")
	c.Runtime.AccountsDir = filepath.Join(r, "accounts")
	c.Runtime.SharedCodexHome = filepath.Join(r, "shared")
	c.Runtime.EnginesDir = filepath.Join(r, "engines")
	if err := cr.EnsureAccountHome(c, "account-secondary"); err != nil {
		t.Fatal(err)
	}
	if err := config.AtomicWrite(filepath.Join(c.Runtime.AccountsDir, "account-secondary", "auth.json"), []byte("synthetic-login"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	_, rev, _ := config.Current(c.Path)
	return c, accounts.RemoveArgs{Account: "account-secondary", Revision: rev}
}

func TestOfflineAccountRemovalPreservesHistoricalJobsAndRejectsLiveServiceLease(t *testing.T) {
	for _, mode := range []string{"offline", "lease", "worker", "corrupt", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			c, p := offlineRemovalFixture(t)
			st, err := store.Open(c.StateDir())
			if err != nil {
				t.Fatal(err)
			}
			j := jobs.Job{ID: "history", State: "complete", Managed: true, Account: "account-secondary", ThreadID: "same-thread", Goal: &jobs.Goal{Status: "complete", TokensUsed: 21}}
			if mode == "worker" {
				j.State = "paused"
				j.Paused = true
			}
			if err = st.PutJob(j); err != nil {
				t.Fatal(err)
			}
			st.Close()
			if mode == "corrupt" {
				if err = os.WriteFile(filepath.Join(c.StateDir(), "state.sqlite"), []byte("invalid database"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "lease" {
				fd, err := unix.Open(filepath.Join(c.StateDir(), "supervisor.lock"), unix.O_CREAT|unix.O_RDWR, 0600)
				if err != nil {
					t.Fatal(err)
				}
				defer unix.Close(fd)
				if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			before, _ := os.ReadFile(c.Path)
			_, err = RemoveAccount(ctx, c, p, "offline-remove-operation")
			if mode != "offline" {
				after, _ := os.ReadFile(c.Path)
				if err == nil || string(after) != string(before) {
					t.Fatal("unsafe offline removal changed state", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			loaded, _ := config.Load(c.Path)
			names, _ := accounts.List(loaded)
			if slices.Contains(names, "account-secondary") {
				t.Fatal("removed alias still listed")
			}
			st, err = store.ReadOnly(c.StateDir())
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			got, err := st.Job(j.ID)
			if err != nil || got.ThreadID != j.ThreadID || got.Goal.TokensUsed != 21 {
				t.Fatal("historical conversation changed", err)
			}
		})
	}
}
