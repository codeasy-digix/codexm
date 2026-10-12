package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
)

type processRecord struct {
	PID      int    `json:"pid"`
	Identity string `json:"identity"`
	UID      int    `json:"uid"`
	Session  string `json:"session"`
}

func ProcessSession(c config.Config, run string) string {
	return "process:" + filepath.Join(c.StateDir(), "workers", run+".process.json")
}
func IsProcess(s string) bool { return strings.HasPrefix(s, "process:") }
func processPath(s string) (string, error) {
	p := strings.TrimPrefix(s, "process:")
	if !IsProcess(s) || !filepath.IsAbs(p) || !strings.HasSuffix(p, ".process.json") {
		return "", errors.New("invalid worker process receipt")
	}
	if e := PrivateSocketDir(filepath.Dir(p)); e != nil {
		return "", e
	}
	return p, nil
}
func readProcess(s string) (processRecord, error) {
	var r processRecord
	p, e := processPath(s)
	if e != nil {
		return r, e
	}
	st, e := os.Lstat(p)
	if e != nil {
		return r, e
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		return r, errors.New("worker receipt must be private")
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return r, e
	}
	if e = json.Unmarshal(b, &r); e != nil {
		return r, e
	}
	if r.PID <= 1 || r.UID != os.Getuid() || r.Session != s {
		return r, errors.New("worker receipt ownership mismatch")
	}
	id, e := processIdentity(r.PID)
	if e != nil {
		return r, e
	}
	if id != r.Identity {
		return r, errors.New("worker PID was reused")
	}
	return r, nil
}
func launchProcess(ctx context.Context, cmd *exec.Cmd, session string) error {
	p, e := processPath(session)
	if e != nil {
		return e
	}
	if _, e = readProcess(session); e == nil {
		return errors.New("worker process already exists")
	}
	lf, e := os.OpenFile(strings.TrimSuffix(p, ".json")+".log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		return e
	}
	defer lf.Close()
	cmd.Stdout, cmd.Stderr = lf, lf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if e = cmd.Start(); e != nil {
		return e
	}
	go cmd.Wait()
	id, e := processIdentity(cmd.Process.Pid)
	if e != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("worker exited during startup: %w", e)
	}
	r := processRecord{PID: cmd.Process.Pid, UID: os.Getuid(), Identity: id, Session: session}
	b, _ := json.Marshal(r)
	if e = config.AtomicWrite(p, b, 0600); e != nil {
		_ = cmd.Process.Kill()
		return e
	}
	return nil
}
func killProcess(ctx context.Context, session string) error {
	r, e := readProcess(session)
	if os.IsNotExist(e) || errors.Is(e, os.ErrProcessDone) {
		return nil
	}
	if e != nil {
		return e
	}
	group, e := syscall.Getpgid(r.PID)
	if e != nil {
		return e
	}
	if group != r.PID {
		return errors.New("worker process group ownership mismatch")
	}
	if e = syscall.Kill(-r.PID, syscall.SIGTERM); e != nil && !errors.Is(e, syscall.ESRCH) {
		return e
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	forced := false
	for {
		if _, e = readProcess(session); e != nil {
			if os.IsNotExist(e) || errors.Is(e, os.ErrProcessDone) || errors.Is(e, syscall.ESRCH) {
				return nil
			}
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			if !forced {
				if _, e = readProcess(session); e == nil {
					_ = syscall.Kill(-r.PID, syscall.SIGKILL)
				}
				forced = true
				deadline.Reset(3 * time.Second)
			} else {
				return errors.New("worker did not exit after termination")
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
}
