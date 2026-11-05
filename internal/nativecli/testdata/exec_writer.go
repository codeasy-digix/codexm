// Synthetic external writer. Never installed or connected to a model.
package main

import (
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func main() {
	root := os.Getenv("CODEXM_EXEC_FIXTURE_ROOT")
	if root == "" {
		os.Exit(2)
	}
	locks := filepath.Join(root, "shared", "thread-writer-locks")
	if err := os.MkdirAll(locks, 0700); err != nil {
		panic(err)
	}
	ids := []string{"11111111-2222-4333-8444-555555555555"}
	mode := os.Getenv("CODEXM_EXEC_FIXTURE_MODE")
	if mode == "multiple" {
		ids = append(ids, "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
	}
	var files []*os.File
	for _, id := range ids {
		f, err := os.OpenFile(filepath.Join(locks, id+".lock"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			panic(err)
		}
		if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
			panic(err)
		}
		files = append(files, f)
	}
	terminated := make(chan os.Signal, 1)
	signal.Notify(terminated, syscall.SIGTERM)
	if err := os.WriteFile(filepath.Join(root, "ready"), nil, 0600); err != nil {
		panic(err)
	}
	for range terminated {
		f, err := os.OpenFile(filepath.Join(root, "signals"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
		_, _ = f.WriteString("SIGTERM\n")
		_ = f.Close()
		if mode == "ignore" {
			continue
		}
		for _, f := range files {
			_ = f.Close()
		}
		if mode == "release-only" {
			continue
		}
		return
	}
}
