// Package service manages a user-owned OS service, never the native Codex daemon.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
)

type Manager struct {
	Config config.Config
	Self   string
	Kind   string
}
type Receipt struct {
	Label      string `json:"label"`
	Definition string `json:"definition"`
	Digest     string `json:"digest"`
	Binary     string `json:"binary"`
	Enabled    bool   `json:"enabled"`
}
type Status struct {
	Label      string `json:"label"`
	Platform   string `json:"platform"`
	Installed  bool   `json:"installed"`
	Enabled    bool   `json:"enabled"`
	Loaded     bool   `json:"loaded"`
	Running    bool   `json:"running"`
	PID        int    `json:"pid,omitempty"`
	Definition string `json:"definition"`
	Log        string `json:"log"`
	Problem    string `json:"problem,omitempty"`
}

func (m Manager) kind() string {
	if m.Kind == "watchdog" {
		return "watchdog"
	}
	return "supervisor"
}
func (m Manager) Label() string {
	label := "org.codeasy.codexm." + m.kind()
	// A custom state directory gets its own unit, including isolated tests.
	if m.Config.StateDir() != config.Defaults().StateDir() {
		h := sha256.Sum256([]byte(m.Config.StateDir()))
		label += "-" + hex.EncodeToString(h[:6])
	}
	return label
}
func (m Manager) root() string        { return filepath.Join(m.Config.StateDir(), "services") }
func (m Manager) receiptPath() string { return filepath.Join(m.root(), m.kind()+".json") }
func (m Manager) LogPath() string     { return filepath.Join(m.root(), m.kind()+".log") }
func (m Manager) DefinitionPath() string {
	h, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(h, "Library", "LaunchAgents", m.Label()+".plist")
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(base) {
		base = filepath.Join(h, ".config")
	}
	return filepath.Join(base, "systemd", "user", m.Label()+".service")
}
func (m Manager) read() (Receipt, error) {
	var r Receipt
	b, e := os.ReadFile(m.receiptPath())
	if e != nil {
		return r, e
	}
	if e = json.Unmarshal(b, &r); e != nil {
		return r, e
	}
	if r.Label != m.Label() || r.Definition != m.DefinitionPath() {
		return r, errors.New("service receipt does not match this configuration")
	}
	return r, nil
}
func (m Manager) Registered() bool { _, e := m.read(); return e == nil }
func (m Manager) write(r Receipt) error {
	b, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		return e
	}
	return config.AtomicWrite(m.receiptPath(), b, 0600)
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func xmlText(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func unitText(s string) string { return strconv.Quote(strings.ReplaceAll(s, "%", "%%")) }

// Save only the command search path, never the invoking shell's credentials.
func servicePath() string {
	var paths []string
	seen := map[string]bool{}
	for _, p := range filepath.SplitList(os.Getenv("PATH") + ":/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin") {
		if filepath.IsAbs(p) && !seen[p] && !strings.ContainsAny(p, "\n\r") {
			paths = append(paths, p)
			seen[p] = true
		}
	}
	return strings.Join(paths, ":")
}

// Definition is pure so paths with spaces, XML characters and systemd specifiers
// can be validated without touching the user's service manager.
func (m Manager) Definition(platform string) ([]byte, error) {
	if !filepath.IsAbs(m.Self) || !filepath.IsAbs(m.Config.Path) {
		return nil, errors.New("absolute binary and configuration paths are required")
	}
	mode := "__service"
	if m.kind() == "watchdog" {
		mode = "__daemon-watchdog"
	}
	args := []string{m.Self, "--config", m.Config.Path, mode}
	if platform == "darwin" {
		var a strings.Builder
		for _, v := range args {
			fmt.Fprintf(&a, "<string>%s</string>", xmlText(v))
		}
		return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string><key>ProgramArguments</key><array>%s</array>
<key>RunAtLoad</key><true/><key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
<key>EnvironmentVariables</key><dict><key>PATH</key><string>%s</string></dict>
<key>ThrottleInterval</key><integer>5</integer><key>ExitTimeOut</key><integer>30</integer>
<key>AbandonProcessGroup</key><true/><key>ProcessType</key><string>Background</string>
<key>Umask</key><integer>63</integer>
<key>StandardOutPath</key><string>%s</string><key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, xmlText(m.Label()), a.String(), xmlText(servicePath()), xmlText(m.LogPath()), xmlText(m.LogPath()))), nil
	}
	if platform == "linux" {
		quoted := make([]string, len(args))
		for i, v := range args {
			quoted[i] = unitText(v)
		}
		return []byte(fmt.Sprintf("[Unit]\nDescription=codexm %s\nStartLimitIntervalSec=60\nStartLimitBurst=10\n\n[Service]\nType=simple\nExecStart=:%s\nEnvironment=%s\nRestart=on-failure\nRestartSec=5\nKillMode=process\nTimeoutStopSec=30\nUMask=0077\nStandardOutput=append:%s\nStandardError=append:%s\n\n[Install]\nWantedBy=default.target\n", m.kind(), strings.Join(quoted, " "), unitText("PATH="+servicePath()), unitText(m.LogPath()), unitText(m.LogPath()))), nil
	}
	return nil, errors.New("user services are supported on macOS and Linux")
}
func (m Manager) run(ctx context.Context, args ...string) ([]byte, error) {
	binary := "launchctl"
	if runtime.GOOS == "linux" {
		binary = "systemctl"
		args = append([]string{"--user"}, args...)
	}
	b, e := exec.CommandContext(ctx, binary, args...).CombinedOutput()
	if e != nil {
		return b, fmt.Errorf("%s %s failed: %s", binary, args[0], strings.TrimSpace(string(b)))
	}
	return b, nil
}
func (m Manager) target() string { return fmt.Sprintf("gui/%d/%s", os.Getuid(), m.Label()) }
func (m Manager) checkOwned(r Receipt) error {
	b, e := os.ReadFile(m.DefinitionPath())
	if e != nil {
		return e
	}
	if digest(b) != r.Digest {
		return errors.New("service definition was edited outside codexm; preserve it before reinstalling")
	}
	return nil
}
func (m Manager) Install(ctx context.Context, start bool) error {
	if e := m.Config.Validate(); e != nil {
		return e
	}
	if m.Config.Path == "" {
		return errors.New("configuration must be saved first")
	}
	if _, e := os.Stat(m.Config.Path); e != nil {
		return e
	}
	b, e := m.Definition(runtime.GOOS)
	if e != nil {
		return e
	}
	enabled := false
	if _, e = os.Lstat(m.DefinitionPath()); e == nil {
		r, re := m.read()
		if re != nil {
			return errors.New("an unowned service definition already exists; it was preserved")
		}
		if e = m.checkOwned(r); e != nil {
			return e
		}
		s := m.Status(ctx)
		if s.Loaded && r.Digest != digest(b) {
			return errors.New("stop the existing codexm service before replacing its definition")
		}
		enabled = r.Enabled
	} else if !os.IsNotExist(e) {
		return e
	}
	if e = os.MkdirAll(m.root(), 0700); e != nil {
		return e
	}
	lf, e := os.OpenFile(m.LogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	lf.Close()
	if e = config.AtomicWrite(m.DefinitionPath(), b, 0600); e != nil {
		return e
	}
	r := Receipt{Label: m.Label(), Definition: m.DefinitionPath(), Digest: digest(b), Binary: m.Self, Enabled: enabled}
	if e = m.write(r); e != nil {
		return e
	}
	if runtime.GOOS == "linux" {
		if _, e = m.run(ctx, "daemon-reload"); e != nil {
			return e
		}
	}
	if start {
		return m.Start(ctx)
	}
	return nil
}
func (m Manager) Start(ctx context.Context) error {
	r, e := m.read()
	if e != nil {
		return fmt.Errorf("install the codexm service first: %w", e)
	}
	if e = m.checkOwned(r); e != nil {
		return e
	}
	r.Enabled = true
	if e = m.write(r); e != nil {
		return e
	}
	if runtime.GOOS == "darwin" {
		if _, e = m.run(ctx, "enable", m.target()); e != nil {
			return e
		}
		if !m.Status(ctx).Loaded {
			_, e = m.run(ctx, "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), m.DefinitionPath())
			return e
		}
		if !m.Status(ctx).Running {
			_, e = m.run(ctx, "kickstart", m.target())
		}
		return e
	}
	_, e = m.run(ctx, "enable", "--now", m.Label()+".service")
	return e
}
func (m Manager) Ensure(ctx context.Context) error {
	r, e := m.read()
	if os.IsNotExist(e) {
		return m.Install(ctx, true)
	}
	if e != nil {
		return e
	}
	if !r.Enabled {
		return errors.New("codexm service was explicitly stopped; run codexm service start")
	}
	if m.Status(ctx).Running {
		return nil
	}
	return m.Start(ctx)
}

// Upgrade replaces only the observer binary. Workers outlive service Stop.
// A failed definition/start/readiness check restores the owned previous unit.
func (m Manager) Upgrade(ctx context.Context, ready func(context.Context) error) error {
	r, e := m.read()
	if e != nil {
		return e
	}
	if e = m.checkOwned(r); e != nil {
		return e
	}
	previous, e := os.ReadFile(m.DefinitionPath())
	if e != nil {
		return e
	}
	if e = m.Stop(ctx); e != nil {
		return e
	}
	e = m.Install(ctx, r.Enabled)
	if e == nil && r.Enabled {
		e = WaitReady(ctx, ready)
	}
	if e == nil {
		return nil
	}
	rollback, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if stop := m.Stop(rollback); stop != nil {
		return fmt.Errorf("upgrade failed: %w; rollback stop failed: %v", e, stop)
	}
	if restore := config.AtomicWrite(m.DefinitionPath(), previous, 0600); restore != nil {
		return fmt.Errorf("upgrade failed: %w; definition restore failed: %v", e, restore)
	}
	if restore := m.write(r); restore != nil {
		return fmt.Errorf("upgrade failed: %w; receipt restore failed: %v", e, restore)
	}
	if runtime.GOOS == "linux" {
		if _, reload := m.run(rollback, "daemon-reload"); reload != nil {
			return fmt.Errorf("upgrade failed: %w; rollback reload failed: %v", e, reload)
		}
	}
	if r.Enabled {
		if restart := m.Start(rollback); restart != nil {
			return fmt.Errorf("upgrade failed: %w; rollback restart failed: %v", e, restart)
		}
	}
	return fmt.Errorf("upgrade failed; previous service definition restored: %w", e)
}

// Disarm records explicit stop intent before a graceful shutdown. It leaves the
// current process running long enough to interrupt and settle owned work.
func (m Manager) Disarm(ctx context.Context) error {
	r, e := m.read()
	if e != nil {
		return e
	}
	if e = m.checkOwned(r); e != nil {
		return e
	}
	r.Enabled = false
	if e = m.write(r); e != nil {
		return e
	}
	if runtime.GOOS == "darwin" {
		_, e = m.run(ctx, "disable", m.target())
	} else {
		_, e = m.run(ctx, "disable", m.Label()+".service")
	}
	return e
}
func (m Manager) Stop(ctx context.Context) error {
	if e := m.Disarm(ctx); e != nil {
		return e
	}
	var e error
	if runtime.GOOS == "darwin" {
		if m.Status(ctx).Loaded {
			_, e = m.run(ctx, "bootout", m.target())
		}
		return e
	}
	_, e = m.run(ctx, "disable", "--now", m.Label()+".service")
	return e
}
func (m Manager) Uninstall(ctx context.Context) error {
	if e := m.Stop(ctx); e != nil {
		return e
	}
	if e := os.Remove(m.DefinitionPath()); e != nil {
		return e
	}
	if e := os.Remove(m.receiptPath()); e != nil {
		return e
	}
	if runtime.GOOS == "linux" {
		_, e := m.run(ctx, "daemon-reload")
		return e
	}
	return nil
}
func (m Manager) Status(ctx context.Context) Status {
	s := Status{Label: m.Label(), Platform: runtime.GOOS, Definition: m.DefinitionPath(), Log: m.LogPath()}
	r, e := m.read()
	if os.IsNotExist(e) {
		return s
	}
	if e != nil {
		s.Problem = e.Error()
		return s
	}
	s.Enabled = r.Enabled
	s.Installed = true
	if e = m.checkOwned(r); e != nil {
		s.Problem = e.Error()
		return s
	}
	if runtime.GOOS == "darwin" {
		b, e := m.run(ctx, "print", m.target())
		if e != nil {
			return s
		}
		s.Loaded = true
		for _, line := range strings.Split(string(b), "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(line), " = ")
			if ok && k == "pid" {
				s.PID, _ = strconv.Atoi(v)
			}
		}
		s.Running = s.PID > 0
	} else {
		b, e := m.run(ctx, "show", m.Label()+".service", "--property=LoadState,ActiveState,MainPID")
		if e != nil {
			return s
		}
		for _, line := range strings.Split(string(b), "\n") {
			k, v, _ := strings.Cut(line, "=")
			switch k {
			case "LoadState":
				s.Loaded = v == "loaded"
			case "ActiveState":
				s.Running = v == "active"
			case "MainPID":
				s.PID, _ = strconv.Atoi(v)
			}
		}
	}
	return s
}
func (m Manager) Logs(w io.Writer) error {
	f, e := os.Open(m.LogPath())
	if e != nil {
		return e
	}
	defer f.Close()
	if st, e := f.Stat(); e == nil && st.Size() > 64*1024 {
		_, _ = f.Seek(-64*1024, io.SeekEnd)
	}
	_, e = io.Copy(w, f)
	return e
}
func (m Manager) Command(ctx context.Context, args []string, w io.Writer) error {
	if len(args) == 0 {
		args = []string{"status"}
	}
	var e error
	switch args[0] {
	case "install":
		if len(args) > 2 || len(args) == 2 && args[1] != "--no-start" {
			return errors.New("usage: codexm service install [--no-start]")
		}
		e = m.Install(ctx, len(args) == 1)
	case "start":
		e = m.Start(ctx)
	case "stop":
		e = m.Stop(ctx)
	case "restart":
		if e = m.Stop(ctx); e == nil {
			e = m.Start(ctx)
		}
	case "uninstall":
		e = m.Uninstall(ctx)
	case "logs":
		return m.Logs(w)
	case "status":
		s := m.Status(ctx)
		if len(args) > 1 && args[1] == "--json" {
			return json.NewEncoder(w).Encode(s)
		}
		fmt.Fprintf(w, "codexm service · %s · installed=%t enabled=%t running=%t PID=%d\nLog: %s\n", s.Label, s.Installed, s.Enabled, s.Running, s.PID, s.Log)
		if s.Problem != "" {
			return errors.New(s.Problem)
		}
		return nil
	default:
		return errors.New("usage: codexm service install|start|stop|restart|status|logs|uninstall")
	}
	if e == nil {
		fmt.Fprintf(w, "codexm service: %s completed\n", args[0])
	}
	return e
}

// WaitReady waits only on a caller-supplied readiness probe, never starts work.
func WaitReady(ctx context.Context, probe func(context.Context) error) error {
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	for {
		if probe(ctx) == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("codexm service is not ready; inspect codexm service status and logs")
		case <-time.After(200 * time.Millisecond):
		}
	}
}
