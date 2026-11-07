//go:build gui_experiment

package nativecli

// Bounded orchestration of the official daemon, not a new Codex runtime or
// rotation service. Reuse the existing account boundary and RPC implementation.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

type DaemonInfo struct {
	Account  string          `json:"account"`
	Version  string          `json:"version"`
	Socket   string          `json:"socket"`
	Process  processIdentity `json:"process"`
	Identity string          `json:"identity"`
	Loaded   []string        `json:"loaded_threads,omitempty"`
}

type daemonVersion struct {
	Status  string `json:"status"`
	Socket  string `json:"socketPath"`
	Binary  string `json:"managedCodexPath"`
	Version string `json:"appServerVersion"`
}

type daemonAccount struct {
	Account *struct{ Type, Email string } `json:"account"`
	Routing *struct {
		Workspace string `json:"chatgptAccountId"`
	} `json:"workspaceRouting"`
}

type daemonConnection struct {
	info   DaemonInfo
	client *cr.Client
}

func sameDaemonPath(a, b string) bool {
	left, e1 := filepath.EvalSymlinks(a)
	right, e2 := filepath.EvalSymlinks(b)
	return e1 == nil && e2 == nil && left == right
}

func daemonRoot(c config.Config) string {
	return filepath.Join(config.Expand(c.Storage.StateDir), "daemon-experiment")
}

func daemonAccountHome(c config.Config, account string) (string, error) {
	if account == "" || account == "default" {
		return "", errors.New("daemon experiment requires an explicit saved account; the native default/GUI daemon is preserved")
	}
	if err := validateGUIAccount(c, account); err != nil {
		return "", err
	}
	home := filepath.Join(config.Expand(c.Runtime.AccountsDir), account)
	info, err := os.Lstat(home)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("daemon account home must be a real directory")
	}
	return home, nil
}

func daemonNative(ctx context.Context, c config.Config, account string, args ...string) ([]byte, error) {
	r, err := Resolver(c)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.Binary, args...)
	cmd.Env = environment(c, account)
	cmd.Dir = config.Expand(c.Runtime.SharedCodexHome)
	return cmd.Output() // Do not include stderr: native errors can contain auth URLs.
}

func daemonDiskIdentity(c config.Config, account string) (accounts.LoginIdentity, string, error) {
	i := accounts.InspectIdentity(c, account)
	if i.Key == "" || i.Email == "" {
		return i, "", errors.New("saved login lacks an identifiable ChatGPT user/workspace")
	}
	b, err := os.ReadFile(filepath.Join(config.Expand(c.Runtime.AccountsDir), account, "auth.json"))
	var a struct {
		Tokens struct {
			Account string `json:"account_id"`
		} `json:"tokens"`
	}
	if err != nil || json.Unmarshal(b, &a) != nil || a.Tokens.Account == "" {
		return i, "", errors.New("saved workspace identity is unavailable")
	}
	return i, a.Tokens.Account, nil
}

func daemonLiveIdentity(c config.Config, account string, live daemonAccount) (string, error) {
	i, workspace, err := daemonDiskIdentity(c, account)
	if err != nil {
		return "", err
	}
	if live.Account == nil || live.Account.Type != "chatgpt" || live.Account.Email == "" ||
		!strings.EqualFold(i.Email, live.Account.Email) || live.Routing == nil || live.Routing.Workspace != workspace {
		return "", errors.New("daemon's live ChatGPT user/workspace differs from the selected login or cannot be verified; no login was changed")
	}
	return i.Key, nil
}

func openDaemon(ctx context.Context, c config.Config, account string, requireReceipt bool) (*daemonConnection, error) {
	home, err := daemonAccountHome(c, account)
	if err != nil {
		return nil, err
	}
	b, err := daemonNative(ctx, c, account, "app-server", "daemon", "version")
	var version daemonVersion
	if err != nil || json.Unmarshal(b, &version) != nil || version.Status != "running" {
		return nil, errors.New("selected official daemon is not running; explicitly use daemon start --account NAME first")
	}
	if version.Version != "0.167.0" {
		return nil, errors.New("daemon protocol experiment currently requires the tested official runtime 0.167.0")
	}
	if !sameDaemonPath(version.Socket, filepath.Join(home, "app-server-control", "app-server-control.sock")) ||
		!sameDaemonPath(version.Binary, filepath.Join(home, "packages", "app-server-daemon", "current", "bin", "codex")) {
		return nil, errors.New("official daemon paths do not belong to the selected account")
	}
	socket, err := filepath.EvalSymlinks(version.Socket)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(socket)
	if err != nil || st.Mode()&os.ModeSocket == 0 {
		return nil, errors.New("daemon socket is unavailable")
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return nil, errors.New("daemon socket belongs to another user")
	}
	// A cold official account/read may include bounded workspace discovery.
	// Allow its documented discovery window without retrying the RPC.
	probe, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	b, err = exec.CommandContext(probe, "lsof", "-nP", "-t", "--", socket).Output()
	ids := strings.Fields(string(b))
	if err != nil || len(ids) != 1 {
		return nil, errors.New("cannot verify one official daemon socket owner")
	}
	pid, err := strconv.Atoi(ids[0])
	if err != nil {
		return nil, err
	}
	p, err := inspectProcess(probe, pid)
	if err != nil {
		return nil, err
	}
	actual, e1 := filepath.EvalSymlinks(p.Executable)
	expected, e2 := filepath.EvalSymlinks(version.Binary)
	args, e3 := processArguments(pid)
	command, known := nativeCommand(args)
	if e1 != nil || e2 != nil || e3 != nil || actual != expected || !known || command != "app-server" {
		return nil, errors.New("socket owner is not the selected official app-server process")
	}
	client, err := cr.DialUnix(probe, socket)
	if err != nil {
		return nil, err
	}
	// Drain notifications without answering model requests or approving tools.
	go func() {
		for {
			select {
			case <-client.Events:
			case <-client.Done:
				return
			}
		}
	}()
	d := &daemonConnection{info: DaemonInfo{Account: account, Version: version.Version, Socket: socket, Process: p}, client: client}
	var live daemonAccount
	if err = client.Call(probe, "account/read", map[string]bool{"refreshToken": false}, &live); err == nil {
		d.info.Identity, err = daemonLiveIdentity(c, account, live)
	}
	if err == nil && requireReceipt {
		var pinned DaemonInfo
		b, err = os.ReadFile(filepath.Join(daemonRoot(c), "servers", account+".json"))
		if err == nil {
			err = json.Unmarshal(b, &pinned)
		}
		if err != nil || pinned.Identity != d.info.Identity || pinned.Socket != socket ||
			pinned.Process.PID != p.PID || pinned.Process.Started != p.Started || pinned.Process.Executable != p.Executable {
			err = errors.New("daemon/login incarnation is not registered by this experiment; explicitly run daemon start to verify the current server (no automatic restart)")
		}
	}
	if err != nil {
		client.Close()
		return nil, err
	}
	return d, nil
}

// Start is the only explicit service-start operation in this experiment. It never
// restarts, logs into, stops, or changes the authentication of an existing server.
func StartDaemonExperiment(ctx context.Context, c config.Config, account string) (DaemonInfo, error) {
	if _, err := daemonAccountHome(c, account); err != nil {
		return DaemonInfo{}, err
	}
	lock, err := cr.AccountLoginLock(c, account)
	if err != nil {
		return DaemonInfo{}, err
	}
	defer lock.Close()
	before, _, err := daemonDiskIdentity(c, account)
	if err != nil {
		return DaemonInfo{}, err
	}
	version, err := daemonNative(ctx, c, account, "--version")
	if err != nil || strings.TrimSpace(string(version)) != "codex-cli 0.167.0" {
		return DaemonInfo{}, errors.New("daemon start requires the tested packaged official CLI 0.167.0; the installed runtime is not changed")
	}
	b, probeErr := daemonNative(ctx, c, account, "app-server", "daemon", "version")
	var running daemonVersion
	if probeErr != nil || json.Unmarshal(b, &running) != nil || running.Status != "running" {
		home, _ := daemonAccountHome(c, account)
		if _, e := os.Lstat(filepath.Join(home, "app-server-control", "app-server-control.sock")); !os.IsNotExist(e) {
			return DaemonInfo{}, errors.New("an existing daemon socket could not be verified; inspect it with the official CLI before starting anything")
		}
		if _, err := daemonNative(ctx, c, account, "app-server", "daemon", "start"); err != nil {
			return DaemonInfo{}, errors.New("official daemon start did not complete; inspect its native status before retrying; no automatic retry or stop")
		}
	}
	d, err := openDaemon(ctx, c, account, false)
	if err != nil {
		return DaemonInfo{}, err
	}
	defer d.client.Close()
	if d.info.Identity != before.Key {
		return DaemonInfo{}, errors.New("login changed during daemon start; no connection registered")
	}
	b, err = json.MarshalIndent(d.info, "", "  ")
	if err == nil {
		err = config.AtomicWrite(filepath.Join(daemonRoot(c), "servers", account+".json"), b, 0600)
	}
	return d.info, err
}

func loadedDaemon(ctx context.Context, client handoffRPC) ([]string, error) {
	result := []string{}
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		var r struct {
			Data       []string
			NextCursor string
		}
		params := map[string]any{"limit": 100}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := client.Call(ctx, "thread/loaded/list", params, &r); err != nil {
			return nil, err
		}
		result = append(result, r.Data...)
		if r.NextCursor == "" {
			return result, nil
		}
		if seen[r.NextCursor] {
			return nil, errors.New("daemon repeated its loaded-chat cursor")
		}
		seen[r.NextCursor], cursor = true, r.NextCursor
	}
	return nil, errors.New("daemon loaded-chat listing exceeded its bounded page limit")
}

func DaemonExperimentStatus(ctx context.Context, c config.Config, account string) (DaemonInfo, error) {
	d, err := openDaemon(ctx, c, account, true)
	if err != nil {
		return DaemonInfo{}, err
	}
	defer d.client.Close()
	d.info.Loaded, err = loadedDaemon(ctx, d.client)
	return d.info, err
}

func (d *daemonConnection) verify(ctx context.Context, c config.Config, id string, mustOwn bool) error {
	if !sameProcess(ctx, d.info.Process) {
		return errors.New("official daemon incarnation changed")
	}
	var live daemonAccount
	if err := d.client.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &live); err != nil {
		return err
	}
	key, err := daemonLiveIdentity(c, d.info.Account, live)
	if err != nil {
		return err
	}
	if key != d.info.Identity {
		return errors.New("daemon authentication changed during operation")
	}
	if id == "" {
		return nil
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if !sameProcess(ctx, d.info.Process) {
			return errors.New("official daemon changed while verifying ownership")
		}
		pids, err := writerProcesses(ctx, config.Expand(c.Runtime.SharedCodexHome), id)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if len(pids) == 0 && !mustOwn {
			return nil
		}
		if len(pids) != 1 || pids[0] != d.info.Process.PID {
			return fmt.Errorf("chat is not exclusively owned by account %s's verified daemon; GUI/CLI owners are preserved", d.info.Account)
		}
		ids, err := loadedDaemon(ctx, d.client)
		if err != nil {
			return err
		}
		for _, loaded := range ids {
			if loaded == id {
				return nil
			}
		}
		// Idle unloading removes the loaded entry shortly before releasing its
		// writer. An attach may wait for this verified server to finish releasing;
		// archive/interrupt still require a currently loaded chat. No mutation or
		// foreign-writer retry is authorized by this observation-only wait.
		if mustOwn || time.Now().After(deadline) {
			return errors.New("daemon does not list this chat as loaded")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(40 * time.Millisecond):
		}
	}
}
