package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
)

type EditableSettings struct {
	ManagerAccount           string   `json:"manager_account"`
	DefaultWorker            string   `json:"default_worker"`
	Priority                 []string `json:"priority"`
	FallbackAccount          string   `json:"fallback_account"`
	PreferredRecheckSeconds  int      `json:"preferred_recheck_seconds"`
	ReconcileIntervalSeconds int      `json:"reconcile_interval_seconds"`
	MaxParallelJobs          int      `json:"max_parallel_jobs"`
	UsageSampleSeconds       int      `json:"usage_sample_seconds"`
	FiveHourDiscord          *bool    `json:"five_hour_discord,omitempty"`
}
type SettingsDocument struct {
	Revision    string           `json:"revision"`
	Values      EditableSettings `json:"values"`
	Accounts    []string         `json:"accounts"`
	Application string           `json:"application"`
}

func editable(c config.Config) EditableSettings {
	return EditableSettings{c.Manager.Account, c.DefaultWorker(), slices.Clone(c.WorkerAccounts.Priority), c.WorkerAccounts.FallbackAccount, c.WorkerAccounts.PreferredRecheckSeconds, c.Supervisor.ReconcileIntervalSeconds, c.Execution.MaxParallelJobs, c.Logging.UsageSampleSeconds, &c.Notifications.FiveHourDiscord}
}
func (s *Server) settingsDocument() (SettingsDocument, error) {
	c, rev, e := config.Current(s.Config.Path)
	if e != nil {
		return SettingsDocument{}, e
	}
	names, e := accounts.List(c)
	return SettingsDocument{rev, editable(c), names, "새 작업과 다음 계정 선택에 적용됩니다. 기존 세션 계정은 바뀌지 않습니다. 감시 간격은 다음 대조부터 반영됩니다."}, e
}
func (s *Server) saveSettings(ctx context.Context, b json.RawMessage) (SettingsDocument, error) {
	var doc struct {
		Revision string           `json:"revision"`
		Values   EditableSettings `json:"values"`
		Changed  []string         `json:"changed,omitempty"`
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(&doc); e != nil {
		return SettingsDocument{}, e
	}
	if doc.Revision == "" {
		return SettingsDocument{}, errors.New("a settings revision is required")
	}
	v := doc.Values
	validFields := []string{"manager_account", "default_worker", "priority", "fallback_account", "preferred_recheck_seconds", "reconcile_interval_seconds", "max_parallel_jobs", "usage_sample_seconds", "five_hour_discord"}
	for _, field := range doc.Changed {
		if !slices.Contains(validFields, field) {
			return SettingsDocument{}, errors.New("unsupported changed setting")
		}
	}
	changed := func(field string) bool { return len(doc.Changed) == 0 || slices.Contains(doc.Changed, field) }
	if v.ManagerAccount == "" || v.PreferredRecheckSeconds < 60 || v.PreferredRecheckSeconds > 86400 || v.ReconcileIntervalSeconds < 1 || v.ReconcileIntervalSeconds > 300 || v.MaxParallelJobs < 1 || v.MaxParallelJobs > 32 || v.UsageSampleSeconds < 10 || v.UsageSampleSeconds > 3600 {
		return SettingsDocument{}, errors.New("invalid settings: recheck 60–86400s, reconcile 1–300s, parallel 1–32, usage sampling 10–3600s; manager required")
	}
	names, e := accounts.List(s.Config)
	if e != nil {
		return SettingsDocument{}, e
	}
	if !slices.Contains(names, v.ManagerAccount) || !slices.Contains(v.Priority, v.DefaultWorker) {
		return SettingsDocument{}, errors.New("choose a saved manager and a default worker from the priority list")
	}
	for _, name := range v.Priority {
		if !slices.Contains(names, name) {
			return SettingsDocument{}, errors.New("every priority account must have a saved login")
		}
	}
	if v.ManagerAccount != s.manager() {
		a := s.Reader.Read(ctx, v.ManagerAccount)
		if !a.Authenticated || a.Error != "" {
			return SettingsDocument{}, errors.New("manager login could not be verified")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _, e = config.Update(s.Config.Path, doc.Revision, func(c *config.Config) error {
		if changed("manager_account") {
			c.Manager.Account = v.ManagerAccount
		}
		if changed("default_worker") {
			c.WorkerAccounts.DefaultAccount = v.DefaultWorker
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
		return SettingsDocument{}, e
	}
	s.managerAccount = v.ManagerAccount
	return s.settingsDocument()
}
