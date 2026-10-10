package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"slices"
	"strings"
)

type Thread struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Preview   string          `json:"preview"`
	Cwd       string          `json:"cwd"`
	Path      string          `json:"path"`
	UpdatedAt int64           `json:"updatedAt"`
	Source    json.RawMessage `json:"source"`
	Status    struct {
		Type        string   `json:"type"`
		ActiveFlags []string `json:"activeFlags"`
	} `json:"status"`
	Turns []Turn `json:"turns"`
}
type Turn struct {
	ID     string     `json:"id"`
	Status string     `json:"status"`
	Error  *TurnError `json:"error"`
	Items  []Item     `json:"items"`
}
type TurnError struct {
	Message        string          `json:"message"`
	CodexErrorInfo json.RawMessage `json:"codexErrorInfo"`
}
type Item struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Text    string `json:"text"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}
type ResumeResponse struct {
	Thread          Thread          `json:"thread"`
	Model           string          `json:"model"`
	ReasoningEffort string          `json:"reasoningEffort"`
	ApprovalPolicy  json.RawMessage `json:"approvalPolicy"`
	Sandbox         json.RawMessage `json:"sandbox"`
}

func (c *Client) Read(ctx context.Context, id string, turns bool) (Thread, error) {
	var r struct {
		Thread Thread `json:"thread"`
	}
	e := c.Call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": turns}, &r)
	return r.Thread, e
}
func (c *Client) Resume(ctx context.Context, id string) (ResumeResponse, error) {
	var r ResumeResponse
	e := c.Call(ctx, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true}, &r)
	return r, e
}
func (c *Client) Recent(ctx context.Context, id string, limit int, cursor string) (Thread, string, error) {
	t, e := c.Read(ctx, id, false)
	if e != nil {
		return t, "", e
	}
	var page struct {
		Data       []Turn `json:"data"`
		NextCursor string `json:"nextCursor"`
	}
	p := map[string]any{"threadId": id, "limit": limit, "itemsView": "full", "sortDirection": "desc"}
	if cursor != "" {
		p["cursor"] = cursor
	}
	if e = c.Call(ctx, "thread/turns/list", p, &page); e != nil {
		var rpc *RPCError
		if errors.As(e, &rpc) && rpc.Code == -32600 && strings.Contains(rpc.Message, "not materialized yet") {
			t.Turns = []Turn{}
			return t, "", nil
		}
		return t, "", e
	}
	slices.Reverse(page.Data)
	t.Turns = page.Data
	return t, page.NextCursor, nil
}
func (c *Client) Goal(ctx context.Context, id string) (*jobs.Goal, error) {
	var r struct {
		Goal *jobs.Goal `json:"goal"`
	}
	e := c.Call(ctx, "thread/goal/get", map[string]any{"threadId": id}, &r)
	return r.Goal, e
}
func (c *Client) SetGoalStatus(ctx context.Context, id, status, origin string) error {
	return c.Call(ctx, "thread/goal/set", map[string]any{"threadId": id, "status": status, "origin": origin}, nil)
}
func (c *Client) List(ctx context.Context, search, cursor string) ([]Thread, string, error) {
	var r struct {
		Data       []Thread `json:"data"`
		NextCursor string   `json:"nextCursor"`
	}
	p := map[string]any{"limit": 50, "sortKey": "updated_at", "searchTerm": search}
	if cursor != "" {
		p["cursor"] = cursor
	}
	e := c.Call(ctx, "thread/list", p, &r)
	return r.Data, r.NextCursor, e
}
func TextInput(s string) []map[string]any {
	return []map[string]any{{"type": "text", "text": s, "text_elements": []any{}}}
}
func (c *Client) SendInput(ctx context.Context, id string, input jobs.Input, turnID string) (Turn, error) {
	var r struct {
		Turn   Turn   `json:"turn"`
		TurnID string `json:"turnId"`
	}
	method := "turn/start"
	p := map[string]any{"threadId": id, "input": TextInput(input.Text)}
	if input.MessageID != "" {
		p["clientUserMessageId"] = input.MessageID
	}
	if input.Steer {
		method = "turn/steer"
		p["expectedTurnId"] = turnID
	}
	e := c.Call(ctx, method, p, &r)
	if input.Steer {
		r.Turn.ID = r.TurnID
	}
	return r.Turn, e
}
func (c *Client) Interrupt(ctx context.Context, id, turn string) error {
	return c.Call(ctx, "turn/interrupt", map[string]any{"threadId": id, "turnId": turn}, nil)
}
func LastMessages(t Thread) (input, output string) {
	for _, turn := range t.Turns {
		for _, item := range turn.Items {
			switch item.Type {
			case "userMessage":
				for _, x := range item.Content {
					if x.Type == "text" {
						input = x.Text
					}
				}
			case "agentMessage":
				output = item.Text
			}
		}
	}
	return
}
