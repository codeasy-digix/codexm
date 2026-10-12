package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
)

// Quota monitoring reads metadata, never starts a model. Opt-in is kept in the
// configuration; the existing .home notification helper owns Discord credentials.
func (s *Server) quotaNotificationLoop() {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
			c, e := config.Load(s.Config.Path)
			if e == nil && c.Notifications.FiveHourDiscord {
				s.notifyQuotas(c)
				s.deliverQuotaNotices()
			}
			timer.Reset(time.Minute)
		}
	}
}
func (s *Server) notifyQuotas(c config.Config) {
	names := slices.Clone(c.Notifications.Accounts)
	if len(names) == 0 {
		var e error
		names, e = accounts.List(c)
		if e != nil {
			return
		}
	}
	slices.Sort(names)
	names = slices.Compact(names)
	for _, name := range names {
		if s.ctx.Err() != nil {
			return
		}
		if !config.Nickname.MatchString(name) {
			continue
		}
		// Reuse fresh worker observations; query the other saved accounts as needed.
		observed, _, e := s.snapshots()
		if e != nil {
			return
		}
		a := observed[name]
		if a.Account == "" || a.Error != "" || time.Since(a.CheckedAt) < 0 || time.Since(a.CheckedAt) > time.Minute {
			ctx, cancel := context.WithTimeout(s.ctx, 40*time.Second)
			a = s.Reader.Read(ctx, name)
			cancel()
			if e = s.recordAccount(a); e != nil {
				return
			}
		}
		s.queueQuotaNotice(a)
	}
}

func (s *Server) queueQuotaNotice(a accounts.Snapshot) {
	c, e := config.Load(s.Config.Path)
	if e != nil || !c.Notifications.FiveHourDiscord {
		return
	}
	include := slices.Contains(c.Notifications.Accounts, a.Account)
	if len(c.Notifications.Accounts) == 0 {
		include = true
	}
	if !include {
		return
	}
	key, message, ok := fiveHourNotice(a, time.Now())
	if !ok {
		return
	}
	// Record while this account is still assigned to the affected job. A fast
	// automatic account switch must not erase the pending notification.
	_, _ = s.Store.DB.Exec("INSERT OR IGNORE INTO quota_notifications(event_key,account,at,state,message) VALUES(?,?,?,'pending',?)", key, a.Account, time.Now().Unix(), message)
}

func (s *Server) recordQuotaUpdate(account string, params json.RawMessage) {
	var update struct {
		Limits *accounts.Bucket `json:"rateLimits"`
	}
	if json.Unmarshal(params, &update) != nil || update.Limits == nil {
		return
	}
	// A slow authenticated refresh must not stall the worker event stream. Keep
	// the notification durable even while that refresh owns the snapshot writer.
	locked := s.accountMu.TryLock()
	if locked {
		defer s.accountMu.Unlock()
	}
	snapshots, _, e := s.snapshots()
	if e != nil {
		return
	}
	a := snapshots[account]
	a.Account = account
	next := *update.Limits
	old := a.Single
	if b, ok := a.Buckets[next.ID]; ok {
		old = b
	}
	mergeWindow := func(previous, next *accounts.Window) *accounts.Window {
		if next == nil {
			return previous
		}
		if previous == nil {
			return next
		}
		copy := *next
		if copy.Minutes == nil {
			copy.Minutes = previous.Minutes
		}
		if copy.ResetsAt == nil {
			copy.ResetsAt = previous.ResetsAt
		}
		return &copy
	}
	next.Primary = mergeWindow(old.Primary, next.Primary)
	next.Secondary = mergeWindow(old.Secondary, next.Secondary)
	if next.ID == "" {
		next.ID = old.ID
	}
	if next.Name == "" {
		next.Name = old.Name
	}
	if next.NormalModel == "" {
		next.NormalModel = old.NormalModel
	}
	if next.Reached == "" {
		next.Reached = old.Reached
	}
	if next.SpendReached == nil {
		next.SpendReached = old.SpendReached
	}
	if next.ID != "" {
		if a.Buckets == nil {
			a.Buckets = map[string]accounts.Bucket{}
		}
		a.Buckets[next.ID] = next
	}
	if a.Single.ID == next.ID || a.Single.ID == "" || next.ID == "codex" {
		a.Single = next
	}
	a.CheckedAt = time.Now()
	if a.Authenticated {
		a.Error = ""
	}
	s.queueQuotaNotice(a)
	if locked {
		_ = s.Store.Snapshot(account, a)
	}
}
func (s *Server) deliverQuotaNotices() {
	rows, e := s.Store.DB.Query("SELECT event_key,account,message FROM quota_notifications WHERE state='pending' ORDER BY at LIMIT 20")
	if e != nil {
		return
	}
	type notice struct{ key, account, message string }
	pending := []notice{}
	for rows.Next() {
		var n notice
		if e = rows.Scan(&n.key, &n.account, &n.message); e != nil {
			rows.Close()
			return
		}
		pending = append(pending, n)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return
	}
	for _, n := range pending {
		if s.ctx.Err() != nil {
			return
		}
		s.sendQuotaNotice(n.account, n.key, n.message)
	}
}
func fiveHourNotice(a accounts.Snapshot, now time.Time) (string, string, bool) {
	if !a.Authenticated || a.Error != "" {
		return "", "", false
	}
	b := a.Single
	if v, ok := a.Buckets["codex"]; ok {
		b = v
	}
	w := b.Primary
	// Do not mistake weekly exhaustion, unknown quota or a stale pre-reset
	// observation for a newly exhausted 5-hour window.
	if w == nil || w.Minutes == nil || *w.Minutes != 300 || w.UsedPercent < 100 || w.ResetsAt == nil || *w.ResetsAt <= now.Unix() {
		return "", "", false
	}
	key := fmt.Sprintf("five-hour:%s:%d", a.Account, *w.ResetsAt)
	weekly := "미확인"
	if b.Secondary != nil {
		weekly = fmt.Sprintf("%d%%", max(0, 100-b.Secondary.UsedPercent))
	}
	reset := time.Unix(*w.ResetsAt, 0).Local().Format("2006-01-02 15:04 MST")
	message := fmt.Sprintf("계정 %s의 5시간 한도 소진을 관측했습니다 (%s).\n관측 당시 5시간 잔여: 0%% · 장기 잔여: %s\n한도 초기화: %s\ncodexm의 계정 전환 정책은 계속 적용됩니다.", a.Account, now.Local().Format("01/02 15:04 MST"), weekly, reset)
	return key, message, true
}
func (s *Server) sendQuotaNotice(account, key, message string) {
	home, e := os.UserHomeDir()
	if e != nil {
		return
	}
	helper := filepath.Join(home, ".local", "bin", "agent_job_done")
	if st, e := os.Stat(helper); e != nil || !st.Mode().IsRegular() || st.Mode()&0111 == 0 {
		fmt.Fprintln(os.Stderr, "codexm: Discord quota alert helper is unavailable; configure ~/.local/bin/agent_job_done")
		return
	}
	// Claim before launching the sender. Discord has no delivery idempotency key;
	// an uncertain delivery is not automatically repeated after a restart.
	r, e := s.Store.DB.Exec("UPDATE quota_notifications SET state='sending' WHERE event_key=? AND state='pending'", key)
	if e != nil {
		return
	}
	n, e := r.RowsAffected()
	if e != nil || n == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 90*time.Second)
	defer cancel()
	// Sender output could contain webhook diagnostics; keep it out of shared logs.
	result := "sent"
	message += "\n\n" + s.notificationStatus()
	for i, part := range discordParts(message) {
		call, done := context.WithTimeout(ctx, 25*time.Second)
		cmd := exec.CommandContext(call, helper, fmt.Sprintf("codexm · 5h 한도 · %d", i+1), part)
		e = cmd.Run()
		done()
		if e != nil {
			result = "delivery_unconfirmed"
			break
		}
	}
	_, _ = s.Store.DB.Exec("UPDATE quota_notifications SET state=? WHERE event_key=?", result, key)
	fmt.Fprintf(os.Stderr, "codexm: five-hour Discord alert account=%s delivery=%s\n", account, result)
}

// Report metadata only: no credentials, goal objectives or conversation text.
func (s *Server) notificationStatus() string {
	var out strings.Builder
	fmt.Fprintf(&out, "전체 계정 / 현재 관리 상태 (%s)\n", time.Now().Local().Format("01/02 15:04 MST"))
	c, e := config.Load(s.Config.Path)
	if e != nil {
		return "현재 설정 조회 미확인"
	}
	names, e := accounts.List(c)
	if e != nil {
		return "전체 계정 목록 조회 미확인"
	}
	snapshots, _, e := s.snapshots()
	if e != nil {
		return "현재 한도 관측 조회 미확인"
	}
	for _, name := range names {
		a := snapshots[name]
		b := a.Single
		if v, ok := a.Buckets["codex"]; ok {
			b = v
		}
		if !a.Authenticated || a.Error != "" {
			fmt.Fprintf(&out, "• %s: 조회 미확인\n", name)
			continue
		}
		fmt.Fprintf(&out, "• %s: 단기 잔여 %s / 장기 잔여 %s\n  초기화 %s / %s · 관측 %s\n", name, quotaRemaining(b.Primary), quotaRemaining(b.Secondary), quotaReset(b.Primary), quotaReset(b.Secondary), a.CheckedAt.Local().Format("15:04"))
	}
	fmt.Fprintf(&out, "관리자 %s · 기본 작업 계정 %s · codexm %s\n", c.Manager.Account, c.DefaultWorker(), config.Version)
	js, e := s.Store.Jobs()
	if e != nil {
		return out.String() + "작업 상태 조회 미확인"
	}
	fmt.Fprintf(&out, "등록 작업 %d개:\n", len(js))
	for _, j := range js {
		id := j.ID
		if len(id) > 8 {
			id = id[:8]
		}
		fmt.Fprintf(&out, "• %s · %s · %s", id, j.Account, notificationState(j.State))
		if j.TargetAccount != "" {
			fmt.Fprintf(&out, " → %s", j.TargetAccount)
		}
		out.WriteByte('\n')
	}
	return strings.TrimSpace(out.String())
}
func quotaRemaining(w *accounts.Window) string {
	if w == nil {
		return "미확인"
	}
	return fmt.Sprintf("%d%%", max(0, min(100, 100-w.UsedPercent)))
}
func quotaReset(w *accounts.Window) string {
	if w == nil || w.ResetsAt == nil {
		return "미확인"
	}
	return time.Unix(*w.ResetsAt, 0).Local().Format("01/02 15:04")
}
func notificationState(v string) string {
	switch v {
	case "running":
		return "실행 중"
	case "idle":
		return "대기"
	case "paused":
		return "일시중지"
	case "complete":
		return "완료"
	case "stopped":
		return "중지"
	case "quota_wait":
		return "한도 대기"
	case "switching":
		return "계정 전환 중"
	case "external_writer":
		return "다른 Codex에서 실행"
	case "error":
		return "오류"
	default:
		return v
	}
}
func discordParts(message string) []string {
	const limit = 1700
	parts := []string{}
	current := []rune{}
	for _, line := range strings.Split(message, "\n") {
		runes := []rune(line + "\n")
		for len(runes) > 0 {
			space := limit - len(current)
			n := min(space, len(runes))
			current = append(current, runes[:n]...)
			runes = runes[n:]
			if len(current) == limit {
				parts = append(parts, string(current))
				current = nil
			}
		}
	}
	if len(current) > 0 {
		parts = append(parts, string(current))
	}
	return parts
}
