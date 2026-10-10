package main

import (
	"fmt"
	"github.com/codeasy-digix/codexm/internal/cli"
	"os"
)

func main() {
	if e := cli.Main(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, "codexm:", e)
		os.Exit(1)
	}
}
