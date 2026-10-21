package main

import (
	"encoding/json"
	"fmt"
	"github.com/codeasy-digix/codexm/internal/webbundle"
	"os"
)

func main() {
	if len(os.Args) != 3 || os.Args[1] != "seal" {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/webbundle seal BUNDLE_DIR")
		os.Exit(2)
	}
	m, err := webbundle.Seal(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(m)
}
