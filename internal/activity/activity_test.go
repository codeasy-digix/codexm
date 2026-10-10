package activity

import (
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/jobs"
)

func TestQuotaUnknownZeroAndResetsAreNotMisattributed(t *testing.T) {
	reset, later := int64(10), int64(20)
	zero := &jobs.UsageWindow{UsedPercent: 0, ResetsAt: &reset}
	seven := &jobs.UsageWindow{UsedPercent: 7, ResetsAt: &reset}
	if got := Window(zero, seven); got != "0%→7% (+7%p)" {
		t.Fatal(got)
	}
	if got := Window(nil, zero); got != "?→0%" {
		t.Fatal(got)
	}
	if got := Window(seven, &jobs.UsageWindow{UsedPercent: 2, ResetsAt: &later}); !strings.Contains(got, "갱신") || strings.Contains(got, "+-") {
		t.Fatal(got)
	}
	if got := Window(seven, &jobs.UsageWindow{UsedPercent: 8}); strings.Contains(got, "%p") {
		t.Fatal("unknown reset counted as consumption", got)
	}
	u := Usage(accounts.Snapshot{Authenticated: true, CheckedAt: time.Now(), Single: accounts.Bucket{Primary: &accounts.Window{UsedPercent: 80}}, Error: "failed"}, "")
	if u.Primary != nil || u.Error == "" {
		t.Fatal("failed sample retained a stale percent", u)
	}
}
