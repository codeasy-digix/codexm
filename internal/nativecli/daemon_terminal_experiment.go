//go:build gui_experiment

package nativecli

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/codeasy-digix/codexm/internal/config"
)

type daemonTerminal struct {
	Server DaemonInfo `json:"server"`
	Thread string     `json:"thread_id,omitempty"`
	Cwd    string     `json:"cwd"`
}

func DaemonRequestID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6], b[8] = b[6]&15|64, b[8]&63|128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func daemonResumeArgs(binary, socket, thread string) []string {
	args := []string{binary, "--remote", "unix://" + socket, "--no-alt-screen"}
	if thread != "" {
		args = append(args, "resume", thread)
	}
	return args
}

func daemonTerminalPath(c config.Config, id string) string {
	return filepath.Join(daemonRoot(c), "terminals", id+".json")
}

// A tmux pane holds only the official frontend. The official daemon owns the
// turn, independently of this pane and of the codexm controller's lifetime.
func OpenDaemonTerminal(ctx context.Context, c config.Config, self, account, id, cwd string) (Launch, error) {
	l := Launch{Account: account, Thread: id, TmuxServer: CodexTmuxServer, TmuxSession: WorkspaceSession}
	if id != "" && !threadID.MatchString(id) {
		return l, errors.New("complete thread UUID required")
	}
	d, err := openDaemon(ctx, c, account, true)
	if err != nil {
		return l, err
	}
	defer d.client.Close()
	if err = d.verify(ctx, c, id, false); err != nil {
		return l, err
	}
	if id != "" {
		thread, err := d.client.Read(ctx, id, false)
		if err != nil {
			return l, err
		}
		cwd = thread.Cwd
		archived, err := archivedState(ctx, c, id)
		if err != nil {
			return l, err
		}
		if archived {
			return l, errors.New("chat is archived; restore it explicitly in its owner before attaching")
		}
	}
	if !filepath.IsAbs(cwd) {
		return l, errors.New("absolute project directory required")
	}
	st, err := os.Stat(cwd)
	if err != nil || !st.IsDir() {
		return l, errors.New("project directory is unavailable")
	}
	l.Cwd = cwd
	request, err := DaemonRequestID()
	if err != nil {
		return l, err
	}
	b, _ := json.Marshal(daemonTerminal{Server: d.info, Thread: id, Cwd: cwd})
	if err := config.AtomicWrite(daemonTerminalPath(c, request), b, 0600); err != nil {
		return l, err
	}
	if err := ensureWorkspace(ctx); err != nil {
		return l, err
	}
	args := []string{"new-window", "-d", "-P", "-F", "#{pane_id}", "-t", WorkspaceSession + ":", "-n", "daemon-" + account + "-" + request[:8], "-c", cwd}
	for _, key := range []string{"PATH", "HOME", "LANG", "LC_ALL", "LC_CTYPE", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
		if value, ok := os.LookupEnv(key); ok {
			args = append(args, "-e", key+"="+value)
		}
	}
	args = append(args, self, "--config", c.Path, "daemon", "__run", request)
	pane, err := tmux(ctx, CodexTmuxServer, args...)
	if err != nil {
		return l, err
	}
	l.Pane = strings.TrimSpace(pane)
	window, err := tmux(ctx, CodexTmuxServer, "display-message", "-p", "-t", l.Pane, "#{window_id}")
	l.Window = strings.TrimSpace(window)
	return l, err // Never remove a started pane on an observation failure.
}

func RunDaemonTerminal(ctx context.Context, c config.Config, request string) error {
	if !threadID.MatchString(request) {
		return errors.New("invalid daemon terminal request")
	}
	b, err := os.ReadFile(daemonTerminalPath(c, request))
	if err != nil {
		return err
	}
	var launch daemonTerminal
	if err := json.Unmarshal(b, &launch); err != nil {
		return err
	}
	d, err := openDaemon(ctx, c, launch.Server.Account, true)
	if err != nil {
		return err
	}
	defer d.client.Close()
	if d.info.Identity != launch.Server.Identity || d.info.Socket != launch.Server.Socket ||
		d.info.Process.PID != launch.Server.Process.PID || d.info.Process.Started != launch.Server.Process.Started {
		return errors.New("prepared daemon/login changed; terminal was not started")
	}
	if launch.Thread != "" && !threadID.MatchString(launch.Thread) {
		return errors.New("invalid prepared chat UUID")
	}
	if err := d.verify(ctx, c, launch.Thread, false); err != nil {
		return err
	}
	if !filepath.IsAbs(launch.Cwd) {
		return errors.New("invalid prepared project directory")
	}
	if err := os.Chdir(launch.Cwd); err != nil {
		return err
	}
	// Close the short-lived controller before exec. No extra connection keeps
	// this chat alive; persistence must come from the official daemon itself.
	d.client.Close()
	binary := d.info.Process.Executable
	return syscall.Exec(binary, daemonResumeArgs(binary, d.info.Socket, launch.Thread), environment(c, d.info.Account))
}
