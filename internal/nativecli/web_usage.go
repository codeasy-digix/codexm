package nativecli

import (
	"context"
	"sync"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
)

type webLimits struct {
	FiveHour *int `json:"fiveHour"`
	Week     *int `json:"week"`
	Loading  bool `json:"loading"`
}

type webUsageEntry struct {
	limits    webLimits
	checkedAt time.Time
}

type webUsageCache struct {
	mu      sync.Mutex
	entries map[string]webUsageEntry
	pending int
}

// The bridge serves one request at a time. Keep the passive official quota
// probe outside that request loop and share it across tabs for the same login.
func (w *WebChat) usage(account string) (webLimits, error) {
	if account == "" {
		account = "default"
	}
	c := w.c
	if account != "default" {
		if err := requireRegisteredAccount(c, account); err != nil {
			return webLimits{}, err
		}
	}
	revision := accounts.InspectIdentity(c, account).Revision
	cache := &w.usageCache
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if entry, ok := cache.entries[revision]; ok && (entry.limits.Loading || time.Since(entry.checkedAt) < time.Minute) {
		return entry.limits, nil
	}
	if cache.pending >= 2 {
		return webLimits{Loading: true}, nil
	}
	if cache.entries == nil {
		cache.entries = make(map[string]webUsageEntry)
	}
	for key, entry := range cache.entries {
		if !entry.limits.Loading && time.Since(entry.checkedAt) >= time.Minute {
			delete(cache.entries, key)
		}
	}
	loading := webLimits{Loading: true}
	cache.entries[revision] = webUsageEntry{limits: loading}
	cache.pending++
	go func() {
		ctx, cancel := context.WithTimeout(w.ctx, 10*time.Second)
		defer cancel()
		read, err := ReadDesktopUsage(ctx, c, account)
		var limits webLimits
		if err == nil && read.Revision == revision {
			usage := usageFromSnapshot(read.Snapshot, "")
			limits.FiveHour, limits.Week = usage.FiveHour, usage.Week
		}
		cache.mu.Lock()
		cache.entries[revision] = webUsageEntry{limits: limits, checkedAt: time.Now()}
		cache.pending--
		cache.mu.Unlock()
	}()
	return loading, nil
}
