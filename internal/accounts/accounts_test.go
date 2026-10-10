package accounts

import (
	"github.com/codeasy-digix/codexm/internal/config"
	"testing"
)

func boolp(b bool) *bool { return &b }
func available(n string) Snapshot {
	return Snapshot{Account: n, Authenticated: true, Ordinary: boolp(true)}
}
func exhausted(n string) Snapshot {
	return Snapshot{Account: n, Authenticated: true, Ordinary: boolp(false)}
}
func TestPriorityFallbackAndUnknown(t *testing.T) {
	c := config.Defaults()
	s := map[string]Snapshot{"account-work": exhausted("account-work"), "account-secondary": available("account-secondary"), "account-backup": available("account-backup")}
	n, _ := Choose(c, s, "", nil)
	if n != "account-secondary" {
		t.Fatal(n)
	}
	s["account-secondary"] = exhausted("account-secondary")
	n, _ = Choose(c, s, "", nil)
	if n != "" {
		t.Fatal("unknown primary permitted fallback", n)
	}
	s["account-main"] = exhausted("account-main")
	n, _ = Choose(c, s, "", nil)
	if n != "account-backup" {
		t.Fatal(n)
	}
	s["account-work"] = available("account-work")
	n, _ = Choose(c, s, "", nil)
	if n != "account-work" {
		t.Fatal("preferred recovery ignored")
	}
}
func TestRecoveryRequiresAuthoritativeAvailability(t *testing.T) {
	c := config.Defaults()
	s := map[string]Snapshot{"account-work": {Account: "account-work", Authenticated: true, Single: Bucket{Primary: &Window{UsedPercent: 0}}}}
	n, _ := Choose(c, s, "", map[string]bool{"account-work": true})
	if n != "" {
		t.Fatal("percentage inferred recovery")
	}
	s["account-work"] = available("account-work")
	n, _ = Choose(c, s, "", map[string]bool{"account-work": true})
	if n != "account-work" {
		t.Fatal("authoritative recovery not accepted")
	}
}
