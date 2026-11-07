package nativecli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

// This is an isolated authentication/ownership experiment, not the product's
// read-and-queue relay. A loopback model checks synthetic bearer markers; no
// real account, credential, GUI or native home is used.
func (f *guiControlFixture) accountPeer(name string) (*cr.Client, *exec.Cmd) {
	f.t.Helper()
	if err := cr.EnsureAccountHome(f.c, name); err != nil {
		f.t.Fatal(err)
	}
	cmd := exec.CommandContext(f.ctx, f.binary, "--no-daemon", "--disable", "plugins", "--disable", "apps", "--disable", "hooks", "--enable", "skip_host_skill_discovery", "app-server", "--listen", "stdio://")
	cmd.Env, cmd.Dir = append(environment(f.c, name), "CODEXM_GUI_FIXTURE_KEY=fixture-account-b"), f.cmd.Dir
	peer, err := cr.StartStdio(f.ctx, cmd)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() {
		peer.Close()
		select {
		case <-peer.ProcessDone:
		case <-time.After(3 * time.Second):
			f.t.Error("owned test peer did not exit on EOF")
			_ = cmd.Process.Kill() // This isolated child only.
			<-peer.ProcessDone
		}
	})
	return peer, cmd
}

func TestGUIControlAccountQueueUsesOwnerAuthentication(t *testing.T) {
	f := newGUIControlFixture(t, "stdio")
	id, neighbor := f.thread(), f.thread()
	f.start(id, "GUI_FIXTURE_FIRST")
	f.completed(id, 1)
	key, err := cr.NativeProcessIdentity(f.cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	peer, _ := f.accountPeer("fixture-b")
	var unsub struct{ Status string }
	if err := peer.Call(f.ctx, "thread/unsubscribe", map[string]any{"threadId": id}, &unsub); err != nil || unsub.Status != "notLoaded" {
		t.Fatal("non-owning subscription result", unsub.Status, err)
	}
	f.assertOwner(key, id, neighbor)
	if err := peer.Call(f.ctx, "thread/resume", map[string]any{"threadId": id}, nil); !cr.IsWriterCollision(err) {
		t.Fatal("another account must not take over the GUI-like writer", err)
	}
	if err := peer.Call(f.ctx, "thread/queue/add", map[string]any{"threadId": id, "clientUserMessageId": "00000000-0000-4000-8000-000000000099", "input": cr.TextInput("GUI_FIXTURE_QUEUED_BY_B")}, nil); err != nil {
		t.Fatal(err)
	}
	f.completed(id, 2)
	f.assertOwner(key, id, neighbor)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.keys) != 2 || f.keys[0] != "Bearer fixture-owner-a" || f.keys[1] != "Bearer fixture-owner-a" {
		t.Fatal("queue submitter did not select execution authentication", f.keys)
	}
	t.Log("account-B relay queued input, but account-A owner executed it; foreign unsubscribe/resume did not release either writer")
}

func TestGUIControlAccountOwnerReleaseAllowsSelectedCLI(t *testing.T) {
	f := newGUIControlFixture(t, "stdio")
	id, neighbor := f.thread(), f.thread()
	f.start(id, "GUI_FIXTURE_TARGET")
	f.completed(id, 1)
	f.start(neighbor, "GUI_FIXTURE_NEIGHBOR")
	f.completed(neighbor, 1)
	neighborTurn := f.start(neighbor, "GUI_FIXTURE_ACTIVE")
	select {
	case <-f.active:
	case <-f.ctx.Done():
		t.Fatal("neighbor did not enter its active turn")
	}
	key, err := cr.NativeProcessIdentity(f.cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	// Only the original owner releases the selected chat. This is deliberately
	// not a mutation performed by the product's separate queue client.
	if err := f.client.Call(f.ctx, "thread/archive", map[string]any{"threadId": id}, nil); err != nil {
		t.Fatal(err)
	}
	if err := cr.WaitWriterRelease(f.ctx, f.c.Runtime.SharedCodexHome, id); err != nil {
		t.Fatal(err)
	}
	peer, cmd := f.accountPeer("fixture-b")
	if err := peer.Call(f.ctx, "thread/unarchive", map[string]any{"threadId": id}, nil); err != nil {
		t.Fatal(err)
	}
	if err := peer.Call(f.ctx, "thread/resume", map[string]any{"threadId": id}, nil); err != nil {
		t.Fatal(err)
	}
	if err := peer.Call(f.ctx, "turn/start", map[string]any{"threadId": id, "input": cr.TextInput("GUI_FIXTURE_CONTINUE_B")}, nil); err != nil {
		t.Fatal(err)
	}
	// Read from the actual new owner, not a passive reconstruction of a live turn.
	old := f.client
	f.client = peer
	f.completed(id, 2)
	f.client = old
	activeNeighbor, _, err := f.client.Recent(f.ctx, neighbor, 10, "")
	if err != nil || len(activeNeighbor.Turns) != 2 || activeNeighbor.Turns[1].ID != neighborTurn || activeNeighbor.Turns[1].Status != "inProgress" {
		t.Fatal("neighboring GUI-like turn was interrupted during account transfer", err)
	}
	f.once.Do(func() { close(f.release) })
	f.completed(neighbor, 2)
	f.start(neighbor, "GUI_FIXTURE_NEIGHBOR_STILL_A")
	f.completed(neighbor, 3)
	if current, e := cr.NativeProcessIdentity(f.cmd.Process.Pid); e != nil || current != key {
		t.Fatal("original server was restarted", e)
	}
	for chat, pid := range map[string]int{id: cmd.Process.Pid, neighbor: f.cmd.Process.Pid} {
		writers, e := writerProcesses(f.ctx, f.c.Runtime.SharedCodexHome, chat)
		if e != nil || len(writers) != 1 || writers[0] != pid {
			t.Fatal("per-chat owner mismatch", e)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Join(f.keys, ",") != "Bearer fixture-owner-a,Bearer fixture-owner-a,Bearer fixture-owner-a,Bearer fixture-account-b,Bearer fixture-owner-a" {
		t.Fatal("selected account did not stay isolated", f.keys)
	}
	// Account homes share history/locks but no credential files were copied.
	if _, e := os.Stat(filepath.Join(f.c.Runtime.AccountsDir, "fixture-b", "auth.json")); !os.IsNotExist(e) {
		t.Fatal("fixture unexpectedly copied a login")
	}
	t.Log("same ID continued with B after owner-side release while neighbor stayed active with A; GUI-like server PID unchanged")
}
