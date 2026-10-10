package manager

import (
	"github.com/codeasy-digix/codexm/internal/jobs"
	"os"
	"path/filepath"
	"testing"
)

func TestManagerDecisionsCannotEscapeScopeOrRestartPausedWork(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	os.Mkdir(child, 0700)
	outside := t.TempDir()
	js := []jobs.Job{{ID: "a", Managed: true, State: "error", Cwd: root}, {ID: "b", Managed: true, Paused: true, State: "paused", Cwd: root}}
	d := Decision{Actions: []Action{{Type: "create_job", ParentID: "a", Cwd: child, Kind: "task", Prompt: "inspect"}}}
	if e := Validate(d, js, "a", []string{root}, true); e != nil {
		t.Fatal(e)
	}
	d.Actions[0].Cwd = outside
	if Validate(d, js, "a", []string{root}, true) == nil {
		t.Fatal("escaped scope")
	}
	d = Decision{Actions: []Action{{Type: "retry", JobID: "b"}}}
	if Validate(d, js, "", []string{root}, false) == nil {
		t.Fatal("paused job restarted")
	}
	if _, e := Parse(`{"summary":"ok","actions":[],"shell":"rm"}`); e == nil {
		t.Fatal("unknown manager field accepted")
	}
}
