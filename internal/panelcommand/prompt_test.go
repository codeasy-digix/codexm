package panelcommand

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCommandsOnly(t *testing.T) {
	for prompt, want := range map[string]string{
		"@codexm manage": "/", "\t@codexm-manage\n": "/", "$codexm-manage": "/", "/manage": "/",
		"[@codexm](plugin://codexm@local-personal) manage":            "/",
		"[$codexm-manage](/tmp/plugin/skills/codexm-manage/SKILL.md)": "/",
		"@codexm accounts": "/accounts", "@codexm-accounts": "/accounts", "$codexm accounts": "/accounts",
		"": "", "@codexm": "", "@codexm manage then resume": "", "explain @codexm-manage": "",
		"`@codexm manage`": "", "@codexm manage; touch /tmp/no": "", "@codexm manage\ncontinue work": "",
		"[@codexm](https://example.com) manage": "", "[$codexm-manage](/tmp/other/SKILL.md)": "",
		"[@codexm](plugin://other@local-personal) manage": "",
	} {
		if got := Target(prompt, "local-personal"); got != want {
			t.Errorf("Target(%q) = %q, want %q", prompt, got, want)
		}
	}
}

func TestRecognizedCommandBlocksWithBoundedFixedURL(t *testing.T) {
	for _, fail := range []bool{false, true} {
		var out bytes.Buffer
		calls := 0
		err := Serve(context.Background(), strings.NewReader(`{"hook_event_name":"UserPromptSubmit","prompt":"@codexm accounts"}`), &out, "local-personal", func(ctx context.Context, link string) error {
			calls++
			if link != "codex://plugins/codexm@local-personal/app/codexm_open?path=%2Faccounts" {
				t.Fatal(link)
			}
			if _, bounded := ctx.Deadline(); !bounded {
				t.Fatal("unbounded opener")
			}
			if fail {
				return errors.New("private opener failure")
			}
			return nil
		})
		var result map[string]any
		if err != nil || json.Unmarshal(out.Bytes(), &result) != nil || result["decision"] != "block" || calls != 1 || strings.Contains(out.String(), "private") {
			t.Fatal(out.String(), calls, err)
		}
	}
}

func TestOrdinaryOrMalformedInputDoesNotOpenOrBlock(t *testing.T) {
	for _, input := range []string{`{}`, `invalid`, `{"hook_event_name":"Stop","prompt":"@codexm manage"}`, `{"hook_event_name":"UserPromptSubmit","prompt":"Fix the failing test"}`, `{"hook_event_name":"UserPromptSubmit","prompt":"@codexm manage"} {}`, strings.Repeat(" ", maxInput+1)} {
		var out bytes.Buffer
		err := Serve(context.Background(), strings.NewReader(input), &out, "local-personal", func(context.Context, string) error {
			t.Fatal("unexpected opener")
			return nil
		})
		if err != nil || out.String() != "{}\n" {
			t.Fatal(out.String(), err)
		}
	}
}

func TestInvalidMarketplaceStillBlocksCommand(t *testing.T) {
	var out bytes.Buffer
	err := Serve(context.Background(), strings.NewReader(`{"hook_event_name":"UserPromptSubmit","prompt":"@codexm manage"}`), &out, "bad;name", func(context.Context, string) error {
		t.Fatal("invalid link opened")
		return nil
	})
	if err != nil || !strings.Contains(out.String(), `"decision":"block"`) {
		t.Fatal(err, out.String())
	}
}
