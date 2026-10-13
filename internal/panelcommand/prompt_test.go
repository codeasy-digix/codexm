package panelcommand

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestCommandsOnly(t *testing.T) {
	for prompt, want := range map[string]string{
		"@codexm manage": "/", "\t@codexm-manage\n": "/", "$codexm-manage": "/", "/manage": "/",
		"[@codexm](plugin://codexm@local-personal) manage":            "/",
		"[$codexm-manage](/tmp/plugin/skills/codexm-manage/SKILL.md)": "/",
		"$codexm:codexm-manage":                                       "/", "@codexm:codexm-manage": "/",
		"[$codexm:codexm-manage](/tmp/plugin/skills/codexm-manage/SKILL.md)": "/",
		"[@codexm:codexm-manage](/tmp/plugin/skills/codexm-manage/SKILL.md)": "/",
		"[$codexm:codexm](/tmp/plugin/skills/codexm/SKILL.md) manage":        "/",
		"@codexm accounts": "/accounts", "@codexm-accounts": "/accounts", "$codexm accounts": "/accounts",
		"$codexm:codexm-accounts": "/accounts", "@codexm:codexm-accounts": "/accounts",
		"[$codexm:codexm-accounts](/tmp/plugin/skills/codexm-accounts/SKILL.md)": "/accounts",
		"[@codexm:codexm-accounts](/tmp/plugin/skills/codexm-accounts/SKILL.md)": "/accounts",
		"": "", "@codexm": "", "@codexm manage then resume": "", "explain @codexm-manage": "",
		"`@codexm manage`": "", "@codexm manage; touch /tmp/no": "", "@codexm manage\ncontinue work": "",
		"[@codexm](https://example.com) manage": "", "[$codexm-manage](/tmp/other/SKILL.md)": "",
		"[@codexm](plugin://other@local-personal) manage": "",
		"$other:codexm-manage":                            "", "$codexm:other-manage": "", "$codexm:codexm:codexm-manage": "",
		"$codexm:codexm": "", "$codexm:codexm-manage continue": "",
		"[$other:codexm-manage](/tmp/plugin/skills/codexm-manage/SKILL.md)":                  "",
		"[$codexm:codexm-manage](/tmp/plugin/skills/other/SKILL.md)":                         "",
		"[$codexm:codexm-manage](/tmp/plugin/skills/codexm-accounts/SKILL.md)":               "",
		"[$codexm:codexm-manage](https://example.com/codexm-manage/SKILL.md)":                "",
		"[$codexm:codexm-manage](/tmp/plugin/skills/codexm-manage/SKILL.md) 이 채팅에 적용되었는지 확인": "",
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
			outer, err := url.Parse(link)
			if err != nil {
				t.Fatal(err)
			}
			inner, err := url.Parse(outer.Query().Get("path"))
			if err != nil || outer.Scheme != "codex" || outer.Host != "plugins" || outer.Path != "/codexm@local-personal/app/codexm_open" || inner.Path != "/accounts" || len(inner.Query().Get("request")) != 16 {
				t.Fatal(link)
			}
			if _, bounded := ctx.Deadline(); !bounded {
				t.Fatal("unbounded opener")
			}
			if fail {
				return errors.New("private opener failure")
			}
			return nil
		}, nil)
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
		}, nil)
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
	}, nil)
	if err != nil || !strings.Contains(out.String(), `"decision":"block"`) {
		t.Fatal(err, out.String())
	}
}

func TestRegistrationCommandUsesOnlyHookSessionAndAlwaysBlocks(t *testing.T) {
	for _, prompt := range []string{"@codexm", "$codexm", "$codexm:codexm", "@codexm register", "[$codexm:codexm](/tmp/plugin/skills/codexm/SKILL.md)", "[@codexm](plugin://codexm@local-personal)"} {
		for _, fail := range []bool{false, true} {
			input, _ := json.Marshal(map[string]string{"hook_event_name": "UserPromptSubmit", "prompt": prompt, "session_id": "current-hook-session"})
			var out bytes.Buffer
			calls := 0
			err := Serve(context.Background(), bytes.NewReader(input), &out, "local-personal", func(context.Context, string) error {
				t.Fatal("registration opened a panel")
				return nil
			}, func(ctx context.Context, id string) (string, error) {
				calls++
				if _, bounded := ctx.Deadline(); !bounded || id != "current-hook-session" {
					t.Fatal("registration lost current session or deadline", id)
				}
				if fail {
					return "", errors.New("private registration failure")
				}
				return "codexm 관리 등록 완료", nil
			})
			var result map[string]any
			if err != nil || json.Unmarshal(out.Bytes(), &result) != nil || result["decision"] != "block" || calls != 1 || strings.Contains(out.String(), "private") {
				t.Fatal(prompt, out.String(), calls, err)
			}
		}
	}
	var out bytes.Buffer
	err := Serve(context.Background(), strings.NewReader(`{"hook_event_name":"UserPromptSubmit","prompt":"@codexm"}`), &out, "local-personal", nil, func(context.Context, string) (string, error) {
		t.Fatal("missing session ID must not register another chat")
		return "", nil
	})
	if err != nil || !strings.Contains(out.String(), `"decision":"block"`) {
		t.Fatal(err, out.String())
	}
}

func TestRegistrationRejectsOtherMentionsAndAdditionalInstructions(t *testing.T) {
	for _, prompt := range []string{"@codexm manage", "@codexm accounts", "@codexm register other-thread", "@codexm fix the test", "$other:codexm", "[$codexm:codexm](/tmp/other/SKILL.md)", "[@codexm](plugin://other@local-personal)"} {
		if Registers(prompt, "local-personal") {
			t.Fatal("unrelated input requested registration", prompt)
		}
	}
}
