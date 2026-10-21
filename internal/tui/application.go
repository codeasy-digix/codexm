package tui

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/codeasy-digix/codexm/internal/jobs"
)

// The TUI and GUI share management policy, authentication isolation and durable
// operation IDs. UI callbacks never interpret shell strings or call a model.
func (m Model) appCall(method string, args any) tea.Cmd {
	op := jobs.ID()
	return func() tea.Msg {
		if m.App == nil {
			return replyMsg{Command: "app:" + method, Epoch: m.ViewEpoch, Error: errors.New("관리 연결을 다시 여세요.")}
		}
		parent := m.Context
		if parent == nil {
			parent = context.Background()
		}
		ctx, cancel := context.WithTimeout(parent, 180*time.Second)
		defer cancel()
		raw, err := json.Marshal(args)
		if err != nil {
			return replyMsg{Command: "app:" + method, Error: err, Epoch: m.ViewEpoch}
		}
		if string(raw) == "null" {
			raw = []byte(`{}`)
		}
		result, err := m.App.Call(ctx, m.ClientID, method, op, raw)
		var data json.RawMessage
		if err == nil {
			data, err = json.Marshal(result)
		}
		return replyMsg{Command: "app:" + method, Data: data, Error: err, Epoch: m.ViewEpoch, OperationID: op}
	}
}

var applicationMethods = map[string]string{
	"new": "jobs.create", "resume": "jobs.import", "manage": "jobs.manage", "register": "jobs.register",
	"unregister": "jobs.unregister", "send": "jobs.send", "pause": "jobs.pause",
	"stop": "jobs.stop", "resume-job": "jobs.resume", "complete": "jobs.complete",
	"reply": "jobs.reply", "switch-account": "jobs.account", "goal": "jobs.goal",
	"manager": "manager.invoke", "resolve-action": "actions.resolve",
}
