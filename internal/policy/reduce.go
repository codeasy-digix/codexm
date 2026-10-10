package policy

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func Reduce(j jobs.Job, ev jobs.Event, m cr.Message) (jobs.Job, []jobs.Action) {
	if ev.RunID != "" && j.RunID != ev.RunID {
		return j, nil
	}
	var p struct {
		ThreadID string  `json:"threadId"`
		TurnID   string  `json:"turnId"`
		Turn     cr.Turn `json:"turn"`
		Status   struct {
			Type        string   `json:"type"`
			ActiveFlags []string `json:"activeFlags"`
		} `json:"status"`
		Goal      *jobs.Goal   `json:"goal"`
		Error     cr.TurnError `json:"error"`
		WillRetry bool         `json:"willRetry"`
		Item      cr.Item      `json:"item"`
		Delta     string       `json:"delta"`
	}
	_ = json.Unmarshal(m.Params, &p)
	if p.ThreadID != "" && p.ThreadID != j.ThreadID {
		return j, nil
	}
	j.LastObserved = ev.At
	j.UpdatedAt = ev.At
	actions := []jobs.Action{}
	enqueue := func(kind string, v any) {
		b, _ := json.Marshal(v)
		actions = append(actions, jobs.Action{ID: ev.Key + ":" + kind, JobID: j.ID, RunID: j.RunID, Kind: kind, Payload: b, State: "pending", CreatedAt: ev.At, NotBefore: ev.At})
	}
	if len(m.ID) > 0 {
		for _, r := range j.PendingRequests {
			if string(r.ID) == string(m.ID) && r.RunID == j.RunID {
				return j, nil
			}
		}
		j.PendingRequests = append(j.PendingRequests, jobs.Request{ID: m.ID, Method: m.Method, Params: m.Params, RunID: j.RunID})
		if m.Method == "item/tool/requestUserInput" || m.Method == "tool/requestUserInput" || m.Method == "mcpServer/elicitation/request" {
			j.State = "waiting_on_user"
		} else {
			j.State = "waiting_on_approval"
		}
		return j, nil
	}
	switch m.Method {
	case "turn/started":
		j.TurnID = p.Turn.ID
		j.State = "running"
		j.LastError = ""
		j.LastOutput = ""
	case "thread/tokenUsage/updated":
		var u struct {
			TokenUsage json.RawMessage `json:"tokenUsage"`
		}
		if json.Unmarshal(m.Params, &u) == nil {
			j.TokenUsage = u.TokenUsage
		}
	case "thread/status/changed":
		if j.Paused || jobs.Terminal(j.State) {
			break
		}
		switch p.Status.Type {
		case "active":
			j.State = "running"
			for _, f := range p.Status.ActiveFlags {
				if f == "waitingOnApproval" {
					j.State = "waiting_on_approval"
				}
				if f == "waitingOnUserInput" {
					j.State = "waiting_on_user"
				}
			}
		case "systemError":
			j.State = "error"
		case "idle":
			if j.State == "running" && j.TurnID == "" {
				j.State = "idle"
			}
		}
	case "item/completed":
		if p.Item.Type == "agentMessage" {
			j.LastOutput = p.Item.Text
		}
	case "item/agentMessage/delta":
		j.LastOutput += p.Delta
		if len(j.LastOutput) > 32768 {
			j.LastOutput = j.LastOutput[len(j.LastOutput)-32768:]
		}
	case "serverRequest/resolved":
		var v struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		_ = json.Unmarshal(m.Params, &v)
		out := []jobs.Request{}
		for _, r := range j.PendingRequests {
			if string(r.ID) != string(v.RequestID) {
				out = append(out, r)
			}
		}
		j.PendingRequests = out
	case "thread/goal/updated":
		j.Goal = p.Goal
		if p.Goal != nil {
			if p.Goal.Status == "active" {
				j.Kind = "goal"
			}
			switch p.Goal.Status {
			case "complete":
				if j.Kind != "goal" {
					break
				}
				j.State = "complete"
				enqueue("cleanup", nil)
			case "paused":
				if !j.Paused && j.State != "switching" {
					j.State = "paused"
					j.Paused = true
				}
			case "usageLimited":
				j.State = "quota_wait"
				enqueue("switch_account", nil)
			case "budgetLimited":
				j.State = "budget_limited"
			case "blocked":
				j.State = "blocked"
				enqueue("manager_incident", map[string]any{"reason": "goal blocked"})
			}
		}
	case "thread/goal/cleared":
		j.Goal = nil
		if j.Kind == "goal" {
			j.Kind = "task"
		}
	case "error":
		if p.TurnID != "" && j.TurnID != "" && p.TurnID != j.TurnID {
			break
		}
		j.LastError = cr.SafeText(p.Error.Message)
		if p.WillRetry {
			break
		}
		kind := ErrorKind(p.Error.CodexErrorInfo)
		switch kind {
		case "usageLimitExceeded":
			j.State = "quota_wait"
			enqueue("switch_account", nil)
		case "rateLimitExceeded":
			j.State = "retry_wait"
			enqueue("retry", map[string]any{"reason": kind})
		case "sessionBudgetExceeded":
			j.State = "budget_limited"
		case "unauthorized":
			j.State = "auth_required"
		default:
			j.State = "error"
			enqueue("manager_incident", map[string]any{"reason": kind})
		}
	case "turn/completed":
		if j.TurnID != "" && p.Turn.ID != j.TurnID {
			break
		}
		j.TurnID = ""
		j.LastTurnID = p.Turn.ID
		j.PendingRequests = nil
		if j.Paused || jobs.Terminal(j.State) || j.State == "quota_wait" || j.State == "retry_wait" || j.State == "auth_required" || j.State == "budget_limited" {
			break
		}
		if p.Turn.Error != nil {
			j.LastError = cr.SafeText(p.Turn.Error.Message)
			switch ErrorKind(p.Turn.Error.CodexErrorInfo) {
			case "usageLimitExceeded":
				j.State = "quota_wait"
				enqueue("switch_account", nil)
			case "rateLimitExceeded":
				j.State = "retry_wait"
				enqueue("retry", nil)
			case "sessionBudgetExceeded":
				j.State = "budget_limited"
			case "unauthorized":
				j.State = "auth_required"
			default:
				j.State = "error"
				enqueue("manager_incident", map[string]string{"reason": j.LastError})
			}
			break
		}
		if p.Turn.Status == "interrupted" {
			if j.State == "switching" {
				break
			}
			j.State = "paused"
			j.Paused = true
			break
		}
		j.RetryCount = 0
		if j.Kind == "chat" {
			j.State = "idle"
		} else if j.Kind == "goal" {
			j.State = "goal_wait"
			if j.Goal != nil && j.Goal.Status == "active" {
				enqueue("goal_continue", nil)
			}
		} else {
			j.State = "result_ready"
			if len(j.CompletionCommand) > 0 {
				enqueue("check_completion", nil)
			}
		}
	case "connection/lost":
		if !j.Paused && !jobs.Terminal(j.State) {
			j.State = "unknown"
		}
	}
	return j, actions
}
func ErrorKind(b json.RawMessage) string {
	var s string
	if json.Unmarshal(b, &s) == nil {
		return s
	}
	var m map[string]any
	if json.Unmarshal(b, &m) == nil {
		for k := range m {
			return k
		}
	}
	return "unknown"
}
func Action(kind string, j jobs.Job, v any) jobs.Action {
	b, _ := json.Marshal(v)
	now := time.Now()
	return jobs.Action{ID: jobs.ID(), JobID: j.ID, RunID: j.RunID, Kind: kind, Payload: b, State: "pending", NotBefore: now, CreatedAt: now}
}
func ValidateParent(j jobs.Job, all []jobs.Job, maxDepth int) error {
	if j.ParentID == "" {
		return nil
	}
	byID := map[string]jobs.Job{}
	for _, x := range all {
		byID[x.ID] = x
	}
	depth := 0
	for id := j.ParentID; id != ""; {
		p, ok := byID[id]
		if !ok {
			return fmt.Errorf("parent job not found")
		}
		depth++
		if depth > maxDepth {
			return fmt.Errorf("child depth exceeds %d", maxDepth)
		}
		if p.ID == j.ID {
			return fmt.Errorf("cyclic job parent")
		}
		id = p.ParentID
	}
	return nil
}
