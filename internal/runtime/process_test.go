package runtime

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
)

func TestOwnedWorkerSurvivesCallerAndStopsWithoutTmux(t *testing.T) {
	c := config.Defaults()
	c.Storage.StateDir = t.TempDir()
	s := ProcessSession(c, "000000000000000000000001")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e := launchProcess(ctx, exec.Command("sleep", "60"), s); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = KillSession(context.Background(), s) })
	if !HasSession(ctx, s) {
		t.Fatal("worker is not alive")
	}
	if e := KillSession(ctx, s); e != nil {
		t.Fatal(e)
	}
	if HasSession(ctx, s) {
		t.Fatal("worker survived explicit stop")
	}
}

func TestStaleReceiptCannotKillAnotherProcess(t *testing.T) {
	c := config.Defaults()
	c.Storage.StateDir = t.TempDir()
	s := ProcessSession(c, "000000000000000000000002")
	p, e := processPath(s)
	if e != nil {
		t.Fatal(e)
	}
	r := processRecord{PID: os.Getpid(), UID: os.Getuid(), Identity: "stale", Session: s}
	b, _ := json.Marshal(r)
	if e = config.AtomicWrite(p, b, 0600); e != nil {
		t.Fatal(e)
	}
	if HasSession(context.Background(), s) {
		t.Fatal("reused PID accepted")
	}
	if e = KillSession(context.Background(), s); e == nil {
		t.Fatal("stale receipt should refuse termination")
	}
	if _, e = os.Stat(filepath.Dir(p)); e != nil {
		t.Fatal(e)
	}
}
