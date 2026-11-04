package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

type Window struct {
	UsedPercent int    `json:"usedPercent"`
	Minutes     *int   `json:"windowDurationMins"`
	ResetsAt    *int64 `json:"resetsAt"`
}
type Bucket struct {
	ID           string  `json:"limitId"`
	Name         string  `json:"limitName"`
	NormalModel  string  `json:"normalModelSlug"`
	Primary      *Window `json:"primary"`
	Secondary    *Window `json:"secondary"`
	Reached      string  `json:"rateLimitReachedType"`
	SpendReached *bool   `json:"spendControlReached"`
}
type Snapshot struct {
	Email         string            `json:"email,omitempty"`
	ResetCredits  json.RawMessage   `json:"rateLimitResetCredits,omitempty"`
	Account       string            `json:"account"`
	AccountID     string            `json:"accountId"`
	Plan          string            `json:"plan"`
	Ordinary      *bool             `json:"ordinaryUsageAllowed"`
	Single        Bucket            `json:"rateLimits"`
	Buckets       map[string]Bucket `json:"rateLimitsByLimitId"`
	CheckedAt     time.Time         `json:"checked_at"`
	Error         string            `json:"error,omitempty"`
	Authenticated bool              `json:"authenticated"`
	WasExhausted  bool              `json:"was_exhausted"`
}
type Reader struct {
	Resolver cr.Resolver
	mu       sync.Mutex
	locks    map[string]*sync.Mutex
}

func New(r cr.Resolver) *Reader { return &Reader{Resolver: r, locks: map[string]*sync.Mutex{}} }

// SavedLogins includes native logins even when their codexm worker registration
// has been removed. Native account selection never restores worker policy.
func SavedLogins(c config.Config) ([]string, error) { return storedNames(c) }

func List(c config.Config) ([]string, error) {
	names, err := storedNames(c)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(names, func(name string) bool { return slices.Contains(c.AccountRegistry.Removed, name) }), nil
}

func storedNames(c config.Config) ([]string, error) {
	ds, e := os.ReadDir(config.Expand(c.Runtime.AccountsDir))
	if e != nil {
		return nil, e
	}
	out := []string{}
	for _, d := range ds {
		if d.IsDir() && config.Nickname.MatchString(d.Name()) {
			if info, e := os.Stat(filepath.Join(config.Expand(c.Runtime.AccountsDir), d.Name(), "auth.json")); e == nil && info.Mode().IsRegular() {
				out = append(out, d.Name())
			}
		}
	}
	sort.Strings(out)
	return out, nil
}
func (r *Reader) Read(ctx context.Context, name string) Snapshot {
	s := Snapshot{Account: name, CheckedAt: time.Now()}
	if !config.Nickname.MatchString(name) {
		s.Error = "invalid account nickname"
		return s
	}
	r.mu.Lock()
	lock := r.locks[name]
	if lock == nil {
		lock = &sync.Mutex{}
		r.locks[name] = lock
	}
	r.mu.Unlock()
	lock.Lock()
	defer lock.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	probeName := name
	if probeName == "default" {
		probeName = ""
	}
	p, e := r.Resolver.Probe(ctx, probeName)
	if e != nil {
		s.Error = cr.SafeText(e.Error())
		return s
	}
	defer p.Close()
	return readAccountUsage(ctx, p, s)
}

type usageCaller interface {
	Call(context.Context, string, any, any) error
}

func readAccountUsage(ctx context.Context, p usageCaller, s Snapshot) Snapshot {
	var a struct {
		Account *struct {
			Type  string `json:"type"`
			Plan  string `json:"planType"`
			Email string `json:"email"`
		} `json:"account"`
	}
	// Do not force an auth refresh just to inspect usage. Some CLI versions
	// return account:null for refreshToken=true even while the authenticated
	// rate-limit RPC succeeds. The subsequent RPC supplies live usage/error data.
	if e := p.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &a); e != nil {
		s.Error = cr.SafeText(e.Error())
		return s
	}
	if a.Account == nil || a.Account.Type != "chatgpt" {
		s.Error = "ChatGPT login is required"
		return s
	}
	s.Authenticated = true
	s.Plan = a.Account.Plan
	s.Email = a.Account.Email
	if e := p.Call(ctx, "account/rateLimits/read", map[string]any{}, &s); e != nil {
		s.Error = cr.SafeText(e.Error())
	}
	return s
}
func (s Snapshot) State(model string) string {
	if s.Error != "" || !s.Authenticated {
		return "unknown"
	}
	if s.Ordinary != nil && !*s.Ordinary {
		return "exhausted"
	}
	b := s.Single
	if x, ok := s.Buckets["codex"]; ok {
		b = x
	}
	for _, x := range s.Buckets {
		if x.NormalModel == model && model != "" {
			b = x
			break
		}
	}
	if b.Reached != "" || b.SpendReached != nil && *b.SpendReached {
		return "exhausted"
	}
	for _, w := range []*Window{b.Primary, b.Secondary} {
		if w != nil && w.UsedPercent >= 100 {
			return "exhausted"
		}
	}
	if s.Ordinary != nil && *s.Ordinary {
		return "available"
	}
	if b.Primary != nil || b.Secondary != nil {
		return "candidate"
	}
	return "unknown"
}
func Choose(c config.Config, snapshots map[string]Snapshot, model string, blocked map[string]bool) (string, string) {
	allExhausted := true
	for _, name := range c.WorkerAccounts.Priority {
		if name == c.WorkerAccounts.FallbackAccount {
			continue
		}
		s, ok := snapshots[name]
		state := "unknown"
		if ok {
			state = s.State(model)
		}
		if state != "exhausted" {
			allExhausted = false
		}
		if state == "available" || state == "candidate" && !blocked[name] {
			return name, ""
		}
	}
	f := c.WorkerAccounts.FallbackAccount
	if f != "" && (!c.WorkerAccounts.FallbackRequiresAllPrimaryExhausted || allExhausted) {
		s, ok := snapshots[f]
		if ok {
			state := s.State(model)
			if state == "available" || state == "candidate" && !blocked[f] {
				return f, ""
			}
		}
	}
	if allExhausted && (f == "" || snapshots[f].State(model) == "exhausted") {
		return "", "all worker accounts are exhausted"
	}
	return "", "account availability is unknown; refresh is required"
}
func Describe(s Snapshot) string {
	st := s.State("")
	if s.Error != "" {
		return fmt.Sprintf("%s: %s (%s)", s.Account, st, s.Error)
	}
	parts := []string{s.Account + ": " + st}
	b := s.Single
	if x, ok := s.Buckets["codex"]; ok {
		b = x
	}
	for _, w := range []*Window{b.Primary, b.Secondary} {
		if w != nil {
			parts = append(parts, fmt.Sprintf("%d%% remaining", max(0, 100-w.UsedPercent)))
		}
	}
	return strings.Join(parts, " · ")
}
