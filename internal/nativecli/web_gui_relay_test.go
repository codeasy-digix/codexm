package nativecli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
)

func TestWebGUIRelayClaimsOnceAndKeepsMessageLiteral(t *testing.T) {
	c := config.Defaults()
	c.Storage.StateDir = t.TempDir()
	c.Runtime.WebGUIRelay = "00000000-0000-4000-8000-000000000001"
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const target = "00000000-0000-4000-8000-000000000002"
	const input = "한글 입력\n`echo literal` $(also literal)"
	receipt := webGUIRelayReceipt{Browser: sha256.Sum256([]byte("fixture-browser")), Relay: c.Runtime.WebGUIRelay,
		Thread: target, Text: input, State: "pending", ExpiresAt: time.Now().Add(webRelayTimeout)}
	if err := saveGUIRelay(c, id, receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := webGUIRelayStatus(c, "other-browser", target, id); err == nil {
		t.Fatal("another browser could read a relay receipt")
	}
	if _, err := webGUIRelayStatus(c, "fixture-browser", receipt.Relay, id); err == nil {
		t.Fatal("the receipt was not bound to its exact target")
	}
	check := func(context.Context, config.Config, webGUIRelayReceipt) error { return nil }
	type result struct {
		output string
		err    error
	}
	claims := make(chan result, 2)
	for range 2 {
		go func() {
			var output bytes.Buffer
			err := claimGUIRelay(t.Context(), c, id, check, &output)
			claims <- result{output.String(), err}
		}()
	}
	count := 0
	for range 2 {
		claim := <-claims
		if claim.err != nil {
			if claim.output != "" {
				t.Fatal("failed claim returned a message")
			}
			continue
		}
		count++
		var got map[string]string
		if err := json.Unmarshal([]byte(claim.output), &got); err != nil || got["thread_id"] != target || got["text"] != input {
			t.Fatal("relay changed the destination or the user text", err)
		}
	}
	if count != 1 {
		t.Fatal("only one relay claim should succeed", count)
	}
	if saved, err := readGUIRelay(c, id); err != nil || saved.State != "claimed" || saved.Text != "" {
		t.Fatal("a claimed request must retain no message body", err)
	}
	if prompt := guiRelayPrompt("/a path/codexm", "/a user's/config.toml", id); strings.Contains(prompt, target) || strings.Contains(prompt, input) {
		t.Fatal("the native queue entry must not contain the destination or message")
	}
}

func TestWebGUIRelayDoesNotSendExpiredInput(t *testing.T) {
	c := config.Defaults()
	c.Storage.StateDir = t.TempDir()
	c.Runtime.WebGUIRelay = "00000000-0000-4000-8000-000000000001"
	const id = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	receipt := webGUIRelayReceipt{Browser: sha256.Sum256([]byte("fixture-browser")), Relay: c.Runtime.WebGUIRelay,
		Thread: "00000000-0000-4000-8000-000000000002", Text: "expired payload", State: "pending", ExpiresAt: time.Now().Add(-time.Second)}
	if err := saveGUIRelay(c, id, receipt); err != nil {
		t.Fatal(err)
	}
	check := func(context.Context, config.Config, webGUIRelayReceipt) error {
		t.Fatal("an expired request must not reach ownership verification")
		return nil
	}
	if err := claimGUIRelay(t.Context(), c, id, check, io.Discard); err == nil {
		t.Fatal("expired request was claimed")
	}
	status, err := webGUIRelayStatus(c, "fixture-browser", receipt.Thread, id)
	if err != nil || status.(map[string]string)["status"] != "expired" {
		t.Fatal("expiry not reported", status, err)
	}
	if saved, err := readGUIRelay(c, id); err != nil || saved.Text != "" {
		t.Fatal("expired request retained private content", err)
	}
}
