package nativecli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	"golang.org/x/sys/unix"
)

var sessionName = regexp.MustCompile(`^codexm-cli-[a-f0-9]{16}$`)

func sessionPath(c config.Config, s string) string {
	return filepath.Join(root(c), "sessions", s+".json")
}
func saveSession(c config.Config, l Launch) error {
	b, e := json.Marshal(l)
	if e != nil {
		return e
	}
	return config.AtomicWrite(sessionPath(c, l.Session), b, 0600)
}
func tmux(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	b, e := exec.CommandContext(ctx, "tmux", args...).CombinedOutput()
	if e != nil {
		return "", fmt.Errorf("tmux: %w: %s", e, strings.TrimSpace(string(b)))
	}
	return string(b), nil
}
func readSession(c config.Config, s string) (Launch, error) {
	var l Launch
	if !sessionName.MatchString(s) {
		return l, errors.New("invalid native session")
	}
	b, e := os.ReadFile(sessionPath(c, s))
	if e != nil {
		return l, e
	}
	e = json.Unmarshal(b, &l)
	if e == nil && (l.Session != s || !threadID.MatchString(l.Thread) || !regexp.MustCompile(`^%[0-9]+$`).MatchString(l.Pane)) {
		e = errors.New("invalid native session receipt")
	}
	return l, e
}

// Open creates a named terminal only. There is no supervisor, RPC worker or model
// request. Reopening the same live thread/account attaches to its existing CLI.
func Open(ctx context.Context, c config.Config, self, id, account string) (Launch, error) {
	l, e := Plan(ctx, c, id, account)
	if e != nil {
		return l, e
	}
	if _, e = exec.LookPath("tmux"); e != nil {
		return l, errors.New("tmux is required for web/native terminal sessions")
	}
	h := sha256.Sum256([]byte(config.Expand(c.Runtime.SharedCodexHome) + "\x00" + id + "\x00" + account))
	l.Session = "codexm-cli-" + hex.EncodeToString(h[:8])
	if e = os.MkdirAll(root(c), 0700); e != nil {
		return l, e
	}
	f, e := os.OpenFile(filepath.Join(root(c), "launch.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return l, e
	}
	defer f.Close()
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX); e != nil {
		return l, e
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	if old, e := readSession(c, l.Session); e == nil {
		screen, e := Screen(ctx, c, l.Session)
		if e != nil {
			return l, e
		}
		if screen.Alive {
			return old, nil
		}
		_, _ = tmux(ctx, "kill-session", "-t", "="+l.Session)
	}
	args := []string{"new-session", "-d", "-P", "-F", "#{pane_id}", "-s", l.Session, "-x", "110", "-y", "36", "-c", l.Cwd}
	// A long-lived tmux server can retain an obsolete PATH/locale. Resolve the
	// native CLI and XDG paths in the same environment as the requesting client.
	for _, key := range []string{"PATH", "HOME", "LANG", "LC_ALL", "LC_CTYPE", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
		if value, ok := os.LookupEnv(key); ok {
			args = append(args, "-e", key+"="+value)
		}
	}
	args = append(args, "/bin/sleep", "60")
	pane, e := tmux(ctx, args...)
	if e != nil {
		return l, e
	}
	l.Pane = strings.TrimSpace(pane)
	complete := false
	defer func() {
		if !complete {
			_, _ = tmux(context.Background(), "kill-session", "-t", "="+l.Session)
		}
	}()
	if _, e = tmux(ctx, "set-option", "-w", "-t", l.Pane, "remain-on-exit", "on"); e != nil {
		return l, e
	}
	// set-option's session target does not accept the '=' exact-match prefix on
	// supported tmux versions; this exact, freshly-created name is already owned.
	if _, e = tmux(ctx, "set-option", "-t", l.Session, "history-limit", "10000"); e != nil {
		return l, e
	}
	if e = saveSession(c, l); e != nil {
		return l, e
	}
	if _, e = tmux(ctx, "respawn-pane", "-k", "-t", l.Pane, "-c", l.Cwd, self, "--config", c.Path, "__native-run", sessionPath(c, l.Session)); e != nil {
		return l, e
	}
	complete = true
	return l, nil
}

func RunSession(ctx context.Context, c config.Config, filename string) error {
	s := strings.TrimSuffix(filepath.Base(filename), ".json")
	if filepath.Clean(filename) != sessionPath(c, s) {
		return errors.New("native launch spec must be in the owned state directory")
	}
	l, e := readSession(c, s)
	if e != nil {
		return e
	}
	// Revalidate local index, selected login and executable immediately before use.
	latest, e := Plan(ctx, c, l.Thread, l.Account)
	if e == nil {
		l.Binary, l.Cwd = latest.Binary, latest.Cwd
		e = Prepare(ctx, c, l)
	}
	if e != nil {
		l.Error = e.Error()
		_ = saveSession(c, l)
		return e
	}
	l.Ready = true
	if e = saveSession(c, l); e != nil {
		return e
	}
	if e = writeReceipt(c, l); e != nil {
		return e
	}
	if e = os.Chdir(l.Cwd); e != nil {
		return e
	}
	e = syscall.Exec(l.Binary, ResumeArgs(l), environment(c, l.Account))
	l.Ready = false
	l.Error = e.Error()
	_ = saveSession(c, l)
	return e
}

type Terminal struct {
	Launch   Launch `json:"launch"`
	Alive    bool   `json:"alive"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Text     string `json:"text"`
	Attach   string `json:"attach"`
}

func Screen(ctx context.Context, c config.Config, s string) (Terminal, error) {
	l, e := readSession(c, s)
	if e != nil {
		return Terminal{}, e
	}
	t := Terminal{Launch: l, Attach: "tmux attach -t " + s}
	// tmux display-message can fall back to its current pane for a stale target.
	// A closed session is simply exited, even while its last receipt remains.
	if _, e = tmux(ctx, "has-session", "-t", "="+s); e != nil {
		return t, nil
	}
	value, e := tmux(ctx, "display-message", "-p", "-t", l.Pane, "#{session_name}|#{pane_dead}|#{pane_dead_status}")
	if e != nil {
		if _, exists := tmux(ctx, "has-session", "-t", "="+s); exists == nil {
			return t, errors.New("recorded native pane is missing from an existing session; inspect it manually")
		}
		return t, nil
	}
	parts := strings.Split(strings.TrimSpace(value), "|")
	if (len(parts) != 2 && len(parts) != 3) || parts[0] != s {
		return t, errors.New("native pane no longer belongs to the recorded session")
	}
	t.Alive = parts[1] == "0"
	if !t.Alive && len(parts) == 3 {
		if code, err := strconv.Atoi(parts[2]); err == nil {
			t.ExitCode = &code
		}
	}
	t.Text, e = tmux(ctx, "capture-pane", "-p", "-J", "-t", l.Pane, "-S", "-1500")
	// capture-pane pads short output with the empty viewport rows. Do not scroll
	// the browser past all visible text just to display those blank rows.
	t.Text = strings.TrimRight(t.Text, " \t\r\n")
	return t, e
}
func Input(ctx context.Context, c config.Config, s, text, key string) error {
	t, e := Screen(ctx, c, s)
	if e != nil {
		return e
	}
	if !t.Alive {
		return errors.New("the official CLI has exited; reopen the chat")
	}
	if !t.Launch.Ready {
		return errors.New("official CLI is still preparing the resume")
	}
	if (text == "") == (key == "") {
		return errors.New("send either text or one terminal key")
	}
	if text != "" {
		if len(text) > 64*1024 {
			return errors.New("terminal input is too large")
		}
		for _, r := range text {
			if (r < 32 && r != '\n' && r != '\t') || r == 127 {
				return errors.New("control characters are not accepted in text")
			}
		}
		buffer := "codexm-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		cmd := exec.CommandContext(ctx, "tmux", "load-buffer", "-b", buffer, "-")
		cmd.Stdin = strings.NewReader(text)
		if e = cmd.Run(); e != nil {
			return e
		}
		defer tmux(context.Background(), "delete-buffer", "-b", buffer)
		if _, e = tmux(ctx, "paste-buffer", "-p", "-b", buffer, "-t", t.Launch.Pane); e != nil {
			return e
		}
		_, e = tmux(ctx, "send-keys", "-t", t.Launch.Pane, "Enter")
		return e
	}
	allowed := map[string]bool{"Enter": true, "Escape": true, "Up": true, "Down": true, "Left": true, "Right": true, "Tab": true, "BTab": true, "C-c": true, "C-d": true, "PageUp": true, "PageDown": true}
	if !allowed[key] {
		return errors.New("unsupported terminal key")
	}
	_, e = tmux(ctx, "send-keys", "-t", t.Launch.Pane, key)
	return e
}
func Close(ctx context.Context, c config.Config, s string) error {
	t, e := Screen(ctx, c, s)
	if e != nil {
		return e
	}
	if _, e = tmux(ctx, "has-session", "-t", "="+t.Launch.Session); e != nil {
		return nil
	}
	_, e = tmux(ctx, "kill-session", "-t", "="+t.Launch.Session)
	return e
}
func Sessions(c config.Config) []Launch {
	all := []Launch{}
	paths, _ := filepath.Glob(filepath.Join(root(c), "sessions", "codexm-cli-*.json"))
	for _, p := range paths {
		if l, e := readSession(c, strings.TrimSuffix(filepath.Base(p), ".json")); e == nil {
			all = append(all, l)
		}
	}
	return all
}
