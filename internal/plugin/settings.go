package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

func settingProperties(names []string) map[string]any {
	properties := map[string]any{}
	for key, title := range map[string]string{"manager_account": "관리자 계정", "default_worker": "새 작업 기본 계정", "fallback_account": "예비 계정"} {
		p := map[string]any{"type": "string", "title": title}
		if names != nil {
			p["enum"] = append([]string{""}, names...)
		}
		properties[key] = p
	}
	properties["priority"] = map[string]any{"type": "string", "title": "작업 계정 우선순위", "description": "쉼표로 구분합니다. 예비 계정은 마지막에 둡니다.", "maxLength": 2000}
	for key, spec := range map[string]struct {
		title    string
		min, max int
	}{
		"preferred_recheck_seconds":  {"예비 계정에서 복귀 확인 (초)", 60, 86400},
		"reconcile_interval_seconds": {"작업 상태 확인 (초)", 1, 300},
		"max_parallel_jobs":          {"동시 실행 작업 수", 1, 32},
		"usage_sample_seconds":       {"사용량 관측 간격 (초)", 10, 3600},
	} {
		properties[key] = map[string]any{"type": "integer", "title": spec.title, "minimum": spec.min, "maximum": spec.max}
	}
	properties["five_hour_discord"] = map[string]any{"type": "boolean", "title": "5시간 한도 소진 시 Discord 알림", "description": "전체 계정과 현재 작업 상태를 함께 보냅니다."}
	return properties
}

func pluginValues(c config.Config) map[string]any {
	return map[string]any{"manager_account": c.Manager.Account, "default_worker": c.DefaultWorker(), "fallback_account": c.WorkerAccounts.FallbackAccount, "priority": strings.Join(c.WorkerAccounts.Priority, ", "), "preferred_recheck_seconds": c.WorkerAccounts.PreferredRecheckSeconds, "reconcile_interval_seconds": c.Supervisor.ReconcileIntervalSeconds, "max_parallel_jobs": c.Execution.MaxParallelJobs, "usage_sample_seconds": c.Logging.UsageSampleSeconds, "five_hour_discord": c.Notifications.FiveHourDiscord}
}

func (m MCP) readPluginSettings() (any, error) {
	c, e := config.Load(m.Config.Path)
	if e != nil {
		return nil, e
	}
	names, e := accounts.List(c)
	if e != nil {
		return nil, e
	}
	group := func(title string, keys ...string) map[string]any {
		items := []any{}
		for _, key := range keys {
			items = append(items, map[string]any{"kind": "property", "property": key})
		}
		return map[string]any{"kind": "group", "title": title, "items": items}
	}
	return map[string]any{
		"schema": map[string]any{"type": "object", "properties": settingProperties(names)},
		"values": pluginValues(c),
		"layout": []any{
			map[string]any{"kind": "group", "title": "codexm 관리 화면", "items": []any{map[string]any{"kind": "tool", "tool": "codexm_open", "description": "작업·계정·사용 기록·설정을 패널에서 관리합니다."}}},
			group("계정 관리", "manager_account", "default_worker", "priority", "fallback_account"),
			group("감시와 실행", "preferred_recheck_seconds", "reconcile_interval_seconds", "max_parallel_jobs", "usage_sample_seconds"),
			group("알림", "five_hour_discord"),
		},
	}, nil
}

func (m MCP) updatePluginSettings(ctx context.Context, b json.RawMessage) (any, error) {
	var a struct {
		Set map[string]json.RawMessage `json:"set"`
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(b, &top) != nil || len(top) != 1 || top["set"] == nil || json.Unmarshal(b, &a) != nil || len(a.Set) == 0 {
		return nil, errors.New("provide changed settings in set")
	}
	c, rev, e := config.Current(m.Config.Path)
	if e != nil {
		return nil, e
	}
	values := pluginValues(c)
	properties := settingProperties(nil)
	for key, raw := range a.Set {
		if _, ok := properties[key]; !ok {
			return nil, errors.New("unsupported plugin setting")
		}
		var value any
		if json.Unmarshal(raw, &value) != nil || value == nil {
			return nil, errors.New("invalid plugin setting")
		}
		values[key] = value
	}
	priority, ok := values["priority"].(string)
	if !ok {
		return nil, errors.New("priority must be a comma-separated account list")
	}
	var ordered []string
	for _, name := range strings.Split(priority, ",") {
		if name = strings.TrimSpace(name); name != "" {
			ordered = append(ordered, name)
		}
	}
	values["priority"] = ordered
	encoded, e := json.Marshal(values)
	if e != nil {
		return nil, e
	}
	var v supervisor.EditableSettings
	if e = json.Unmarshal(encoded, &v); e != nil {
		return nil, errors.New("setting type does not match its schema")
	}
	if v.ManagerAccount == "" || v.PreferredRecheckSeconds < 60 || v.PreferredRecheckSeconds > 86400 || v.ReconcileIntervalSeconds < 1 || v.ReconcileIntervalSeconds > 300 || v.MaxParallelJobs < 1 || v.MaxParallelJobs > 32 || v.UsageSampleSeconds < 10 || v.UsageSampleSeconds > 3600 {
		return nil, errors.New("plugin settings are outside their allowed range")
	}
	if supervisor.Ping(ctx, c) == nil {
		changed := make([]string, 0, len(a.Set))
		for key := range a.Set {
			changed = append(changed, key)
		}
		if e = supervisor.Request(ctx, c, "settings-save", map[string]any{"revision": rev, "values": v, "changed": changed}, nil); e != nil {
			return nil, e
		}
	} else {
		names, e := accounts.List(c)
		if e != nil {
			return nil, e
		}
		if !slices.Contains(names, v.ManagerAccount) || !slices.Contains(v.Priority, v.DefaultWorker) {
			return nil, errors.New("choose saved accounts")
		}
		for _, name := range v.Priority {
			if !slices.Contains(names, name) {
				return nil, errors.New("priority account is not saved")
			}
		}
		if v.ManagerAccount != c.Manager.Account {
			r, e := cr.Resolve(ctx, c)
			if e != nil {
				return nil, e
			}
			snapshot := accounts.New(r).Read(ctx, v.ManagerAccount)
			if !snapshot.Authenticated || snapshot.Error != "" {
				return nil, errors.New("manager login could not be verified")
			}
		}
		_, _, e = config.Update(c.Path, rev, func(next *config.Config) error {
			if _, ok := a.Set["manager_account"]; ok {
				next.Manager.Account = v.ManagerAccount
			}
			if _, ok := a.Set["default_worker"]; ok {
				next.WorkerAccounts.DefaultAccount = v.DefaultWorker
			}
			if _, ok := a.Set["priority"]; ok {
				next.WorkerAccounts.Priority = slices.Clone(v.Priority)
			}
			if _, ok := a.Set["fallback_account"]; ok {
				next.WorkerAccounts.FallbackAccount = v.FallbackAccount
			}
			if _, ok := a.Set["preferred_recheck_seconds"]; ok {
				next.WorkerAccounts.PreferredRecheckSeconds = v.PreferredRecheckSeconds
			}
			next.WorkerAccounts.FallbackRequiresAllPrimaryExhausted = true
			next.WorkerAccounts.UnknownIsExhausted = false
			if _, ok := a.Set["reconcile_interval_seconds"]; ok {
				next.Supervisor.ReconcileIntervalSeconds = v.ReconcileIntervalSeconds
			}
			if _, ok := a.Set["max_parallel_jobs"]; ok {
				next.Execution.MaxParallelJobs = v.MaxParallelJobs
			}
			if _, ok := a.Set["usage_sample_seconds"]; ok {
				next.Logging.UsageSampleSeconds = v.UsageSampleSeconds
			}
			if _, ok := a.Set["five_hour_discord"]; ok {
				next.Notifications.FiveHourDiscord = *v.FiveHourDiscord
			}
			return next.Validate()
		})
		if e != nil {
			return nil, e
		}
	}
	next, e := config.Load(c.Path)
	if e != nil {
		return nil, e
	}
	return map[string]any{"values": pluginValues(next)}, nil
}
