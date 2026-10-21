package supervisor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"

	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

// Only this explicit surface can be used through a reconnecting web client.
var webMutations = []string{"new", "resume", "register", "unregister", "manage", "send", "pause", "stop", "resume-job", "complete", "reply", "switch-account", "goal", "manager", "settings-save", "resolve-action"}
var operationID = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

type WebOperation struct {
	ID      string          `json:"operation_id"`
	Command string          `json:"command"`
	Args    json.RawMessage `json:"args"`
}

func (s *Server) webOperation(ctx context.Context, raw json.RawMessage) (any, error) {
	var request WebOperation
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&request) != nil || !operationID.MatchString(request.ID) || !slices.Contains(webMutations, request.Command) {
		return nil, errors.New("invalid web operation")
	}
	var args map[string]json.RawMessage
	if json.Unmarshal(request.Args, &args) != nil || args == nil {
		return nil, errors.New("invalid operation arguments")
	}
	if request.Command == "reply" || request.Command == "switch-account" {
		var run string
		if json.Unmarshal(args["run_id"], &run) != nil || request.Command == "reply" && run == "" {
			return nil, errors.New("the expected run ID is required; refresh this conversation")
		}
	}
	// Canonical encoding permits identical objects with different key order.
	decoder := json.NewDecoder(bytes.NewReader(request.Args))
	decoder.UseNumber()
	var canonical any
	if err := decoder.Decode(&canonical); err != nil {
		return nil, err
	}
	normalized, _ := json.Marshal(canonical)
	hash := sha256.Sum256(append([]byte(request.Command+"\x00"), normalized...))
	op, claimed, err := s.Store.ClaimOperation(request.ID, hex.EncodeToString(hash[:]), request.Command)
	if err != nil {
		return nil, err
	}
	if op.Hash != hex.EncodeToString(hash[:]) {
		return nil, errors.New("operation_conflict: this operation ID was used with different input")
	}
	if !claimed {
		switch op.State {
		case "done":
			return op.Result, nil
		case "failed":
			return nil, errors.New(op.Error)
		default:
			return nil, errors.New("outcome_unknown: inspect the operation and conversation before submitting another request")
		}
	}
	if request.Command == "switch-account" {
		args["operation_id"], _ = json.Marshal(request.ID)
		// Empty is a real expected state for a registered, never-run thread.
		args["expected_run_id"] = args["run_id"]
		normalized, _ = json.Marshal(args)
	}
	result, callErr := s.Handle(ctx, Call{Command: request.Command, Args: normalized})
	if ctx.Err() != nil || errors.Is(callErr, errUncertain) {
		reason := "outcome_unknown: dispatch may have completed; inspect the operation and conversation before submitting again"
		_ = s.Store.UnknownOperation(request.ID, reason)
		return nil, errors.New(reason)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	failure := ""
	if callErr != nil {
		failure = cr.SafeText(callErr.Error())
	}
	if err = s.Store.FinishOperation(request.ID, encoded, failure); err != nil {
		return nil, errors.New("outcome_unknown: result could not be recorded; inspect the conversation")
	}
	return result, callErr
}
