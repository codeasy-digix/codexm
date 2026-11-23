package runtime

import "golang.org/x/sys/unix"

func unixPeerIdentity(fd int) (int, int, error) {
	cred, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return 0, 0, err
	}
	return int(cred.Pid), int(cred.Uid), nil
}
