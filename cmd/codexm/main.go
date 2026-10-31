package main

import (
	"fmt"
	"github.com/codeasy-digix/codexm/internal/cli"
	"os"
	"path/filepath"
)

func main() {
	args := os.Args[1:]
	// A stale legacy symlink must not launch the manager or native daemon.
	if filepath.Base(os.Args[0]) == "codex-history-sync" {
		fmt.Fprintln(os.Stderr, "history-sync has been removed; retire this legacy executable link")
		os.Exit(1)
	}
	if e := cli.Main(args); e != nil {
		fmt.Fprintln(os.Stderr, "codexm:", e)
		os.Exit(1)
	}
}
