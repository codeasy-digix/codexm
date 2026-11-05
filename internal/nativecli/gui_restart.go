package nativecli

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Only this certain, unmutated failure can offer GUI/exec takeover. Uncertain archive
// RPCs and unarchive errors must never become a process-termination fallback.
type ForeignWriterError struct{ Thread string }

func (e *ForeignWriterError) Error() string {
	return fmt.Sprintf("chat %s still has an active writer; resume was not started. Retry in a foreground terminal for a verified GUI restart or exec takeover, or release the chat in its owner", e.Thread)
}

func confirmGUIRestart(in io.Reader, out io.Writer, interactive bool, id string, owner guiOwner) bool {
	if !interactive {
		return false
	}
	fmt.Fprintf(out, "\nChat %s is owned by %s (PID %d).\nGUI 전체를 종료한 뒤 선택한 채팅을 CLI로 이어받고 GUI를 다시 엽니다.\n다른 GUI 대화의 진행 중인 작업도 중단되며, 자동 재개되지 않습니다.\n이번 실행에서 GUI를 재시작할까요? [y/N]: ", id, filepath.Base(owner.App), owner.Process.PID)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "예":
		return true
	}
	return false
}

type guiRestartSpec struct {
	Owner   guiOwner        `json:"owner"`
	Caller  processIdentity `json:"caller"`
	Thread  string          `json:"thread"`
	Account string          `json:"account,omitempty"`
	Shared  string          `json:"shared"`
	Session string          `json:"session"`
	Created int64           `json:"created"`
}

var reopenSessionName = regexp.MustCompile(`^codexm-gui-reopen-[a-f0-9]{16}$`)

func writeJSON(path string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return config.AtomicWrite(path, b, 0600)
}
func markerExists(file string) bool { _, err := os.Stat(file); return err == nil }

// prepareInteractive is used only by foreground resume. Detached/web launches
// cannot consent for a user and keep the noninteractive Prepare path.
func prepareInteractive(ctx context.Context, c config.Config, l Launch) (func(), error) {
	noop := func() {}
	err := Prepare(ctx, c, l)
	var foreign *ForeignWriterError
	if !errors.As(err, &foreign) {
		return noop, err
	}
	shared := config.Expand(c.Runtime.SharedCodexHome)
	if !cr.WriterBusy(shared, l.Thread) {
		return noop, prepare(ctx, c, l, false)
	}
	// An exec worker may itself descend from a GUI tool. Prefer its bounded
	// single-process handoff over shutting down the whole verified GUI.
	external, execErr := findExecOwner(ctx, c, l)
	if execErr == nil {
		return noop, prepareExecHandoff(ctx, c, l, external)
	}
	owner, inspectErr := findGUIOwner(ctx, c, l.Thread)
	if inspectErr != nil {
		if !cr.WriterBusy(shared, l.Thread) {
			return noop, prepare(ctx, c, l, false)
		}
		return noop, fmt.Errorf("%w (GUI: %v; exec: %v)", err, inspectErr, execErr)
	}
	if !confirmGUIRestart(os.Stdin, os.Stderr, term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd())), l.Thread, owner) {
		return noop, fmt.Errorf("GUI restart was not confirmed; GUI and its work were left running; %w", err)
	}
	if !cr.WriterBusy(shared, l.Thread) {
		return noop, Prepare(ctx, c, l)
	}
	current, checkErr := findGUIOwner(ctx, c, l.Thread)
	if checkErr != nil || current != owner {
		return noop, errors.New("GUI ownership changed during confirmation; nothing was stopped; retry")
	}
	recovery, err := armGUIReopen(ctx, c, l, owner)
	if err != nil {
		return noop, err
	}
	// No signals until the bounded recovery helper has acknowledged readiness.
	current, checkErr = findGUIOwner(ctx, c, l.Thread)
	if checkErr != nil || current != owner {
		recovery.fail()
		return noop, errors.New("GUI ownership changed before shutdown; nothing was stopped")
	}
	return finishGUIHandoff(ctx, c, l, owner, recovery)
}

type guiRecovery struct{ dir string }

func (g *guiRecovery) fail() {
	_ = config.AtomicWrite(filepath.Join(g.dir, "failed"), []byte("launcher did not exec\n"), 0600)
}
func armGUIReopen(ctx context.Context, c config.Config, l Launch, owner guiOwner) (*guiRecovery, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if _, err = exec.LookPath("tmux"); err != nil {
		return nil, errors.New("tmux is required to safely restore the GUI after handoff")
	}
	caller, err := inspectProcess(ctx, os.Getpid())
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 8)
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	session := "codexm-gui-reopen-" + hex.EncodeToString(nonce)
	dir := filepath.Join(root(c), "gui-restarts", session)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	spec := guiRestartSpec{Owner: owner, Caller: caller, Thread: l.Thread, Account: l.Account, Shared: config.Expand(c.Runtime.SharedCodexHome), Session: session, Created: time.Now().Unix()}
	if err = writeJSON(filepath.Join(dir, "request.json"), spec); err != nil {
		return nil, err
	}
	if err := ensureWorkspace(ctx); err != nil {
		return nil, err
	}
	args := []string{"new-window", "-d", "-t", WorkspaceSession + ":", "-n", session, "-e", "PATH=" + os.Getenv("PATH"), "-e", "HOME=" + os.Getenv("HOME")}
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
		if value, ok := os.LookupEnv(key); ok {
			args = append(args, "-e", key+"="+value)
		}
	}
	args = append(args, self, "--config", c.Path, "__gui-reopen", filepath.Join(dir, "request.json"))
	if _, err = tmux(ctx, CodexTmuxServer, args...); err != nil {
		return nil, err
	}
	// Force cleanup on exit even if personal tmux configuration keeps dead panes.
	if _, err = tmux(ctx, CodexTmuxServer, "set-option", "-w", "-t", WorkspaceSession+":"+session, "remain-on-exit", "off"); err != nil {
		return nil, err
	}
	recovery := &guiRecovery{dir: dir}
	wait, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	for {
		if b, e := os.ReadFile(filepath.Join(dir, "ready.json")); e == nil {
			var helper processIdentity
			if json.Unmarshal(b, &helper) == nil && sameProcess(ctx, helper) {
				fmt.Fprintf(os.Stderr, "GUI recovery: %s\n%s\n", session, "tmux attach -t codexm")
				return recovery, nil
			}
		}
		if markerExists(filepath.Join(dir, "result.json")) {
			recovery.fail()
			return nil, errors.New("GUI recovery helper could not start; GUI was left running")
		}
		select {
		case <-wait.Done():
			recovery.fail()
			return nil, errors.New("GUI recovery helper did not become ready; GUI was left running")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

type guiHandoffActions struct {
	verify  func() bool
	stop    func() error
	wait    func() error
	prepare func() error
}

func executeGUIHandoff(recovery *guiRecovery, actions guiHandoffActions) (err error) {
	defer func() {
		if err != nil {
			recovery.fail()
		}
	}()
	if !actions.verify() {
		return errors.New("GUI ownership changed; nothing was stopped; retry")
	}
	if err = config.AtomicWrite(filepath.Join(recovery.dir, "stop-requested"), []byte("confirmed for this attempt\n"), 0600); err != nil {
		return err
	}
	if err = actions.stop(); err != nil {
		return fmt.Errorf("GUI shutdown failed: %w", err)
	}
	if err = actions.wait(); err != nil {
		return errors.New("GUI did not release the chat; no force-kill was attempted and resume was not started")
	}
	return actions.prepare()
}
func finishGUIHandoff(ctx context.Context, c config.Config, l Launch, owner guiOwner, recovery *guiRecovery) (func(), error) {
	err := executeGUIHandoff(recovery, guiHandoffActions{
		verify: func() bool { current, e := findGUIOwner(ctx, c, l.Thread); return e == nil && current == owner },
		stop:   func() error { return syscall.Kill(owner.Process.PID, syscall.SIGTERM) },
		wait: func() error {
			wait, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			return cr.WaitWriterRelease(wait, config.Expand(c.Runtime.SharedCodexHome), l.Thread)
		},
		prepare: func() error { return Prepare(ctx, c, l) },
	})
	if err != nil {
		return func() {}, err
	}
	return recovery.fail, nil
}

// A successful handoff reopens only after the selected CLI acquired the writer.
// A failed/disappearing caller still restores the GUI, without resuming anything.
func guiCLIReady(ctx context.Context, c config.Config, spec guiRestartSpec) bool {
	pids, err := writerProcesses(ctx, spec.Shared, spec.Thread)
	if err != nil || len(pids) != 1 || !cr.WriterBusy(spec.Shared, spec.Thread) {
		return false
	}
	if pids[0] == spec.Caller.PID {
		return true
	} // Earlier foreground callers exec'd Codex.
	// The new launcher becomes a tmux client; its CLI has an independent PID.
	// Match the fresh, verified child receipt instead of waiting for the viewer
	// itself to acquire a writer lock, which it will never own.
	launch, err := readLast(c, spec.Thread)
	return err == nil && launch.Account == spec.Account && launch.At >= spec.Created*1000 &&
		launch.TmuxServer == CodexTmuxServer && launch.TmuxSession == WorkspaceSession &&
		launch.Owner.PID == pids[0] && verifyOwnedCLI(ctx, c, launch) == nil
}

func guiRecoveryAction(stopped, callerAlive, guiAlive, failed, timedOut, owns bool) string {
	if !stopped && (failed || !callerAlive || timedOut) {
		return "cancel"
	}
	if stopped && !guiAlive && (owns || failed || !callerAlive || timedOut) {
		return "reopen"
	}
	if stopped && guiAlive && (failed || timedOut) {
		return "still-running"
	}
	return "wait"
}

func RunGUIReopen(ctx context.Context, c config.Config, file string) (resultErr error) {
	dir := filepath.Dir(file)
	session := filepath.Base(dir)
	if !reopenSessionName.MatchString(session) || file != filepath.Join(root(c), "gui-restarts", session, "request.json") {
		return errors.New("GUI recovery request must be in the owned state directory")
	}
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("invalid private GUI recovery request")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Getuid()) {
		return errors.New("GUI recovery request belongs to another user")
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var spec guiRestartSpec
	if err = json.Unmarshal(b, &spec); err != nil {
		return err
	}
	if spec.Session != session || spec.Shared != config.Expand(c.Runtime.SharedCodexHome) || !threadID.MatchString(spec.Thread) || time.Since(time.Unix(spec.Created, 0)) < 0 || time.Since(time.Unix(spec.Created, 0)) > 10*time.Second {
		return errors.New("stale or invalid GUI recovery request")
	}
	defer func() {
		_ = writeJSON(filepath.Join(dir, "result.json"), map[string]any{"finished": time.Now().Unix(), "error": errorText(resultErr)})
	}()
	// One GUI-wide recovery at a time, across all selected chats.
	fd, err := unix.Open(filepath.Join(root(c), "gui-restart.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.New("another GUI handoff is already in progress")
	}
	if !sameProcess(ctx, spec.Caller) || !sameProcess(ctx, spec.Owner.Process) {
		return errors.New("GUI or requesting launcher changed")
	}
	if err = verifyGUIApp(ctx, spec.Owner.App); err != nil {
		return err
	}
	helper, err := inspectProcess(ctx, os.Getpid())
	if err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(dir, "ready.json"), helper); err != nil {
		return err
	}
	fmt.Printf("Waiting to restore %s after CLI handoff. This temporary session exits automatically.\n", spec.Owner.App)
	deadline := time.Now().Add(75 * time.Second)
	for {
		stopped := markerExists(filepath.Join(dir, "stop-requested"))
		callerAlive := sameProcess(context.Background(), spec.Caller)
		guiAlive := sameProcess(context.Background(), spec.Owner.Process)
		failed := markerExists(filepath.Join(dir, "failed"))
		timedOut := time.Now().After(deadline) || ctx.Err() != nil
		owns := false
		if stopped && callerAlive && !guiAlive {
			owns = guiCLIReady(context.Background(), c, spec)
		}
		switch guiRecoveryAction(stopped, callerAlive, guiAlive, failed, timedOut, owns) {
		case "cancel":
			return nil
		case "still-running":
			return errors.New("GUI stayed running; handoff did not complete")
		case "reopen":
			reopen, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err = verifyGUIApp(reopen, spec.Owner.App); err != nil {
				return err
			}
			if out, e := exec.CommandContext(reopen, "/usr/bin/open", "-a", spec.Owner.App).CombinedOutput(); e != nil {
				return fmt.Errorf("restore GUI manually with open -a %q: %w: %s", spec.Owner.App, e, strings.TrimSpace(string(out)))
			}
			fmt.Println("GUI reopened; other interrupted GUI turns require manual continuation.")
			return nil
		}

		time.Sleep(150 * time.Millisecond)
	}
}
func errorText(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}
