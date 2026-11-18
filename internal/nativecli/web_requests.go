package nativecli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

type webPendingRequest struct {
	ID        string          `json:"request_id"`
	Account   string          `json:"account"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
	Decisions []string        `json:"decisions,omitempty"`
}

func webPendingID(c *webConnection, id string) string {
	value := fmt.Sprintf("%d:%d:%s:%s", c.generation, c.process.PID, c.process.Started, id)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

func (w *WebChat) writerPending(c *webConnection, id string, owners []int) []webPendingRequest {
	if len(owners) != 1 || owners[0] == c.process.PID {
		return w.pending(c, id)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, connection := range w.connections {
		if !connection.retiring && connection.process.PID == owners[0] {
			return w.pending(connection, id)
		}
	}
	return []webPendingRequest{}
}

func webApprovalDecisions(event cr.Message) []string {
	allowed := []string{"accept", "decline", "cancel"}
	if event.Method == "item/commandExecution/requestApproval" {
		var approval struct{ AvailableDecisions []json.RawMessage }
		if json.Unmarshal(event.Params, &approval) == nil && approval.AvailableDecisions != nil {
			choices := []string{}
			for _, raw := range approval.AvailableDecisions {
				var decision string
				if json.Unmarshal(raw, &decision) == nil && (slices.Contains(allowed, decision) || decision == "acceptForSession") {
					choices = append(choices, decision)
				}
			}
			return choices
		}
	}
	return allowed
}

func (w *WebChat) pending(c *webConnection, id string) []webPendingRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pendingForThread(id)
}

func (c *webConnection) pendingForThread(id string) []webPendingRequest {
	result := []webPendingRequest{}
	for key, event := range c.pending {
		var params struct{ ThreadID string }
		if json.Unmarshal(event.Params, &params) != nil {
			continue
		}
		if id != "" && params.ThreadID != id {
			continue
		}
		request := webPendingRequest{ID: webPendingID(c, key), Account: c.account, Method: event.Method, Params: event.Params}
		if event.Method != "item/tool/requestUserInput" {
			request.Decisions = webApprovalDecisions(event)
		}
		result = append(result, request)
	}
	slices.SortFunc(result, func(a, b webPendingRequest) int { return strings.Compare(a.ID, b.ID) })
	return result
}

func (w *WebChat) respond(ctx context.Context, c *webConnection, p webParams) (any, error) {
	if p.Thread == "" || p.Request == "" {
		return nil, errors.New("thread and pending request IDs required")
	}
	if err := w.verifyOwner(ctx, c, p.Thread, true); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var key string
	for id := range c.pending {
		if webPendingID(c, id) == p.Request {
			key = id
			break
		}
	}
	event, found := c.pending[key]
	if !found {
		return nil, errors.New("request is no longer pending; no response was replayed")
	}
	var target struct {
		Thread string `json:"threadId"`
	}
	if json.Unmarshal(event.Params, &target) != nil || target.Thread != p.Thread {
		return nil, errors.New("pending request belongs to another thread")
	}
	if err := cr.ValidateReply(jobs.Request{Method: event.Method, Params: event.Params}, p.Response); err != nil {
		return nil, err
	}
	var response map[string]any
	if err := json.Unmarshal(p.Response, &response); err != nil {
		return nil, err
	}
	switch event.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		decision, _ := response["decision"].(string)
		if !slices.Contains(webApprovalDecisions(event), decision) {
			return nil, errors.New("현재 요청에서 제공하는 승인 선택지를 사용해 주세요.")
		}
		response = map[string]any{"decision": decision}
	case "item/tool/requestUserInput":
		if err := validateWebAnswers(event.Params, p.Response); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("this request type cannot be approved by the web frontend")
	}
	if err := c.client.Reply(event.ID, response); err != nil {
		delete(c.pending, key)
		return nil, errors.New("요청에 답변을 전달했는지 확인하지 못했습니다. 현재 작업 상태를 확인해 주세요.")
	}
	delete(c.pending, key)
	return map[string]any{"responded": true}, nil
}

func validateWebAnswers(params, response json.RawMessage) error {
	var request struct {
		Questions []struct {
			ID      string
			IsOther bool
			Options []struct{ Label string }
		}
	}
	var reply struct {
		Answers map[string]struct{ Answers []string }
	}
	if json.Unmarshal(params, &request) != nil || json.Unmarshal(response, &reply) != nil || len(request.Questions) == 0 {
		return errors.New("질문과 답변을 확인해 주세요.")
	}
	for _, question := range request.Questions {
		answers := reply.Answers[question.ID].Answers
		if len(answers) != 1 || strings.TrimSpace(answers[0]) == "" {
			return errors.New("각 질문에 답변을 하나씩 입력해 주세요.")
		}
		if len(question.Options) > 0 && !question.IsOther &&
			!slices.ContainsFunc(question.Options, func(option struct{ Label string }) bool { return option.Label == answers[0] }) {
			return errors.New("질문에 표시된 선택지를 골라 주세요.")
		}
	}
	return nil
}
