package nativecli

import "golang.org/x/sys/unix"

func processArguments(pid int) ([]string, error) {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return nil, err
	}
	return decodeDarwinArguments(raw)
}
