//go:build darwin

package terminal

import "golang.org/x/sys/unix"

func discardStartupInput(fd int) error {
	return unix.IoctlSetPointerInt(fd, unix.TIOCFLUSH, 1) // Darwin FREAD, from sys/file.h.
}
