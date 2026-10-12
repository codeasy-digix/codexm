package supervisor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	"github.com/codeasy-digix/codexm/internal/jobs"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/service"
)

// Opt-in: all native state, credentials and the OS service label are isolated.
// No model turn or real account login is used. The caller supplies a built binary.
func TestOSServiceRestartsAndAdoptsOwnedNativeWorker(t *testing.T) {
	if os.Getenv("CODEXM_SERVICE_TEST") != "1" || runtime.GOOS != "darwin" {
		t.Skip("opt-in local LaunchAgent test")
	}
	s := fixture(t)
	s.Self = os.Getenv("CODEXM_BINARY")
	if s.Self == "" {
		t.Fatal("CODEXM_BINARY required")
	}
	s.Config.Runtime.CodexBinary = os.Getenv("CODEXM_NATIVE_RUNTIME")
	s.Config.Supervisor.ReconcileIntervalSeconds = 1
	if e := s.Config.Save(); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	r, e := cr.Resolve(ctx, s.Config)
	if e != nil {
		t.Fatal(e)
	}
	s.Resolver = r
	j := jobs.Job{ID: jobs.ID(), Managed: true, Kind: "chat", State: "idle", Account: "account-work", Cwd: t.TempDir(), Store: s.Config.Runtime.SharedCodexHome, RunID: jobs.ID(), EngineBinary: r.Binary, EngineVersion: r.Version}
	j.Session = cr.ProcessSession(s.Config, j.RunID)
	j.Socket = cr.SocketPath(s.Config, j.RunID[:12])
	if e = s.Store.PutJob(j); e != nil {
		t.Fatal(e)
	}
	if e = s.Store.PutRun(jobs.Run{ID: j.RunID, JobID: j.ID, Account: j.Account, StartedAt: time.Now(), State: "idle", Session: j.Session, Socket: j.Socket}); e != nil {
		t.Fatal(e)
	}
	if e = r.LaunchWorker(ctx, s.Self, cr.WorkerSpec{Account: j.Account, Socket: j.Socket, Session: j.Session, Cwd: j.Cwd}); e != nil {
		t.Fatal(e)
	}
	defer cr.KillSession(context.Background(), j.Session)
	if e = s.connect(ctx, j, false); e != nil {
		t.Fatal(e)
	}
	j, _ = s.Store.Job(j.ID)
	s.cancel()
	s.wg.Wait()
	for _, c := range s.clients {
		c.Close()
	}
	s.clients = map[string]*cr.Client{}
	m := service.Manager{Config: s.Config, Self: s.Self}
	if m.Label() == (service.Manager{Config: config.Defaults()}).Label() {
		t.Fatal("isolated label required")
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		if m.Registered() {
			if e := m.Uninstall(cleanup); e != nil {
				t.Error("service cleanup", e)
			}
		}
	})
	if e = m.Install(ctx, true); e != nil {
		t.Fatal(e)
	}
	if e = service.WaitReady(ctx, func(ctx context.Context) error { return Ping(ctx, s.Config) }); e != nil {
		t.Fatal(e)
	}
	first := m.Status(ctx)
	if !first.Running || first.PID < 2 {
		t.Fatal("service is not running", first.Problem)
	}
	verify := func() {
		var status Status
		if e := Request(ctx, s.Config, "status", nil, &status); e != nil {
			t.Fatal(e)
		}
		if !status.ServiceMode || len(status.Jobs) != 1 || status.Jobs[0].RunID != j.RunID || status.Jobs[0].ThreadID != j.ThreadID || !cr.HasSession(ctx, j.Session) || !cr.WriterBusy(j.Store, j.ThreadID) || status.ManagerCalls != 0 {
			t.Fatal("service did not preserve the worker")
		}
	}
	verify()
	if e = syscall.Kill(first.PID, syscall.SIGKILL); e != nil {
		t.Fatal(e)
	}
	if e = service.WaitReady(ctx, func(ctx context.Context) error {
		next := m.Status(ctx)
		if next.PID == first.PID || !next.Running {
			return errors.New("waiting for replacement service")
		}
		return Ping(ctx, s.Config)
	}); e != nil {
		t.Fatal(e)
	}
	verify()
	// A graceful observer restart also keeps the engine and writer lease.
	if e = m.Stop(ctx); e != nil {
		t.Fatal(e)
	}
	if e = m.Start(ctx); e != nil {
		t.Fatal(e)
	}
	if e = service.WaitReady(ctx, func(ctx context.Context) error { return Ping(ctx, s.Config) }); e != nil {
		t.Fatal(e)
	}
	verify()
	// Change the immutable manager executable path while keeping the same worker.
	upgrade := m
	upgrade.Self = s.Self + ".upgrade-test"
	if e = os.Link(s.Self, upgrade.Self); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.Remove(upgrade.Self) })
	if e = upgrade.Upgrade(ctx, func(ctx context.Context) error { return Ping(ctx, s.Config) }); e != nil {
		t.Fatal(e)
	}
	verify()
	m = upgrade
	// A new observer that fails its readiness check must restore the previous
	// definition without replacing the live native worker or its writer lease.
	previous, e := os.ReadFile(m.DefinitionPath())
	if e != nil {
		t.Fatal(e)
	}
	failed := m
	failed.Self = s.Self + ".rollback-test"
	if e = os.Link(s.Self, failed.Self); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.Remove(failed.Self) })
	failure, stopFailure := context.WithTimeout(ctx, 6*time.Second)
	e = failed.Upgrade(failure, func(context.Context) error { return errors.New("injected readiness failure") })
	stopFailure()
	if e == nil {
		t.Fatal("failed readiness was accepted")
	}
	restored, e := os.ReadFile(m.DefinitionPath())
	if e != nil || !bytes.Equal(previous, restored) {
		t.Fatal("previous service definition was not restored", e)
	}
	if e = service.WaitReady(ctx, func(ctx context.Context) error { return Ping(ctx, s.Config) }); e != nil {
		t.Fatal("rollback service did not reconnect", e)
	}
	verify()
	// Explicit shutdown disarms recovery and parks this isolated worker.
	if e = Request(ctx, s.Config, "shutdown", nil, nil); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(20 * time.Second)
	for Ping(ctx, s.Config) == nil && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if Ping(ctx, s.Config) == nil || cr.HasSession(ctx, j.Session) {
		t.Fatal("explicit shutdown left service or worker running")
	}
	if e = m.Ensure(ctx); e == nil {
		t.Fatal("explicit shutdown was automatically restarted")
	}
}
