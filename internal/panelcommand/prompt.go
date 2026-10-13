// Package panelcommand handles exact management commands before model inference.
// Registration is delegated to the manager; panel opening uses the OS link handler.
package panelcommand

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

var mention = regexp.MustCompile(`^\[([@$](?:codexm:)?codexm(?:-(?:manage|accounts))?)\]\(([^)\r\n]+)\)(?:[ \t]+(manage|accounts|register))?$`)
var marketplaceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// commandText only normalizes our own complete mentions and skill links.
func commandText(prompt, marketplace string) string {
	prompt = strings.TrimSpace(prompt)
	if parts := mention.FindStringSubmatch(prompt); parts != nil {
		name := strings.TrimPrefix(strings.TrimLeft(parts[1], "@$"), "codexm:")
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
	// The desktop skill picker qualifies bundled skills with their plugin name.
	// Strip only our exact namespace; another plugin's skill is not this command.
	if strings.HasPrefix(prompt, "$codexm:") || strings.HasPrefix(prompt, "@codexm:") {
		prompt = prompt[:1] + strings.TrimPrefix(prompt[1:], "codexm:")
	}
	return strings.Join(strings.Fields(prompt), " ")
}

// Target only matches complete panel commands. A request that quotes a command
// or adds further work is a normal prompt, not authorization to open a panel.
func Target(prompt, marketplace string) string {
	switch commandText(prompt, marketplace) {
	case "@codexm manage", "$codexm manage", "@codexm-manage", "$codexm-manage", "/manage":
		return "/"
	case "@codexm accounts", "$codexm accounts", "@codexm-accounts", "$codexm-accounts", "/codexm-accounts":
		return "/accounts"
	}
	return ""
}

func Registers(prompt, marketplace string) bool {
	switch commandText(prompt, marketplace) {
	case "@codexm", "$codexm", "@codexm register", "$codexm register":
		return true
	}
	return false
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
func Serve(ctx context.Context, in io.Reader, out io.Writer, marketplace string, open func(context.Context, string) error, register func(context.Context, string) (string, error)) error {
	b, err := io.ReadAll(io.LimitReader(in, maxInput+1))
	var event struct {
		Event     string `json:"hook_event_name"`
		Prompt    string `json:"prompt"`
		SessionID string `json:"session_id"`
	}
	if err != nil || len(b) > maxInput || json.Unmarshal(b, &event) != nil || event.Event != "UserPromptSubmit" {
		_, err = io.WriteString(out, "{}\n")
		return err
	}
	if Registers(event.Prompt, marketplace) {
		reason := "codexm 등록 결과를 확인하지 못했습니다. 관리 패널에서 현재 채팅의 등록 상태와 서비스 설정을 확인하세요. 이 명령은 모델에 전달하지 않았습니다."
		if event.SessionID != "" && register != nil {
			limited, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			// The callback supplies a user-safe summary even for setup failures.
			// Raw errors may include private RPC data and are never displayed.
			if summary, _ := register(limited, event.SessionID); summary != "" {
				reason = summary + " 이 명령은 모델에 전달하지 않았습니다."
			}
		}
		return json.NewEncoder(out).Encode(map[string]any{"decision": "block", "reason": reason})
	}
	target := Target(event.Prompt, marketplace)
	if target == "" {
		_, err = io.WriteString(out, "{}\n")
		return err
	}
	link, err := Link(marketplace, target)
	if err == nil {
		// A fresh app-relative query causes the host to deliver a new navigation
		// even when this panel is already open at the same deep-link URL.
		var nonce [8]byte
		_, err = rand.Read(nonce[:])
		if err == nil {
			base, _, _ := strings.Cut(link, "?")
			link = base + "?path=" + url.QueryEscape(target+"?request="+hex.EncodeToString(nonce[:]))
		}
	}
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
