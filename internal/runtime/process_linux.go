package runtime

import (
	"fmt"
	"os"
	"strings"
)

func processIdentity(pid int) (string, error) {
	b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if e != nil {
		return "", e
	}
	i := strings.LastIndexByte(string(b), ')')
	if i < 0 {
		return "", os.ErrProcessDone
	}
	f := strings.Fields(string(b)[i+1:])
	if len(f) < 20 || f[0] == "Z" {
		return "", os.ErrProcessDone
	}
	return fmt.Sprintf("%d:%s", pid, f[19]), nil
}
