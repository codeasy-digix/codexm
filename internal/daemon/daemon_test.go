package daemon

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestKilledDaemonRecordIsDetectedWithoutDeletingIt(t *testing.T) {
	shared := t.TempDir()
	dir := filepath.Join(shared, "app-server-daemon")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	child := exec.Command("/bin/sleep", "30")
	if e := child.Start(); e != nil {
		t.Fatal(e)
	}
	defer child.Process.Kill()
	record, _ := json.Marshal(map[string]int{"pid": child.Process.Pid})
	p := filepath.Join(dir, "daemon.pid")
	if e := os.WriteFile(p, record, 0600); e != nil {
		t.Fatal(e)
	}
	if pid, alive, e := Process(shared); e != nil || !alive || pid != child.Process.Pid {
		t.Fatal(pid, alive, e)
	}
	child.Process.Kill()
	child.Wait()
	if _, alive, e := Process(shared); e != nil || alive {
		t.Fatal("killed process record was considered running", alive, e)
	}
	if got, e := os.ReadFile(p); e != nil || string(got) != string(record) {
		t.Fatal("official process record was modified")
	}
}
