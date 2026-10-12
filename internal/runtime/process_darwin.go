package runtime

import (
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

func processIdentity(pid int) (string, error) {
	p, e := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if e != nil {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return "", os.ErrProcessDone
		}
		return "", e
	}
	if p.Proc.P_pid != int32(pid) || p.Proc.P_stat == 5 {
		return "", os.ErrProcessDone
	}
	return fmt.Sprintf("%d:%d:%d", p.Proc.P_pid, p.Proc.P_starttime.Sec, p.Proc.P_starttime.Usec), nil
}
