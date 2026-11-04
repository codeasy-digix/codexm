package nativecli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
)

type ActivationRequest struct {
	Account    string `json:"account"`
	Revision   string `json:"revision"`
	Expression string `json:"expression"`
}

// ActivateUsage retains the toolbar's explicitly enabled, allowance-consuming
// arithmetic request. There is no scheduler here and it cannot resume a chat.
func ActivateUsage(ctx context.Context, c config.Config, args ActivationRequest) error {
	if !regexp.MustCompile(`^[0-9]{1,5} \* [0-9]{1,3} / [1-9][0-9]{0,2} \+ [0-9]{1,3}$`).MatchString(args.Expression) {
		return errors.New("invalid activation expression")
	}
	all, e := AccountCatalog(c)
	if e != nil {
		return e
	}
	if !slices.ContainsFunc(all, func(a DesktopAccount) bool {
		return a.Name == args.Account && a.Revision == args.Revision && a.Key != ""
	}) {
		return errors.New("account changed; refresh before requesting usage activation")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	r, e := Resolver(c)
	if e != nil {
		return e
	}
	name := args.Account
	if name == "default" {
		name = ""
	}
	p, e := r.Probe(ctx, name)
	if e != nil {
		return e
	}
	defer p.Close()
	if accounts.InspectIdentity(c, args.Account).Revision != args.Revision {
		return errors.New("account changed while opening the activation connection")
	}
	var models struct {
		Data []struct {
			Model   string `json:"model"`
			ID      string `json:"id"`
			Default bool   `json:"isDefault"`
			Effort  string `json:"defaultReasoningEffort"`
			Efforts []struct {
				Effort string `json:"reasoningEffort"`
			} `json:"supportedReasoningEfforts"`
		} `json:"data"`
	}
	if e = p.Call(ctx, "model/list", map[string]any{"limit": 100, "includeHidden": false}, &models); e != nil {
		return e
	}
	if len(models.Data) == 0 {
		return errors.New("no available model for usage activation")
	}
	selected := 0
	best := -1
	for i, m := range models.Data {
		model := m.Model
		if model == "" {
			model = m.ID
		}
		score := 0
		if m.Default {
			score = 1
		}
		if strings.Contains(model, "mini") || strings.Contains(model, "terra") {
			score = 2
		}
		if strings.Contains(model, "nano") || strings.Contains(model, "luna") {
			score = 3
		}
		if score > best {
			best = score
			selected = i
		}
	}
	m := models.Data[selected]
	model := m.Model
	if model == "" {
		model = m.ID
	}
	effort := m.Effort
	for _, v := range m.Efforts {
		if v.Effort == "low" {
			effort = "low"
		}
	}
	var thread struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if e = p.Call(ctx, "thread/start", map[string]any{"cwd": os.TempDir(), "approvalPolicy": "never", "sandbox": "read-only", "ephemeral": true, "model": model, "serviceName": "codexm_usage_activation", "developerInstructions": "Solve only the supplied arithmetic. Do not use tools, read files, browse, or modify anything. Return only the number."}, &thread); e != nil {
		return e
	}
	if thread.Thread.ID == "" {
		return errors.New("activation thread was not created")
	}
	params := map[string]any{"threadId": thread.Thread.ID, "input": []map[string]string{{"type": "text", "text": args.Expression}}, "approvalPolicy": "never", "sandboxPolicy": map[string]any{"type": "readOnly", "networkAccess": false}, "model": model, "summary": "none"}
	if effort != "" {
		params["effort"] = effort
	}
	var response json.RawMessage
	if e = p.Call(ctx, "turn/start", params, &response); e != nil {
		return e
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.Done:
			return errors.New("activation disconnected; outcome is unknown and was not replayed")
		case event, ok := <-p.Events:
			if !ok {
				return errors.New("activation connection ended")
			}
			if event.Method != "turn/completed" {
				continue
			}
			var done struct {
				ThreadID string `json:"threadId"`
				Turn     struct {
					Status string `json:"status"`
				} `json:"turn"`
			}
			if json.Unmarshal(event.Params, &done) != nil || done.ThreadID != thread.Thread.ID {
				continue
			}
			if done.Turn.Status != "completed" {
				return errors.New("activation did not complete; it was not replayed")
			}
			return nil
		}
	}
}
