package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

type usageFixture struct {
	methods           []string
	loggedOut, denied bool
}

func (f *usageFixture) Call(_ context.Context, method string, params, result any) error {
	f.methods = append(f.methods, method)
	var value any
	switch method {
	case "account/read":
		// Matches the observed official CLI behavior: explicit refresh may return
		// null, while an ordinary read identifies the current authenticated user.
		if params.(map[string]bool)["refreshToken"] || f.loggedOut {
			value = map[string]any{"account": nil}
		} else {
			value = map[string]any{"account": map[string]string{"type": "chatgpt", "planType": "team"}}
		}
	case "account/rateLimits/read":
		if f.denied {
			return errors.New("not authorized")
		}
		value = map[string]any{"rateLimits": map[string]any{"primary": map[string]int{"usedPercent": 22, "windowDurationMins": 300}, "secondary": map[string]int{"usedPercent": 32, "windowDurationMins": 10080}}}
	default:
		return errors.New("unexpected operation")
	}
	b, _ := json.Marshal(value)
	return json.Unmarshal(b, result)
}
func TestUsageReadDoesNotForceRefreshOrHideQuotaErrors(t *testing.T) {
	for _, mode := range []string{"live", "logged-out", "denied"} {
		t.Run(mode, func(t *testing.T) {
			f := &usageFixture{loggedOut: mode == "logged-out", denied: mode == "denied"}
			s := readAccountUsage(context.Background(), f, Snapshot{Account: "named"})
			if mode == "logged-out" {
				if s.Authenticated || s.Error == "" || !reflect.DeepEqual(f.methods, []string{"account/read"}) {
					t.Fatal(s, f.methods)
				}
				return
			}
			if !reflect.DeepEqual(f.methods, []string{"account/read", "account/rateLimits/read"}) {
				t.Fatal(f.methods)
			}
			if mode == "denied" {
				if s.Error == "" || s.State("") != "unknown" {
					t.Fatal(s)
				}
				return
			}
			if !s.Authenticated || s.Error != "" || s.Plan != "team" || s.Single.Primary.UsedPercent != 22 || s.Single.Secondary.UsedPercent != 32 {
				t.Fatal(s)
			}
		})
	}
}
