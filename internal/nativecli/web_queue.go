package nativecli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

type webQueuePage struct {
	Data       []json.RawMessage `json:"data"`
	NextCursor *string           `json:"nextCursor"`
}

func readWebQueue(ctx context.Context, client *cr.Client, thread string) (webQueuePage, error) {
	page := webQueuePage{Data: []json.RawMessage{}}
	// This is the native queue's full capacity in the supported runtime.
	err := client.Call(ctx, "thread/queue/list", map[string]any{"threadId": thread, "limit": 100}, &page)
	return page, err
}

var (
	errWebQueueGone      = errors.New("메시지가 대기열에 없습니다. 작업이 시작됐는지 대화에서 확인해 주세요.")
	errWebQueueChanged   = errors.New("대기 메시지가 바뀌었습니다. 갱신된 내용을 확인해 주세요.")
	errWebQueueUncertain = errors.New("큐 변경 결과를 확인하지 못했습니다. 대화와 대기열을 확인해 주세요.")
)

func verifyQueuedText(ctx context.Context, client *cr.Client, p webParams) error {
	queue, err := readWebQueue(ctx, client, p.Thread)
	if err != nil {
		return err
	}
	for _, entry := range queue.Data {
		var item struct {
			ID    string
			Input []struct {
				Type         string
				Text         *string
				TextElements []json.RawMessage `json:"text_elements"`
			}
		}
		if err := json.Unmarshal(entry, &item); err != nil {
			return err
		}
		if item.ID != p.Queued {
			continue
		}
		if len(item.Input) != 1 || item.Input[0].Type != "text" ||
			item.Input[0].Text == nil || len(item.Input[0].TextElements) != 0 {
			return errors.New("첨부나 서식이 있는 대기 메시지는 원래 Codex 창에서 편집해 주세요.")
		}
		if *item.Input[0].Text != *p.ExpectedText {
			return errWebQueueChanged
		}
		return nil
	}
	return errWebQueueGone
}

func (w *WebChat) changeQueue(ctx context.Context, c *webConnection, method string, p webParams) (any, error) {
	if strings.EqualFold(p.Thread, w.c.Runtime.WebGUIRelay) {
		return nil, errors.New("이 채팅은 GUI 메시지 중계 전용입니다.")
	}
	if w.webOnly && strings.HasPrefix(p.Queued, webQueueID) {
		return w.changeOwnedQueue(method, p)
	}
	if !threadID.MatchString(p.Queued) || p.ExpectedText == nil {
		return nil, errors.New("대기열에서 변경할 메시지를 선택해 주세요.")
	}
	if method == "web.queueUpdate" && strings.TrimSpace(p.Text) == "" {
		return nil, errors.New("대기 메시지를 입력해 주세요.")
	}
	// Use the verified owner's dispatch guard if it exposes a connection.
	// Stdio-only owners and unloaded chats share the official durable queue.
	client := c.client
	probe, cancel := context.WithTimeout(ctx, 6*time.Second)
	owners, err := webWriterPIDs(probe, config.Expand(w.c.Runtime.SharedCodexHome), p.Thread)
	otherOwner := err != nil || len(owners) > 1 || (len(owners) == 1 && owners[0] != c.process.PID)
	var owner *webInputOwner
	if err == nil && len(owners) == 1 {
		owner, _ = w.openInputOwner(probe, p.Thread, owners[0])
		if owner != nil {
			defer owner.release()
			client = owner.client
			otherOwner = false
		}
	}
	cancel()
	if w.webOnly && otherOwner {
		return nil, errWebOtherOwner
	}
	if err := verifyQueuedText(ctx, client, p); err != nil {
		return nil, err
	}
	if owner != nil {
		if err := w.verifyInputOwner(ctx, owner, p.Thread, false); err != nil {
			return nil, err
		}
	}
	args := map[string]any{"threadId": p.Thread, "queuedSubmissionId": p.Queued}
	if method == "web.queueCancel" {
		var reply struct{ Deleted *bool }
		if err := callWebInput(ctx, client, "thread/queue/delete", args, &reply); err != nil {
			return nil, webQueueMutationError(err, p.Queued)
		}
		if reply.Deleted == nil {
			return nil, errWebQueueUncertain
		}
		if !*reply.Deleted {
			return nil, errWebQueueGone
		}
		return map[string]any{"queued_id": p.Queued, "deleted": true, "check_history": otherOwner}, nil
	}
	args["input"] = cr.TextInput(p.Text)
	var reply struct {
		Submission struct{ ID string } `json:"queuedSubmission"`
	}
	if err := callWebInput(ctx, client, "thread/queue/update", args, &reply); err != nil {
		return nil, webQueueMutationError(err, p.Queued)
	}
	if reply.Submission.ID != p.Queued {
		return nil, errWebQueueUncertain
	}
	return map[string]any{"queued_id": p.Queued, "updated": true}, nil
}

func webQueueMutationError(err error, id string) error {
	if errors.Is(err, errWebInputUnconfirmed) {
		return errWebQueueUncertain
	}
	var rpc *cr.RPCError
	if errors.As(err, &rpc) && rpc.Code == -32600 && rpc.Message == "queued submission not found: "+id {
		return errWebQueueGone
	}
	return err
}
