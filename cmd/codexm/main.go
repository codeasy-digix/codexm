package main

import (
	"fmt"
	"github.com/codeasy-digix/codexm/internal/cli"
	"os"
	"path/filepath"
)

func main() {
	args := os.Args[1:]
	// Optional compatibility link for existing OS units and SSH relay peers.
	// There is still one binary, installer and implementation, owned by codexm.
	if filepath.Base(os.Args[0]) == "codex-history-sync" {
		args = append([]string{"history-sync"}, args...)
	}
	if e := cli.Main(args); e != nil {
		fmt.Fprintln(os.Stderr, "codexm:", e)
		os.Exit(1)
	}
}
