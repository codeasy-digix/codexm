//go:build linux

package terminal

import "golang.org/x/sys/unix"

func discardStartupInput(fd int) error {
	return unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIFLUSH)
}
