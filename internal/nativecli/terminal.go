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
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"golang.org/x/sys/unix"
)

const CodexTmuxServer = "codex"

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

// Empty receipts predate server isolation and belong to the default socket.
func tmuxArgs(server string, args ...string) []string {
	if server == "" {
		server = "default"
	}
	return append([]string{"-L", server}, args...)
}
func tmux(ctx context.Context, server string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	b, e := exec.CommandContext(ctx, "tmux", tmuxArgs(server, args...)...).CombinedOutput()
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
	if e == nil && ((l.TmuxServer != "" && l.TmuxServer != CodexTmuxServer) || l.Session != s || !threadID.MatchString(l.Thread) || !regexp.MustCompile(`^%[0-9]+$`).MatchString(l.Pane)) {
		e = errors.New("invalid native session receipt")
	}
	return l, e
}

func terminalName(c config.Config, id, account string) string {
	h := sha256.Sum256([]byte(config.Expand(c.Runtime.SharedCodexHome) + "\x00" + id + "\x00" + account))
	return "codexm-cli-" + hex.EncodeToString(h[:8])
}

// ReusableTerminal finds an already-running CLI without acquiring its writer,
// archiving the chat or sending input. A receipt alone is not proof: the exact
// live pane must still run Codex and hold this chat's writer-lock file open.
func ReusableTerminal(ctx context.Context, c config.Config, id, account string) (*Launch, error) {
	for _, l := range Sessions(c) {
		if l.Thread != id || l.Session != terminalName(c, l.Thread, l.Account) {
			continue
		}
		screen, err := Screen(ctx, c, l.Session)
		if err != nil {
			return nil, err
		}
		if !screen.Alive {
			continue
		}
		if !l.Ready {
			return nil, fmt.Errorf("existing CLI is still preparing this chat; inspect with codexm terminal %s", l.Session)
		}
		info, err := tmux(ctx, l.TmuxServer, "display-message", "-p", "-t", l.Pane, "#{session_name}|#{pane_pid}|#{pane_current_command}")
		if err != nil {
			return nil, err
		}
		parts := strings.Split(strings.TrimSpace(info), "|")
		if len(parts) != 3 || parts[0] != l.Session || parts[2] != filepath.Base(l.Binary) {
			return nil, fmt.Errorf("recorded terminal no longer identifies the expected Codex process; inspect with codexm terminal %s", l.Session)
		}
		pid, err := strconv.Atoi(parts[1])
		if err != nil || pid <= 0 {
			return nil, errors.New("invalid native terminal process")
		}
		lsof, err := exec.LookPath("lsof")
		if err != nil {
			return nil, fmt.Errorf("cannot verify the existing chat owner without lsof; reconnect explicitly with codexm terminal %s", l.Session)
		}
		lockDir, err := filepath.EvalSymlinks(filepath.Join(config.Expand(c.Runtime.SharedCodexHome), "thread-writer-locks"))
		if err != nil {
			return nil, fmt.Errorf("cannot verify the existing writer directory; reconnect explicitly with codexm terminal %s", l.Session)
		}
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		cmd := exec.CommandContext(probeCtx, lsof, "-nP", "-a", "-p", strconv.Itoa(pid), "-F", "pn")
		data, err := cmd.Output()
		cancel()
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				return nil, fmt.Errorf("cannot verify existing chat ownership; inspect with codexm terminal %s", l.Session)
			}
			continue // No open lock: this CLI may have switched to another chat.
		}
		owner := false
		loaded := map[string]bool{}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "p") {
				owner = line == "p"+strconv.Itoa(pid)
			}
			if owner && strings.HasPrefix(line, "n") {
				file := strings.TrimPrefix(line, "n")
				if filepath.Dir(file) != lockDir {
					continue
				}
				name := filepath.Base(file)
				if strings.HasSuffix(name, ".lock") && threadID.MatchString(strings.TrimSuffix(name, ".lock")) {
					loaded[strings.TrimSuffix(name, ".lock")] = true
				}
			}
		}
		if !loaded[id] {
			continue
		}
		if len(loaded) != 1 {
			return nil, fmt.Errorf("existing CLI has multiple loaded chats; its current view cannot be verified. Connect explicitly with codexm terminal %s", l.Session)
		}
		if l.Account != account {
			return nil, fmt.Errorf("this chat is already running under account %s in %s; selected account %s was not applied. Reconnect with codexm terminal %s, or exit that CLI before changing accounts", l.Account, l.Session, account, l.Session)
		}
		return &l, nil
	}
	return nil, nil
}

// Attach never detaches another client or stops the existing CLI. Inside tmux,
// switch the current client instead of nesting another tmux terminal.
func Attach(ctx context.Context, c config.Config, session string) error {
	t, err := Screen(ctx, c, session)
	if err != nil {
		return err
	}
	if _, err = tmux(ctx, t.Launch.TmuxServer, "has-session", "-t", "="+session); err != nil {
		return err
	}
	bin, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	// switch-client only works on the current server. Cross-server attach needs
	// TMUX cleared; other clients (and their sessions) remain attached.
	socket, err := tmux(ctx, t.Launch.TmuxServer, "display-message", "-p", "-t", t.Launch.Pane, "#{socket_path}")
	if err != nil {
		return err
	}
	command, env := attachMode(os.Getenv("TMUX"), strings.TrimSpace(socket), os.Environ())
	return syscall.Exec(bin, append([]string{bin}, tmuxArgs(t.Launch.TmuxServer, command, "-t", "="+session)...), env)
}
func attachMode(current, socket string, env []string) (string, []string) {
	if current != "" && strings.Split(current, ",")[0] == socket {
		return "switch-client", env
	}
	clean := make([]string, 0, len(env))
	for _, value := range env {
		if !strings.HasPrefix(value, "TMUX=") {
			clean = append(clean, value)
		}
	}
	return "attach-session", clean
}
func AttachCommand(l Launch) string {
	server := l.TmuxServer
	if server == "" {
		server = "default"
	}
	return "tmux -L " + server + " attach -t " + l.Session
}

// Open creates a named terminal only. There is no supervisor, RPC worker or model
// request. Reopening the same live thread/account attaches to its existing CLI.
func Open(ctx context.Context, c config.Config, self, id, account string) (Launch, error) {
	return OpenContinuation(ctx, c, self, id, account, false, "")
}
func OpenContinuation(ctx context.Context, c config.Config, self, id, account string, continuation bool, switchID string) (Launch, error) {
	l, e := Plan(ctx, c, id, account)
	if e != nil {
		return l, e
	}
	l.Continue, l.SwitchID = continuation, switchID
	if _, e = exec.LookPath("tmux"); e != nil {
		return l, errors.New("tmux is required for web/native terminal sessions")
	}
	l.Session = terminalName(c, id, account)
	l.TmuxServer = CodexTmuxServer
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
			if continuation || switchID != "" {
				return l, errors.New("target terminal already exists; no continuation was replayed")
			}
			return old, nil
		}
		_, _ = tmux(ctx, old.TmuxServer, "kill-session", "-t", "="+l.Session)
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
	pane, e := tmux(ctx, l.TmuxServer, args...)
	if e != nil {
		return l, e
	}
	l.Pane = strings.TrimSpace(pane)
	complete := false
	defer func() {
		if !complete {
			_, _ = tmux(context.Background(), l.TmuxServer, "kill-session", "-t", "="+l.Session)
		}
	}()
	if _, e = tmux(ctx, l.TmuxServer, "set-option", "-w", "-t", l.Pane, "remain-on-exit", "on"); e != nil {
		return l, e
	}
	// set-option's session target does not accept the '=' exact-match prefix on
	// supported tmux versions; this exact, freshly-created name is already owned.
	if _, e = tmux(ctx, l.TmuxServer, "set-option", "-t", l.Session, "history-limit", "10000"); e != nil {
		return l, e
	}
	if e = saveSession(c, l); e != nil {
		return l, e
	}
	if _, e = tmux(ctx, l.TmuxServer, "respawn-pane", "-k", "-t", l.Pane, "-c", l.Cwd, self, "--config", c.Path, "__native-run", sessionPath(c, l.Session)); e != nil {
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
		if l.SwitchID != "" && cr.WriterBusy(config.Expand(c.Runtime.SharedCodexHome), l.Thread) {
			e = errors.New("another writer acquired the chat during handoff; nothing was interrupted")
		}
	}
	if e == nil && l.SwitchID != "" {
		var s SwitchRequest
		b, readErr := os.ReadFile(filepath.Join(switchDir(c, l.SwitchID), "request.json"))
		key, keyErr := authKey(c, l.Account)
		if readErr != nil || json.Unmarshal(b, &s) != nil || keyErr != nil || s.ID != l.SwitchID || s.Target.Thread != l.Thread || s.Target.Account != l.Account || s.TargetAuth != key {
			e = errors.New("handoff target identity changed; replacement was not started")
		}
	}
	if e == nil {
		e = prepare(ctx, c, l, l.SwitchID == "")
	}
	if e != nil {
		l.Error = e.Error()
		_ = saveSession(c, l)
		return e
	}
	l.Ready = true
	if e = trackLaunch(ctx, c, &l); e != nil {
		return e
	}
	if e = saveSession(c, l); e != nil {
		return e
	}
	if e = writeReceipt(c, l); e != nil {
		return e
	}
	if e = os.Chdir(l.Cwd); e != nil {
		return e
	}
	e = syscall.Exec(l.Binary, ResumeArgs(l), launchEnvironment(c, l))
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
	t := Terminal{Launch: l, Attach: AttachCommand(l)}
	// tmux display-message can fall back to its current pane for a stale target.
	// A closed session is simply exited, even while its last receipt remains.
	if _, e = tmux(ctx, l.TmuxServer, "has-session", "-t", "="+s); e != nil {
		return t, nil
	}
	value, e := tmux(ctx, l.TmuxServer, "display-message", "-p", "-t", l.Pane, "#{session_name}|#{pane_dead}|#{pane_dead_status}")
	if e != nil {
		if _, exists := tmux(ctx, l.TmuxServer, "has-session", "-t", "="+s); exists == nil {
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
	t.Text, e = tmux(ctx, l.TmuxServer, "capture-pane", "-p", "-J", "-t", l.Pane, "-S", "-1500")
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
		cmd := exec.CommandContext(ctx, "tmux", tmuxArgs(t.Launch.TmuxServer, "load-buffer", "-b", buffer, "-")...)
		cmd.Stdin = strings.NewReader(text)
		if e = cmd.Run(); e != nil {
			return e
		}
		defer tmux(context.Background(), t.Launch.TmuxServer, "delete-buffer", "-b", buffer)
		if _, e = tmux(ctx, t.Launch.TmuxServer, "paste-buffer", "-p", "-b", buffer, "-t", t.Launch.Pane); e != nil {
			return e
		}
		_, e = tmux(ctx, t.Launch.TmuxServer, "send-keys", "-t", t.Launch.Pane, "Enter")
		return e
	}
	allowed := map[string]bool{"Enter": true, "Escape": true, "Up": true, "Down": true, "Left": true, "Right": true, "Tab": true, "BTab": true, "C-c": true, "C-d": true, "PageUp": true, "PageDown": true}
	if !allowed[key] {
		return errors.New("unsupported terminal key")
	}
	_, e = tmux(ctx, t.Launch.TmuxServer, "send-keys", "-t", t.Launch.Pane, key)
	return e
}
func Close(ctx context.Context, c config.Config, s string) error {
	t, e := Screen(ctx, c, s)
	if e != nil {
		return e
	}
	if _, e = tmux(ctx, t.Launch.TmuxServer, "has-session", "-t", "="+t.Launch.Session); e != nil {
		return nil
	}
	_, e = tmux(ctx, t.Launch.TmuxServer, "kill-session", "-t", "="+t.Launch.Session)
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
