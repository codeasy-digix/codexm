package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
)

func TestFiveHourNoticeRequiresKnownCurrentFiveHourExhaustion(t *testing.T) {
	now := time.Now()
	duration := 300
	reset := now.Add(time.Hour).Unix()
	base := accounts.Snapshot{Account: "account-work", Authenticated: true, CheckedAt: now, Single: accounts.Bucket{Primary: &accounts.Window{UsedPercent: 100, Minutes: &duration, ResetsAt: &reset}}}
	if _, _, ok := fiveHourNotice(base, now); !ok {
		t.Fatal("valid five-hour exhaustion ignored")
	}
	for _, mutate := range []func(*accounts.Snapshot){
		func(a *accounts.Snapshot) { a.Authenticated = false }, func(a *accounts.Snapshot) { a.Error = "unknown" }, func(a *accounts.Snapshot) { a.Single.Primary.Minutes = nil }, func(a *accounts.Snapshot) { week := 10080; a.Single.Primary.Minutes = &week }, func(a *accounts.Snapshot) { a.Single.Primary.UsedPercent = 99 }, func(a *accounts.Snapshot) { a.Single.Primary.ResetsAt = nil }, func(a *accounts.Snapshot) { old := now.Add(-time.Second).Unix(); a.Single.Primary.ResetsAt = &old },
	} {
		w := *base.Single.Primary
		a := base
		a.Single.Primary = &w
		mutate(&a)
		if _, _, ok := fiveHourNotice(a, now); ok {
			t.Fatal("unknown/stale/non-five-hour quota triggered alert")
		}
	}
}

func TestDiscordAlertDurableDedupeIncludesAllAccountsAndJobsAndHidesContent(t *testing.T) {
	s := fixture(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	out := filepath.Join(home, "sent.txt")
	helper := filepath.Join(home, ".local", "bin", "agent_job_done")
	if e := config.AtomicWrite(helper, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$HOME/sent.txt\"\n"), 0700); e != nil {
		t.Fatal(e)
	}
	s.Config.Notifications.FiveHourDiscord = true
	if e := s.Config.Save(); e != nil {
		t.Fatal(e)
	}
	yes := true
	duration := 300
	reset := time.Now().Add(3 * time.Hour).Unix()
	reader := fakeReader{}
	for _, name := range s.Config.WorkerAccounts.Priority {
		reader[name] = accounts.Snapshot{Authenticated: true, Ordinary: &yes, Single: accounts.Bucket{Primary: &accounts.Window{UsedPercent: 35, Minutes: &duration, ResetsAt: &reset}}}
	}
	a := reader["account-work"]
	a.Single.Primary = &accounts.Window{UsedPercent: 100, Minutes: &duration, ResetsAt: &reset}
	reader["account-work"] = a
	s.Reader = reader
	for _, state := range []string{"running", "paused"} {
		j := jobs.Job{ID: jobs.ID(), Account: "account-work", State: state, Goal: &jobs.Goal{Objective: "DO_NOT_SEND_GOAL"}, LastInput: "DO_NOT_SEND_INPUT", LastOutput: "DO_NOT_SEND_OUTPUT"}
		s.Store.PutJob(j)
	}
	s.notifyQuotas(s.Config)
	s.notifyQuotas(s.Config)
	var count int
	s.Store.DB.QueryRow("SELECT COUNT(*) FROM quota_notifications").Scan(&count)
	if count != 1 {
		t.Fatal("duplicate notice queue", count)
	}
	s.deliverQuotaNotices()
	first, e := os.ReadFile(out)
	if e != nil {
		t.Fatal(e)
	}
	text := string(first)
	for _, value := range []string{"account-work", "account-secondary", "account-main", "account-backup", "등록 작업 2개", "실행 중", "일시중지", "5시간 잔여: 0%"} {
		if !strings.Contains(text, value) {
			t.Fatal("status omitted", value)
		}
	}
	for _, value := range []string{"DO_NOT_SEND_GOAL", "DO_NOT_SEND_INPUT", "DO_NOT_SEND_OUTPUT"} {
		if strings.Contains(text, value) {
			t.Fatal("private contents in notification")
		}
	}
	s.deliverQuotaNotices()
	second, _ := os.ReadFile(out)
	if string(first) != string(second) {
		t.Fatal("notice sent twice")
	}
	var delivery string
	s.Store.DB.QueryRow("SELECT state FROM quota_notifications").Scan(&delivery)
	if delivery != "sent" {
		t.Fatal(delivery)
	}
	s.Config.Notifications.Accounts = []string{"account-secondary"}
	s.Config.Save()
	s.queueQuotaNotice(reader.Read(context.Background(), "account-main"))
	s.Store.DB.QueryRow("SELECT COUNT(*) FROM quota_notifications").Scan(&count)
	if count != 1 {
		t.Fatal("out-of-scope notice")
	}
}

func TestDiscordUncertainDeliveryIsNotAutomaticallyRetried(t *testing.T) {
	s := fixture(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	helper := filepath.Join(home, ".local", "bin", "agent_job_done")
	config.AtomicWrite(helper, []byte("#!/bin/sh\nprintf failed >> \"$HOME/attempts\"\nexit 1\n"), 0700)
	s.Store.DB.Exec("INSERT INTO quota_notifications VALUES('one','account-work',1,'pending','fixture')")
	s.deliverQuotaNotices()
	s.deliverQuotaNotices()
	b, e := os.ReadFile(filepath.Join(home, "attempts"))
	if e != nil || string(b) != "failed" {
		t.Fatal("uncertain delivery retried")
	}
	var state string
	s.Store.DB.QueryRow("SELECT state FROM quota_notifications").Scan(&state)
	if state != "delivery_unconfirmed" {
		t.Fatal(state)
	}
	for _, part := range discordParts(strings.Repeat("한글", 3000)) {
		if len([]rune(part)) > 1700 {
			t.Fatal("Discord payload exceeds limit")
		}
	}
}
