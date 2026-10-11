// Package daemon supervises the official shared daemon without account aliases
// or model calls. It never kills a process simply because a socket is slow.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

type Manager struct {
	Config config.Config
	Self   string
	Out    io.Writer
}
type Settings struct {
	Enabled bool `json:"enabled"`
}

func (m Manager) root() string { return filepath.Join(m.Config.StateDir(), "daemon-watchdog") }
func (m Manager) session() string {
	if m.Config.Supervisor.TmuxSession == "codexm-supervisor" {
		return "codexm-daemon-watchdog"
	}
	return m.Config.Supervisor.TmuxSession + "-daemon-watchdog"
}
func (m Manager) settings() (Settings, error) {
	var s Settings
	b, e := os.ReadFile(filepath.Join(m.root(), "settings.json"))
	if os.IsNotExist(e) {
		return s, nil
	}
	if e != nil {
		return s, e
	}
	if json.Unmarshal(b, &s) != nil {
		return s, errors.New("invalid daemon watchdog settings")
	}
	return s, nil
}
func (m Manager) lock(ctx context.Context, fn func() error) error {
	if e := os.MkdirAll(m.root(), 0700); e != nil {
		return e
	}
	fd, e := syscall.Open(filepath.Join(m.root(), "operation.lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	defer syscall.Close(fd)
	for {
		if e := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); e == nil {
			break
		} else if !errors.Is(e, syscall.EWOULDBLOCK) {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	return fn()
}
func (m Manager) enable(ctx context.Context, enabled bool) error {
	return m.lock(ctx, func() error {
		return config.AtomicWrite(filepath.Join(m.root(), "settings.json"), []byte(fmt.Sprintf(`{"enabled":%t}`, enabled)), 0600)
	})
}

func Process(shared string) (int, bool, error) {
	var problem error
	for _, name := range []string{"daemon.pid", "app-server.pid"} {
		p := filepath.Join(shared, "app-server-daemon", name)
		info, e := os.Lstat(p)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil || !info.Mode().IsRegular() || info.Size() > 16384 {
			problem = errors.New("cannot safely inspect the official daemon process record")
			continue
		}
		b, e := os.ReadFile(p)
		var record struct {
			PID int `json:"pid"`
		}
		if e != nil || json.Unmarshal(b, &record) != nil || record.PID <= 0 {
			problem = errors.New("invalid official daemon process record")
			continue
		}
		if e := syscall.Kill(record.PID, 0); e == nil || errors.Is(e, syscall.EPERM) {
			return record.PID, true, nil
		} else if !errors.Is(e, syscall.ESRCH) {
			problem = e
		}
	}
	return 0, false, problem
}
func (m Manager) native(ctx context.Context, action string) error {
	r, e := cr.Locate(m.Config)
	if e != nil {
		return e
	}
	cmd := exec.CommandContext(ctx, r.Binary, "app-server", "daemon", action)
	cmd.Env = cr.Env(m.Config, "", "")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if e := cmd.Run(); e != nil {
		return fmt.Errorf("official daemon %s failed; inspect the official daemon log", action)
	}
	return nil
}
func (m Manager) ownsSession() bool {
	id := m.sessionID()
	if id == "" {
		return false
	}
	b, e := exec.Command("tmux", "show-options", "-v", "-t", id, "@codexm-daemon-watchdog").Output()
	return e == nil && strings.TrimSpace(string(b)) == m.root()
}
func (m Manager) sessionID() string {
	b, e := exec.Command("tmux", "list-sessions", "-F", "#{session_name}\t#{session_id}").Output()
	if e != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		name, id, ok := strings.Cut(line, "\t")
		if ok && name == m.session() {
			return id
		}
	}
	return ""
}
func (m Manager) hasSession() bool {
	return exec.Command("tmux", "has-session", "-t", "="+m.session()).Run() == nil
}

func (m Manager) Command(ctx context.Context, args []string) error {
	if len(args) == 0 {
		args = []string{"status"}
	}
	if len(args) > 2 || len(args) == 2 && (args[0] != "status" || args[1] != "--json") {
		return errors.New("usage: codexm daemon watch|unwatch|status|start|restart|stop")
	}
	switch args[0] {
	case "watch":
		if _, e := exec.LookPath("tmux"); e != nil {
			return e
		}
		if m.hasSession() && !m.ownsSession() {
			return errors.New("watchdog tmux session belongs to another process; it was preserved")
		}
		if e := m.enable(ctx, true); e != nil {
			return e
		}
		if !m.hasSession() {
			argv := []string{m.Self, "--config", m.Config.Path, "__daemon-watchdog"}
			for i := range argv {
				argv[i] = cr.ShellQuote(argv[i])
			}
			if e := exec.Command("tmux", "new-session", "-d", "-s", m.session(), "exec "+strings.Join(argv, " ")).Run(); e != nil {
				return e
			}
			if e := exec.Command("tmux", "set-option", "-t", m.sessionID(), "@codexm-daemon-watchdog", m.root()).Run(); e != nil {
				return e
			}
		}
		fmt.Fprintf(m.Out, "Automatic official daemon recovery enabled; no model calls.\nSession: %s\nAttach: tmux attach -t %s\nLog: %s\n", m.session(), m.session(), filepath.Join(m.root(), "watchdog.log"))
		return nil
	case "unwatch":
		if e := m.enable(ctx, false); e != nil {
			return e
		}
		if m.hasSession() {
			if !m.ownsSession() {
				return errors.New("watchdog ownership could not be verified; session was preserved")
			}
			if e := exec.Command("tmux", "kill-session", "-t", "="+m.session()).Run(); e != nil {
				return e
			}
		}
		fmt.Fprintln(m.Out, "Daemon watchdog stopped; official daemon was preserved.")
		return nil
	case "start", "restart", "stop":
		if _, e := os.Stat(filepath.Join(m.root(), "settings.json")); e == nil {
			if e = m.enable(ctx, args[0] != "stop"); e != nil {
				return e
			}
		}
		if e := m.native(ctx, args[0]); e != nil {
			return e
		}
		fmt.Fprintf(m.Out, "Official daemon: %s completed.\n", args[0])
		return nil
	case "status":
		s, e := m.settings()
		if e != nil {
			return e
		}
		pid, running, e := Process(config.Expand(m.Config.Runtime.SharedCodexHome))
		state := map[string]any{"enabled": s.Enabled, "watchdog_running": m.hasSession() && m.ownsSession(), "daemon_running": running, "daemon_pid": pid, "session": m.session(), "model_calls": 0}
		if e != nil {
			state["error"] = e.Error()
		}
		if len(args) == 2 {
			return json.NewEncoder(m.Out).Encode(state)
		}
		fmt.Fprintf(m.Out, "Official daemon: running=%t PID=%d · automatic recovery=%t · watchdog=%v\nAttach: tmux attach -t %s\n", running, pid, s.Enabled, state["watchdog_running"], m.session())
		return e
	default:
		return errors.New("usage: codexm daemon watch|unwatch|status|start|restart|stop")
	}
}

func (m Manager) Run(ctx context.Context) error {
	if e := os.MkdirAll(m.root(), 0700); e != nil {
		return e
	}
	fd, e := syscall.Open(filepath.Join(m.root(), "worker.lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	defer syscall.Close(fd)
	if e = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return errors.New("daemon watchdog already running")
	}
	lf, e := syscall.Open(filepath.Join(m.root(), "watchdog.log"), syscall.O_CREAT|syscall.O_WRONLY|syscall.O_APPEND|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return e
	}
	logFile := os.NewFile(uintptr(lf), "daemon watchdog log")
	defer logFile.Close()
	log := io.MultiWriter(m.Out, logFile)
	last := ""
	next := time.Time{}
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		state := "paused"
		e := m.lock(ctx, func() error {
			s, e := m.settings()
			if e != nil {
				return e
			}
			if !s.Enabled {
				return nil
			}
			pid, alive, e := Process(config.Expand(m.Config.Runtime.SharedCodexHome))
			if e != nil {
				return e
			}
			if alive {
				state = fmt.Sprintf("running PID %d", pid)
				next = time.Time{}
				backoff = 5 * time.Second
				return nil
			}
			state = "daemon absent; recovery pending"
			if time.Now().Before(next) {
				return nil
			}
			fmt.Fprintf(log, "%s official daemon absent; starting\n", time.Now().Format(time.RFC3339))
			attempt, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if e = m.native(attempt, "start"); e != nil {
				return e
			}
			pid, alive, e = Process(config.Expand(m.Config.Runtime.SharedCodexHome))
			if e != nil {
				return e
			}
			if !alive {
				return errors.New("official daemon did not publish a running PID")
			}
			state = fmt.Sprintf("recovered PID %d", pid)
			next = time.Time{}
			backoff = 5 * time.Second
			return nil
		})
		if e != nil {
			if ctx.Err() != nil {
				return nil
			}
			state = e.Error()
			next = time.Now().Add(backoff)
			backoff = min(time.Minute, backoff*2)
		}
		if state != last {
			fmt.Fprintf(log, "%s %s\n", time.Now().Format(time.RFC3339), state)
			last = state
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}
	return nil
}
