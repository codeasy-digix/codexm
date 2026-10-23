package supervisor

import (
	"context"
	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"testing"
	"time"
)

type countedAccountReader struct{ Calls int }

func (r *countedAccountReader) Read(context.Context, string) accounts.Snapshot {
	r.Calls++
	return accounts.Snapshot{Error: "fixture refuses network authentication"}
}

func TestViewerReusesOnlyRecentKnownPrimaryQuota(t *testing.T) {
	for _, tc := range []struct {
		Name, Account       string
		Age                 time.Duration
		Blocked, WantCached bool
		WantCalls           int
	}{
		{"recent primary", "account-work", time.Second, false, true, 0},
		{"expired", "account-work", 2 * time.Minute, false, false, 3},
		{"previously exhausted", "account-work", time.Second, true, false, 3},
		{"fallback always checks primaries", "account-backup", time.Second, false, false, 3},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			s := fixture(t)
			r := &countedAccountReader{}
			s.Reader = r
			yes := true
			snap := accounts.Snapshot{Account: tc.Account, CheckedAt: time.Now().Add(-tc.Age), Authenticated: true, Ordinary: &yes, WasExhausted: tc.Blocked}
			if e := s.Store.Snapshot(tc.Account, snap); e != nil {
				t.Fatal(e)
			}
			j := jobs.Job{ID: jobs.ID(), Account: tc.Account, Kind: "chat", State: "queued"}
			if e := s.Store.PutJob(j); e != nil {
				t.Fatal(e)
			}
			name, e := s.priorityAccount(context.Background(), j, false)
			if tc.WantCached {
				if e != nil || name != tc.Account || r.Calls != 0 {
					t.Fatal("cached opening performed another authentication", name, e, r.Calls)
				}
			} else if r.Calls != tc.WantCalls || e == nil {
				t.Fatal("unknown/stale/fallback quota bypassed verification", e, r.Calls)
			}
		})
	}
}
