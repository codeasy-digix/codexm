// Package panelcommand handles a small set of literal UI commands before Codex
// sends a user prompt to a model. It never reads accounts or conversation data.
package panelcommand

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const maxInput = 1024 * 1024

var mention = regexp.MustCompile(`^\[([@$]codexm(?:-(?:manage|accounts))?)\]\(([^)\r\n]+)\)(?:[ \t]+(manage|accounts))?$`)
var marketplaceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// Target only matches complete commands. A request that quotes a command or
// adds further work is a normal prompt, not authorization to open a panel.
func Target(prompt, marketplace string) string {
	prompt = strings.TrimSpace(prompt)
	if parts := mention.FindStringSubmatch(prompt); parts != nil {
		name := strings.TrimLeft(parts[1], "@$")
		target := parts[2]
		valid := target == "plugin://codexm@"+marketplace
		// Skill selection may serialize as a Markdown link to SKILL.md.
		valid = valid || filepath.IsAbs(target) && filepath.Base(target) == "SKILL.md" && filepath.Base(filepath.Dir(target)) == name
		if !valid {
			return ""
		}
		prompt = parts[1]
		if parts[3] != "" {
			prompt += " " + parts[3]
		}
	}
	switch strings.Join(strings.Fields(prompt), " ") {
	case "@codexm manage", "$codexm manage", "@codexm-manage", "$codexm-manage", "/manage":
		return "/"
	case "@codexm accounts", "$codexm accounts", "@codexm-accounts", "$codexm-accounts", "/codexm-accounts":
		return "/accounts"
	}
	return ""
}

func Link(marketplace, target string) (string, error) {
	if !marketplaceName.MatchString(marketplace) || target != "/" && target != "/accounts" {
		return "", errors.New("invalid panel command target")
	}
	return "codex://plugins/codexm@" + marketplace + "/app/codexm_open?path=" + url.QueryEscape(target), nil
}

// Serve always blocks a recognized command, including when the OS link opener
// fails. Other prompts produce an empty successful hook response. No input is
// logged, interpolated into a shell command, or sent to a model by this handler.
func Serve(ctx context.Context, in io.Reader, out io.Writer, marketplace string, open func(context.Context, string) error) error {
	b, err := io.ReadAll(io.LimitReader(in, maxInput+1))
	var event struct {
		Event  string `json:"hook_event_name"`
		Prompt string `json:"prompt"`
	}
	if err != nil || len(b) > maxInput || json.Unmarshal(b, &event) != nil || event.Event != "UserPromptSubmit" {
		_, err = io.WriteString(out, "{}\n")
		return err
	}
	target := Target(event.Prompt, marketplace)
	if target == "" {
		_, err = io.WriteString(out, "{}\n")
		return err
	}
	link, err := Link(marketplace, target)
	if err == nil {
		limited, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		err = open(limited, link)
	}
	reason := "codexm 관리 패널 열기를 앱에 요청했습니다. 이 명령은 모델에 전달하지 않았습니다."
	if err != nil {
		reason = "codexm 관리 패널을 열지 못했습니다. Codex 데스크톱 앱과 codexm 플러그인 설치를 확인하세요. 이 명령은 모델에 전달하지 않았습니다."
	}
	return json.NewEncoder(out).Encode(map[string]any{"decision": "block", "reason": reason})
}

func Open(ctx context.Context, link string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.CommandContext(ctx, "/usr/bin/open", link).Run()
	case "linux":
		// A desktop with a registered codex:// handler is required.
		return exec.CommandContext(ctx, "xdg-open", link).Run()
	default:
		return errors.New("desktop link opener unavailable")
	}
}
