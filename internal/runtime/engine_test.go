package runtime

import (
	"context"
	"encoding/json"
	"github.com/codeasy-digix/codexm/internal/config"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeEngine(t *testing.T, dir, version string, compatible bool) string {
	t.Helper()
	os.MkdirAll(dir, 0700)
	methods := []string{"initialize", "thread/start", "thread/name/set", "thread/resume", "thread/read", "thread/list", "thread/turns/list", "thread/loaded/list", "turn/start", "turn/steer", "turn/interrupt", "thread/goal/get", "thread/goal/set", "account/read", "account/rateLimits/read"}
	if !compatible {
		methods = methods[:3]
	}
	items := []any{}
	for _, m := range methods {
		items = append(items, map[string]any{"properties": map[string]any{"method": map[string]any{"enum": []string{m}}}})
	}
	b, _ := json.Marshal(map[string]any{"oneOf": items})
	script := "#!/bin/sh\nfor arg do if [ \"$arg\" = '--version' ]; then printf '%s\\n' 'codex-cli " + version + "'; exit 0; fi; done\nprintf '%s' '" + string(b) + "' > \"$5/ClientRequest.json\"\n"
	p := filepath.Join(dir, "codex")
	if e := os.WriteFile(p, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(dir, "codex-code-mode-host"), []byte("helper"), 0700)
	return p
}
func TestEngineSurvivesExternalUpgradeAndRejectsIncompatibleAdoption(t *testing.T) {
	dir := t.TempDir()
	c := config.Defaults()
	c.Storage.StateDir = filepath.Join(dir, "state")
	c.Runtime.EnginesDir = filepath.Join(dir, "engines")
	source := fakeEngine(t, filepath.Join(dir, "upstream"), "0.162.1", true)
	old, e := PinEngine(context.Background(), c, source)
	if e != nil {
		t.Fatal(e)
	}
	fakeEngine(t, filepath.Dir(source), "99.0.0", false)
	if _, e = PinEngine(context.Background(), c, source); e == nil {
		t.Fatal("incompatible upgrade accepted")
	}
	if e = os.RemoveAll(filepath.Dir(source)); e != nil {
		t.Fatal(e)
	}
	r, e := ReadEngine(c)
	if e != nil || r.Binary != old.Binary || !strings.Contains(r.Version, "0.162.1") {
		t.Fatal(r, e)
	}
	if _, e = os.Stat(filepath.Join(filepath.Dir(r.Binary), "codex-code-mode-host")); e != nil {
		t.Fatal(e)
	}
	os.Chmod(r.Binary, 0700)
	os.WriteFile(r.Binary, []byte("corrupt"), 0700)
	if _, e = ReadEngine(c); e == nil {
		t.Fatal("engine corruption not detected")
	}
}
func TestWriterOwnershipCheckDoesNotDisturbLiveWriter(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "thread-writer-locks"), 0700)
	p := filepath.Join(dir, "thread-writer-locks", "t.lock")
	f, e := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		t.Fatal(e)
	}
	if !WriterBusy(dir, "t") {
		t.Fatal("live writer missed")
	}
	unix.Flock(int(f.Fd()), unix.LOCK_UN)
	if WriterBusy(dir, "t") {
		t.Fatal("released writer still busy")
	}
	if WriterBusy(dir, "absent") {
		t.Fatal("missing lock treated as active")
	}
}
