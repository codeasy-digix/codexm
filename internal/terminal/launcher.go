// Package terminal opens the official Codex TUI outside the manager screen.
package terminal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/supervisor"
)

type Launch struct {
	Terminal string `json:"terminal"`
	Session  string `json:"session,omitempty"`
	Attach   string `json:"attach,omitempty"`
	JobID    string `json:"job_id"`
	Account  string `json:"account"`
}

func (l Launch) String() string {
	if l.Attach != "" {
		return fmt.Sprintf("%s · 계정 %s · %s", l.Session, l.Account, l.Attach)
	}
	return fmt.Sprintf("새 %s 창 · 계정 %s · 작업 %s", l.Terminal, l.Account, l.JobID)
}

// Desktop launchers receive structured argv. Only Terminal's do script needs a
// shell command, constructed from quoted argv without interpolated AppleScript.
func viewerCommand(self string, c config.Config, id, clientID string) []string {
	return []string{self, "--config", c.Path, "__view", id, clientID}
}

func Open(ctx context.Context, c config.Config, self, id string) (Launch, error) {
	clientID := jobs.ID()
	var v supervisor.ViewState
	if e := supervisor.Request(ctx, c, "view", supervisor.JobArgs{ID: id, ClientID: clientID}, &v); e != nil {
		return Launch{}, e
	}
	args := viewerCommand(self, c, id, clientID)
	result, e := launch(ctx, args, id, v.Account, clientID)
	if e != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = supervisor.Request(cleanup, c, "detach", supervisor.JobArgs{ClientID: clientID}, nil)
		cancel()
	}
	return result, e
}

const terminalScript = `on run argv
tell application "Terminal"
  set newTab to do script (item 1 of argv)
  set custom title of newTab to (item 2 of argv)
  activate
end tell
end run`

func launch(ctx context.Context, args []string, id, account, clientID string) (Launch, error) {
	result := Launch{JobID: id, Account: account}
	ssh := os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != ""
	if runtime.GOOS == "darwin" && !ssh {
		quoted := make([]string, len(args))
		for i, a := range args {
			quoted[i] = cr.ShellQuote(a)
		}
		cmd := exec.CommandContext(ctx, "osascript", "-e", terminalScript, "--", strings.Join(quoted, " "), "codexm · "+account+" · "+id)
		if b, e := cmd.CombinedOutput(); e != nil {
			return result, fmt.Errorf("새 Terminal 창을 열지 못했습니다: %s (%w). 현재 터미널: codexm attach %s", cr.SafeText(string(b)), e, id)
		}
		result.Terminal = "Terminal"
		return result, nil
	}
	if runtime.GOOS == "linux" && !ssh && (os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "") {
		for _, entry := range []struct {
			name   string
			prefix []string
		}{{"gnome-terminal", []string{"--"}}, {"konsole", []string{"--separate", "-e"}}, {"x-terminal-emulator", []string{"-e"}}, {"xterm", []string{"-e"}}} {
			if binary, e := exec.LookPath(entry.name); e == nil {
				cmd := exec.Command(binary, append(entry.prefix, args...)...)
				// The terminal is a separate foreground UI, not an owned daemon.
				if e = cmd.Start(); e != nil {
					return result, e
				}
				go cmd.Wait()
				result.Terminal = entry.name
				return result, nil
			}
		}
	}
	if len(id) < 12 {
		return result, errors.New("invalid job ID for terminal session")
	}
	result.Terminal = "tmux"
	result.Session = "codexm-view-" + id[:12] + "-" + clientID[:6]
	result.Attach = "tmux attach -t " + result.Session
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = cr.ShellQuote(a)
	}
	if b, e := exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", result.Session, strings.Join(quoted, " ")).CombinedOutput(); e != nil {
		return result, fmt.Errorf("새 tmux 화면: %s: %w", cr.SafeText(string(b)), e)
	}
	return result, nil
}
