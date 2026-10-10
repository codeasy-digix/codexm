package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

type Action struct {
	Type     string `json:"type"`
	JobID    string `json:"job_id"`
	ParentID string `json:"parent_id"`
	Title    string `json:"title"`
	Cwd      string `json:"cwd"`
	Kind     string `json:"kind"`
	Prompt   string `json:"prompt"`
	Goal     string `json:"goal"`
}
type Decision struct {
	Summary string   `json:"summary"`
	Actions []Action `json:"actions"`
}
type Record struct {
	ID         string          `json:"id"`
	Incident   string          `json:"incident"`
	Account    string          `json:"account"`
	ThreadID   string          `json:"thread_id,omitempty"`
	State      string          `json:"state"`
	Error      string          `json:"error,omitempty"`
	Decision   *Decision       `json:"decision,omitempty"`
	Tokens     json.RawMessage `json:"tokens,omitempty"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt time.Time       `json:"finished_at,omitempty"`
}
type Unavailable struct{ Reason string }

func (e *Unavailable) Error() string { return e.Reason }

func Schema() any {
	props := map[string]any{}
	for _, key := range []string{"job_id", "parent_id", "title", "cwd", "kind", "prompt", "goal"} {
		props[key] = map[string]any{"type": "string"}
	}
	props["type"] = map[string]any{"type": "string", "enum": []string{"create_job", "send_input", "retry", "wait", "needs_user"}}
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"summary", "actions"}, "properties": map[string]any{"summary": map[string]any{"type": "string"}, "actions": map[string]any{"type": "array", "maxItems": 5, "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"type", "job_id", "parent_id", "title", "cwd", "kind", "prompt", "goal"}, "properties": props}}}}
}
func Parse(text string) (Decision, error) {
	var d Decision
	if len(text) > 32768 {
		return d, errors.New("manager response exceeds 32 KiB")
	}
	dec := json.NewDecoder(bytes.NewBufferString(text))
	dec.DisallowUnknownFields()
	if e := dec.Decode(&d); e != nil {
		return d, e
	}
	if len(d.Actions) > 5 {
		return d, errors.New("too many manager actions")
	}
	var extra any
	if e := dec.Decode(&extra); e != io.EOF {
		return d, errors.New("unexpected content after manager response")
	}
	return d, nil
}
func Within(path, root string) bool {
	p, e := filepath.EvalSymlinks(path)
	if e != nil {
		return false
	}
	r, e := filepath.EvalSymlinks(root)
	if e != nil {
		return false
	}
	rel, e := filepath.Rel(r, p)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func Validate(d Decision, all []jobs.Job, scope string, roots []string, allowCreate bool) error {
	allowed := map[string]bool{}
	byID := map[string]jobs.Job{}
	for _, j := range all {
		byID[j.ID] = j
		if scope == "" {
			allowed[j.ID] = true
		}
	}
	if scope != "" {
		if j, ok := byID[scope]; ok && j.Paused && len(d.Actions) > 0 {
			return errors.New("selected job was paused; manager actions are held")
		}
		allowed[scope] = true
		for range len(all) {
			for _, j := range all {
				if allowed[j.ParentID] {
					allowed[j.ID] = true
				}
			}
		}
	}
	for _, a := range d.Actions {
		switch a.Type {
		case "create_job":
			if !allowCreate {
				return errors.New("new jobs are outside this decision's delegation scope")
			}
			if a.Kind != "chat" && a.Kind != "task" && a.Kind != "goal" && a.Kind != "group" {
				return errors.New("invalid manager job kind")
			}
			if a.ParentID != "" && !allowed[a.ParentID] {
				return errors.New("manager parent is outside its scope")
			}
			if scope != "" && a.ParentID == "" {
				return errors.New("child job needs its scoped parent")
			}
			if a.Kind == "goal" && a.Goal == "" {
				return errors.New("manager goal needs an objective")
			}
			if a.Kind != "group" && strings.TrimSpace(a.Prompt) == "" {
				return errors.New("manager job needs a concrete instruction")
			}
			ok := false
			for _, r := range roots {
				if Within(a.Cwd, r) {
					ok = true
				}
			}
			if !ok {
				return fmt.Errorf("workspace %q is outside authorized roots", a.Cwd)
			}
		case "send_input", "retry":
			j, ok := byID[a.JobID]
			if !ok || !allowed[a.JobID] || !j.Managed {
				return errors.New("manager target is outside its scope")
			}
			if j.Paused || jobs.Terminal(j.State) || jobs.Busy(j.State) {
				return errors.New("manager cannot restart a paused, finished or running job")
			}
			if a.Type == "send_input" && strings.TrimSpace(a.Prompt) == "" {
				return errors.New("empty manager input")
			}
			if a.Type == "retry" && (j.State == "budget_limited" || j.State == "auth_required" || j.State == "external_writer") {
				return errors.New("this condition requires a user action")
			}
		case "wait", "needs_user":
			if a.JobID != "" && !allowed[a.JobID] {
				return errors.New("manager wait target outside scope")
			}
		default:
			return fmt.Errorf("unsupported manager action %q", a.Type)
		}
	}
	return nil
}

// One ephemeral model turn; normal agent tools and long-lived history are disabled.
func Run(ctx context.Context, r cr.Resolver, account, model, prompt string, rec *Record) (Decision, error) {
	cmd := r.Command(ctx, account, "app-server", "--listen", "stdio://", "--disable", "shell_tool", "--disable", "code_mode", "--disable", "code_mode_host", "--disable", "multi_agent", "--disable", "apps", "--disable", "memories", "-c", `web_search="disabled"`, "-c", `mcp_servers={}`)
	c, e := cr.StartStdio(ctx, cmd)
	if e != nil {
		return Decision{}, e
	}
	defer c.Close()
	var t cr.ResumeResponse
	p := map[string]any{"ephemeral": true, "sandbox": "read-only", "approvalPolicy": "never", "environments": []any{}, "baseInstructions": "You are a short-lived job coordinator. Use no tools. Read only the supplied snapshot. Return only the requested JSON decision. Never approve permissions, change accounts or budgets, run shell commands, or repeat dispatched inputs whose outcome is unknown. Scope and user pauses are binding. Empty action fields are empty strings. A normal chat waits after one answer. Only propose create_job for explicitly authorized delegation within the supplied workspace roots."}
	if model != "" {
		p["model"] = model
	}
	if e = c.Call(ctx, "thread/start", p, &t); e != nil {
		return Decision{}, e
	}
	rec.ThreadID = t.Thread.ID
	var turn struct {
		Turn cr.Turn `json:"turn"`
	}
	if e = c.Call(ctx, "turn/start", map[string]any{"threadId": t.Thread.ID, "input": cr.TextInput(prompt), "outputSchema": Schema(), "effort": "low"}, &turn); e != nil {
		return Decision{}, e
	}
	var text string
	for {
		select {
		case m := <-c.Events:
			if len(m.ID) > 0 {
				_ = c.Reject(m.ID, "manager decisions cannot request tools or permissions")
				continue
			}
			var p struct {
				ThreadID  string          `json:"threadId"`
				Item      cr.Item         `json:"item"`
				Turn      cr.Turn         `json:"turn"`
				Error     cr.TurnError    `json:"error"`
				WillRetry bool            `json:"willRetry"`
				Tokens    json.RawMessage `json:"tokenUsage"`
			}
			_ = json.Unmarshal(m.Params, &p)
			if p.ThreadID != "" && p.ThreadID != t.Thread.ID {
				continue
			}
			switch m.Method {
			case "item/completed":
				if p.Item.Type == "agentMessage" {
					text = p.Item.Text
				}
			case "thread/tokenUsage/updated":
				rec.Tokens = p.Tokens
			case "error":
				if !p.WillRetry {
					return Decision{}, errors.New(cr.SafeText(p.Error.Message))
				}
			case "turn/completed":
				if p.Turn.ID != turn.Turn.ID {
					continue
				}
				if p.Turn.Error != nil {
					return Decision{}, errors.New(cr.SafeText(p.Turn.Error.Message))
				}
				return Parse(text)
			}
		case <-ctx.Done():
			if turn.Turn.ID != "" {
				stop, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_ = c.Interrupt(stop, t.Thread.ID, turn.Turn.ID)
				cancel()
			}
			return Decision{}, ctx.Err()
		case <-c.Done:
			return Decision{}, c.Err()
		}
	}
}
