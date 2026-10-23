package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
)

type Values struct {
	ManagerAccount           string   `json:"manager_account"`
	Priority                 []string `json:"priority"`
	FallbackAccount          string   `json:"fallback_account"`
	PreferredRecheckSeconds  int      `json:"preferred_recheck_seconds"`
	ReconcileIntervalSeconds int      `json:"reconcile_interval_seconds"`
	MaxParallelJobs          int      `json:"max_parallel_jobs"`
	UsageSampleSeconds       int      `json:"usage_sample_seconds"`
	FiveHourDiscord          *bool    `json:"five_hour_discord,omitempty"`
}
type Document struct {
	Revision    string   `json:"revision"`
	Values      Values   `json:"values"`
	Accounts    []string `json:"accounts"`
	Application string   `json:"application"`
}

func editable(c config.Config) Values {
	return Values{c.Manager.Account, slices.Clone(c.WorkerAccounts.Priority), c.WorkerAccounts.FallbackAccount, c.WorkerAccounts.PreferredRecheckSeconds, c.Supervisor.ReconcileIntervalSeconds, c.Execution.MaxParallelJobs, c.Logging.UsageSampleSeconds, &c.Notifications.FiveHourDiscord}
}
func Read(c config.Config) (Document, error) {
	c, rev, e := config.Current(c.Path)
	if e != nil {
		return Document{}, e
	}
	names, e := accounts.List(c)
	return Document{rev, editable(c), names, "작업 시작·재개·새 요청마다 가용 계정을 확인하고 현재 우선순위로 선택합니다. 진행 중인 turn은 유지합니다. 감시 간격은 다음 대조부터 반영됩니다."}, e
}

type AccountReader interface {
	Read(context.Context, string) accounts.Snapshot
}

func Save(ctx context.Context, c config.Config, reader AccountReader, b json.RawMessage) (Document, error) {
	var doc struct {
		Revision string `json:"revision"`
		Values   struct {
			Values
			LegacyDefaultWorker *string `json:"default_worker,omitempty"`
		} `json:"values"`
		Changed []string `json:"changed,omitempty"`
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(&doc); e != nil {
		return Document{}, e
	}
	if doc.Revision == "" {
		return Document{}, errors.New("a settings revision is required")
	}
	v := doc.Values.Values
	legacyOnly := len(doc.Changed) > 0
	filtered := []string{}
	for _, name := range doc.Changed {
		if name != "default_worker" {
			filtered = append(filtered, name)
			legacyOnly = false
		}
	}
	if legacyOnly {
		return Document{}, errors.New("the default worker setting was removed; reopen the panel and edit account priority")
	}
	doc.Changed = filtered
	validFields := []string{"manager_account", "priority", "fallback_account", "preferred_recheck_seconds", "reconcile_interval_seconds", "max_parallel_jobs", "usage_sample_seconds", "five_hour_discord"}
	for _, field := range doc.Changed {
		if !slices.Contains(validFields, field) {
			return Document{}, errors.New("unsupported changed setting")
		}
	}
	changed := func(field string) bool { return len(doc.Changed) == 0 || slices.Contains(doc.Changed, field) }
	if v.ManagerAccount == "" || v.PreferredRecheckSeconds < 60 || v.PreferredRecheckSeconds > 86400 || v.ReconcileIntervalSeconds < 1 || v.ReconcileIntervalSeconds > 300 || v.MaxParallelJobs < 1 || v.MaxParallelJobs > 32 || v.UsageSampleSeconds < 10 || v.UsageSampleSeconds > 3600 {
		return Document{}, errors.New("invalid settings: recheck 60–86400s, reconcile 1–300s, parallel 1–32, usage sampling 10–3600s; manager required")
	}
	names, e := accounts.List(c)
	if e != nil {
		return Document{}, e
	}
	if !slices.Contains(names, v.ManagerAccount) {
		return Document{}, errors.New("choose a saved manager account")
	}
	for _, name := range v.Priority {
		if !slices.Contains(names, name) {
			return Document{}, errors.New("every priority account must have a saved login")
		}
	}
	if v.ManagerAccount != c.Manager.Account {
		a := reader.Read(ctx, v.ManagerAccount)
		if !a.Authenticated || a.Error != "" {
			return Document{}, errors.New("manager login could not be verified")
		}
	}
	_, _, e = config.Update(c.Path, doc.Revision, func(c *config.Config) error {
		if changed("manager_account") {
			c.Manager.Account = v.ManagerAccount
		}
		if changed("priority") {
			c.WorkerAccounts.Priority = slices.Clone(v.Priority)
		}
		if changed("fallback_account") {
			c.WorkerAccounts.FallbackAccount = v.FallbackAccount
		}
		c.WorkerAccounts.FallbackRequiresAllPrimaryExhausted = true
		c.WorkerAccounts.UnknownIsExhausted = false
		if changed("preferred_recheck_seconds") {
			c.WorkerAccounts.PreferredRecheckSeconds = v.PreferredRecheckSeconds
		}
		if changed("reconcile_interval_seconds") {
			c.Supervisor.ReconcileIntervalSeconds = v.ReconcileIntervalSeconds
		}
		if changed("max_parallel_jobs") {
			c.Execution.MaxParallelJobs = v.MaxParallelJobs
		}
		if changed("usage_sample_seconds") {
			c.Logging.UsageSampleSeconds = v.UsageSampleSeconds
		}
		if changed("five_hour_discord") && v.FiveHourDiscord != nil {
			c.Notifications.FiveHourDiscord = *v.FiveHourDiscord
		}
		return c.Validate()
	})
	if e != nil {
		return Document{}, e
	}
	result, e := Read(c)
	if doc.Values.LegacyDefaultWorker != nil {
		result.Application += " 이전 화면의 기본 작업 계정 값은 제거된 설정이므로 적용하지 않았습니다. 패널을 다시 여세요."
	}
	return result, e
}
