package nativecli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"golang.org/x/term"
)

type execOwner struct {
	Process processIdentity
	Key     string
	Args    [32]byte // In-memory comparison only; never log/persist argv or prompts.
}

// This is live OS evidence, not the conversation's creation source or a stale
// launcher receipt. Interactive CLIs and shared app-servers do not qualify.
func findExecOwner(ctx context.Context, c config.Config, l Launch) (execOwner, error) {
	var owner execOwner
	shared := config.Expand(c.Runtime.SharedCodexHome)
	pids, err := writerProcesses(ctx, shared, l.Thread)
	if err != nil || len(pids) != 1 || !cr.WriterBusy(shared, l.Thread) {
		return owner, errors.New("cannot identify one active exec writer")
	}
	p, err := inspectProcess(ctx, pids[0])
	if err != nil {
		return owner, err
	}
	key, err := cr.NativeProcessIdentity(p.PID)
	if err != nil {
		return owner, errors.New("cannot verify exec process incarnation")
	}
	actual := p.Executable
	if runtime.GOOS == "linux" {
		actual, err = os.Readlink(fmt.Sprintf("/proc/%d/exe", p.PID))
		if err != nil {
			return owner, errors.New("cannot verify exec executable")
		}
	}
	actual, err = filepath.EvalSymlinks(actual)
	expected, expectedErr := filepath.EvalSymlinks(l.Binary)
	if err != nil || expectedErr != nil || actual != expected {
		return owner, errors.New("writer executable does not match the configured Codex runtime")
	}
	args, err := processArguments(p.PID)
	if err != nil || len(args) == 0 || filepath.Base(args[0]) != filepath.Base(actual) || !isExecArguments(args) {
		return owner, errors.New("writer is not a verified codex exec; shared servers and other CLI modes require their owning terminal")
	}
	if err = verifyIndependentCaller(ctx, p.PID); err != nil {
		return owner, err
	}
	lock, err := filepath.EvalSymlinks(filepath.Join(shared, "thread-writer-locks", l.Thread+".lock"))
	if err != nil {
		return owner, errors.New("exec writer changed during inspection")
	}
	probe, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	files, err := exec.CommandContext(probe, "lsof", "-nP", "-a", "-p", strconv.Itoa(p.PID), "-F", "pn").Output()
	if err != nil {
		return owner, errors.New("cannot verify exec writer files; lsof is required")
	}
	loaded, matchedPID := map[string]bool{}, false
	for _, line := range strings.Split(string(files), "\n") {
		if strings.HasPrefix(line, "p") {
			matchedPID = line == "p"+strconv.Itoa(p.PID)
		}
		if matchedPID && strings.HasPrefix(line, "n") {
			file := strings.TrimPrefix(line, "n")
			if filepath.Base(filepath.Dir(file)) == "thread-writer-locks" && strings.HasSuffix(file, ".lock") && threadID.MatchString(strings.TrimSuffix(filepath.Base(file), ".lock")) {
				loaded[file] = true
			}
		}
	}
	if len(loaded) != 1 || !loaded[lock] {
		return owner, errors.New("exec owns another or multiple chats; nothing was stopped")
	}
	current, err := cr.NativeProcessIdentity(p.PID)
	if err != nil || current != key {
		return owner, errors.New("exec process changed during inspection")
	}
	return execOwner{Process: p, Key: key, Args: sha256.Sum256([]byte(strings.Join(args, "\x00")))}, nil
}

// Unlike terminal discovery, an unreadable ancestry cannot mean independent.
// Parent processes may belong to root (login/sshd); inspect only their parent PID.
func verifyIndependentCaller(ctx context.Context, target int) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	pid := os.Getpid()
	seen := map[int]bool{}
	for range 128 {
		if pid == target {
			return errors.New("an exec job cannot authorize its own termination; run codexm from a separate foreground terminal")
		}
		if pid == 1 {
			return nil
		}
		if pid <= 0 || seen[pid] {
			break
		}
		seen[pid] = true
		b, err := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(pid), "-o", "ppid=").Output()
		if err != nil {
			break
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil {
			break
		}
	}
	return errors.New("cannot verify that the requesting terminal is independent of the exec job; nothing was stopped")
}

func confirmExecHandoff(in io.Reader, out io.Writer, interactive bool, l Launch, owner execOwner) bool {
	if !interactive {
		return false
	}
	fmt.Fprintf(out, "\nChat %s is owned by codex exec (PID %d).\n이 채팅의 exec 작업을 중단하고 계정 %s의 Codex CLI로 이어받습니다.\n진행 중인 작업이 중단될 수 있으며, 이미 수행된 외부 작업은 되돌아가지 않습니다.\n이번 실행에서 해당 exec 작업을 중단할까요? [y/N]: ", l.Thread, owner.Process.PID, l.Account)
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

func waitExecRelease(ctx context.Context, shared, id string, owner execOwner) error {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		key, err := cr.NativeProcessIdentity(owner.Process.PID)
		gone := errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) || os.IsNotExist(err) || (err == nil && key != owner.Key)
		if err != nil && !gone {
			return errors.New("exec termination outcome could not be verified; no CLI was started")
		}
		if gone && !cr.WriterBusy(shared, id) {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("exec did not exit and release the chat in time; no force-kill or new CLI was attempted")
		case <-tick.C:
		}
	}
}

// Consent is supplied by the foreground prompt, never by an environment flag,
// persisted preference, piped yes or a background worker. Revalidate after it.
func stopConfirmedExec(ctx context.Context, c config.Config, l Launch, owner execOwner) error {
	shared := config.Expand(c.Runtime.SharedCodexHome)
	if !cr.WriterBusy(shared, l.Thread) {
		// It may be finishing naturally. Releasing the lock alone is not proof
		// that the original exec has stopped; never signal a now-unverified PID.
		wait, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := waitExecRelease(wait, shared, l.Thread, owner); err != nil {
			return err
		}
		return prepare(ctx, c, l, false)
	}
	latest, err := Plan(ctx, c, l.Thread, l.Account)
	if err != nil || latest.Binary != l.Binary || latest.Cwd != l.Cwd {
		return errors.New("selected account, runtime or project changed after confirmation; exec was left running")
	}
	current, err := findExecOwner(ctx, c, l)
	if err != nil || current != owner {
		return errors.New("exec ownership changed after confirmation; nothing was stopped; retry")
	}
	if err = syscall.Kill(owner.Process.PID, syscall.SIGTERM); err != nil {
		return fmt.Errorf("exec stop request failed; resume was not started: %w", err)
	}
	wait, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err = waitExecRelease(wait, shared, l.Thread, owner); err != nil {
		return err
	}
	// A supervisor may race to restart its job. Do not archive a new owner.
	return prepare(ctx, c, l, false)
}

func prepareExecHandoff(ctx context.Context, c config.Config, l Launch, owner execOwner) error {
	if !confirmExecHandoff(os.Stdin, os.Stderr, term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd())), l, owner) {
		return errors.New("exec takeover was not confirmed; run codexm in a separate foreground terminal to confirm this attempt")
	}
	fmt.Fprintln(os.Stderr, "exec 종료와 채팅 잠금 해제를 확인합니다…")
	return stopConfirmedExec(ctx, c, l, owner)
}
