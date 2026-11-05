package nativecli

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
)

// Darwin returns argc, the executable path, padding, argv and then environment.
// Read exactly argc arguments. Environment and prompt text never enter errors.
func decodeDarwinArguments(raw []byte) ([]string, error) {
	invalid := errors.New("cannot decode native process arguments")
	if len(raw) < 5 {
		return nil, invalid
	}
	argc := int(binary.NativeEndian.Uint32(raw[:4]))
	if argc < 1 || argc > 4096 {
		return nil, invalid
	}
	data := raw[4:]
	end := bytes.IndexByte(data, 0)
	if end < 0 {
		return nil, invalid
	}
	data = bytes.TrimLeft(data[end+1:], "\x00")
	args := make([]string, 0, argc)
	for range argc {
		end = bytes.IndexByte(data, 0)
		if end < 0 {
			return nil, invalid
		}
		args = append(args, string(data[:end]))
		data = data[end+1:]
	}
	return args, nil
}

// Only the first positional argument is authoritative. Unknown/ambiguous global
// options fail closed; a config value or a word inside a prompt is not a command.
func nativeCommand(args []string) (string, bool) {
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return "", true // Remaining arguments are an interactive prompt.
		}
		if !strings.HasPrefix(arg, "-") {
			return arg, true
		}
		name, _, assigned := strings.Cut(arg, "=")
		switch name {
		case "-c", "--config", "--enable", "--disable", "-m", "--model", "--local-provider", "-p", "--profile", "-s", "--sandbox", "-a", "--ask-for-approval", "-C", "--cd", "--add-dir":
			if !assigned {
				i++
				if i >= len(args) {
					return "", false
				}
			}
		case "--no-daemon", "--no-alt-screen", "--strict-config", "--oss", "--approve-for-me", "--full-auto", "--dangerously-bypass-approvals-and-sandbox", "--dangerously-bypass-hook-trust", "--search", "--worktree":
			if assigned {
				return "", false
			}
		default:
			// Fixed-arity short options also allow an attached value (-cKEY=V).
			if len(arg) < 3 || arg[0] != '-' || !strings.ContainsRune("cmpsaC", rune(arg[1])) {
				return "", false
			}
		}
	}
	return "", len(args) > 0
}

func isExecArguments(args []string) bool {
	command, known := nativeCommand(args)
	return known && (command == "exec" || command == "e")
}
