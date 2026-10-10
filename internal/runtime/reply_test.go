package runtime

import (
	"encoding/json"
	"github.com/codeasy-digix/codexm/internal/jobs"
	"testing"
)

func TestMalformedRepliesCannotLoseWaitingRequests(t *testing.T) {
	r := jobs.Request{Method: "item/commandExecution/requestApproval"}
	if ValidateReply(r, json.RawMessage(`{}`)) == nil {
		t.Fatal("missing decision accepted")
	}
	if e := ValidateReply(r, json.RawMessage(`{"decision":"decline"}`)); e != nil {
		t.Fatal(e)
	}
	r = jobs.Request{Method: "item/tool/requestUserInput", Params: json.RawMessage(`{"questions":[{"id":"one"},{"id":"two"}]}`)}
	if ValidateReply(r, json.RawMessage(`{"answers":{"one":{"answers":["ok"]}}}`)) == nil {
		t.Fatal("incomplete answers accepted")
	}
	if e := ValidateReply(r, json.RawMessage(`{"answers":{"one":{"answers":["한글"]},"two":{"answers":["ok"]}}}`)); e != nil {
		t.Fatal(e)
	}
}
