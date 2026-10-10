package runtime

import (
	"encoding/json"
	"errors"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"slices"
)

func ValidateReply(r jobs.Request, b json.RawMessage) error {
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil || m == nil {
		return errors.New("request reply must be a JSON object")
	}
	switch r.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		var d string
		if json.Unmarshal(m["decision"], &d) != nil || !slices.Contains([]string{"accept", "acceptForSession", "decline", "cancel"}, d) {
			return errors.New("approval decision must be accept, acceptForSession, decline or cancel")
		}
	case "item/tool/requestUserInput":
		var p struct {
			Questions []struct {
				ID string `json:"id"`
			} `json:"questions"`
		}
		if json.Unmarshal(r.Params, &p) != nil {
			return errors.New("invalid input request")
		}
		var answers map[string]struct {
			Answers []string `json:"answers"`
		}
		if json.Unmarshal(m["answers"], &answers) != nil || answers == nil {
			return errors.New("input response needs an answers object")
		}
		for _, q := range p.Questions {
			a, ok := answers[q.ID]
			if !ok || len(a.Answers) == 0 {
				return errors.New("answer each requested question")
			}
		}
		if len(answers) != len(p.Questions) {
			return errors.New("answer IDs do not match requested questions")
		}
	case "mcpServer/elicitation/request":
		var a string
		if json.Unmarshal(m["action"], &a) != nil || !slices.Contains([]string{"accept", "decline", "cancel"}, a) {
			return errors.New("elicitation action must be accept, decline or cancel")
		}
	case "item/permissions/requestApproval":
		if _, ok := m["permissions"]; !ok {
			return errors.New("permission reply needs a permissions object")
		}
	default:
		return errors.New("respond to this request through the official Codex TUI")
	}
	return nil
}
