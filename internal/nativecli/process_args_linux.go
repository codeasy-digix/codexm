package nativecli

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

func processArguments(pid int) ([]string, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || raw[len(raw)-1] != 0 {
		return nil, errors.New("cannot read native process arguments")
	}
	return strings.Split(string(raw[:len(raw)-1]), "\x00"), nil
}
