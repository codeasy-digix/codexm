package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/codeasy-digix/codexm/internal/config"
)

type Resolver struct {
	Config     config.Config
	Controller string
	Binary     string
	Version    string
}

func Resolve(ctx context.Context, c config.Config) (Resolver, error) {
	r, e := Locate(c)
	if e != nil {
		return r, e
	}
	if pinned, e := ReadEngine(c); e == nil {
		r.Binary, r.Version = pinned.Binary, pinned.Version
		return r, nil
	} else if !os.IsNotExist(e) {
		return r, e
	}
	return r.Refresh(ctx)
}
func Locate(c config.Config) (Resolver, error) {
	r := Resolver{Config: c}
	bin, e := exec.LookPath(c.Runtime.Controller)
	if e != nil {
		return r, fmt.Errorf("codex-accounts is required: %w", e)
	}
	r.Controller = bin
	return r, nil
}
func (r Resolver) Refresh(ctx context.Context) (Resolver, error) {
	c := r.Config
	bin := r.Controller
	if c.Runtime.CodexBinary != "" {
		pinned, e := PinEngine(ctx, c, config.Expand(c.Runtime.CodexBinary))
		if e != nil {
			return r, e
		}
		r.Binary, r.Version = pinned.Binary, pinned.Version
		return r, nil
	}
	cmd := exec.CommandContext(ctx, bin, "doctor", "--json")
	cmd.Env = Env(c, "", "")
	b, e := cmd.Output()
	if e != nil {
		return r, fmt.Errorf("Codex runtime lookup failed: %w", e)
	}
	var d struct {
		Runtime      string `json:"runtime"`
		CodexVersion string `json:"codexVersion"`
	}
	if e = json.Unmarshal(b, &d); e != nil {
		return r, e
	}
	if d.Runtime == "" {
		return r, fmt.Errorf("account controller returned no runtime")
	}
	if c.Runtime.CodexBinary != "" {
		d.Runtime = config.Expand(c.Runtime.CodexBinary)
	}
	pinned, e := PinEngine(ctx, c, d.Runtime)
	if e != nil {
		return r, e
	}
	r.Binary, r.Version = pinned.Binary, pinned.Version
	return r, nil
}
func Env(c config.Config, account, pinned string) []string {
	remove := map[string]bool{"CODEX_ACCOUNT": true, "CODEX_HOME": true, "CODEX_SQLITE_HOME": true, "CODEX_SHARED_HOME": true, "CODEX_ACCOUNTS_DIR": true, "CODEX_ACCOUNTS_RUNTIME": true, "OPENAI_API_KEY": true, "CODEX_API_KEY": true, "CODEX_ACCESS_TOKEN": true}
	env := []string{}
	for _, s := range os.Environ() {
		k, _, _ := strings.Cut(s, "=")
		if !remove[k] {
			env = append(env, s)
		}
	}
	shared := config.Expand(c.Runtime.SharedCodexHome)
	accounts := config.Expand(c.Runtime.AccountsDir)
	env = append(env, "CODEX_SHARED_HOME="+shared, "CODEX_SQLITE_HOME="+shared, "CODEX_ACCOUNTS_DIR="+accounts)
	if account != "" {
		env = append(env, "CODEX_ACCOUNT="+account, "CODEX_HOME="+filepath.Join(accounts, account))
	} else {
		env = append(env, "CODEX_HOME="+shared)
	}
	if pinned != "" {
		env = append(env, "CODEX_ACCOUNTS_RUNTIME="+pinned)
	}
	return env
}
func (r Resolver) Command(ctx context.Context, account string, args ...string) *exec.Cmd {
	shared, _ := json.Marshal(config.Expand(r.Config.Runtime.SharedCodexHome))
	options := []string{"__native", "--no-daemon", "-c", "sqlite_home=" + string(shared)}
	cmd := exec.CommandContext(ctx, r.Controller, append(options, args...)...)
	cmd.Env = Env(r.Config, account, r.Binary)
	cmd.Stderr = io.Discard
	return cmd
}
func (r Resolver) Probe(ctx context.Context, account string) (*Client, error) {
	args := []string{"app-server", "--listen", "stdio://", "-c", `model_provider="openai"`}
	return StartStdio(ctx, r.Command(ctx, account, args...))
}
func (r Resolver) NativeTUI(socket, thread string) *exec.Cmd {
	args := []string{"-c", "check_for_update_on_startup=false", "--remote", "unix://" + socket, "resume", thread}
	cmd := exec.Command(r.Binary, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

type WorkerSpec struct{ Controller, Binary, Account, Socket, Session, Cwd, SharedHome, AccountsDir, ConfigPath string }

func Worker(spec WorkerSpec) error {
	c, e := config.Load(spec.ConfigPath)
	if e != nil {
		return e
	}
	c.Runtime.SharedCodexHome = spec.SharedHome
	c.Runtime.AccountsDir = spec.AccountsDir
	if !config.Nickname.MatchString(spec.Account) || !filepath.IsAbs(spec.Socket) {
		return fmt.Errorf("invalid worker specification")
	}
	shared, _ := json.Marshal(spec.SharedHome)
	args := []string{spec.Controller, "__native", "--no-daemon", "-c", "sqlite_home=" + string(shared), "app-server", "--listen", "unix://" + spec.Socket, "-c", `model_provider="openai"`}
	if e = os.Chdir(spec.Cwd); e != nil {
		return e
	}
	// The official process owns signals, terminal I/O and the server socket.
	return syscall.Exec(spec.Controller, args, Env(c, spec.Account, spec.Binary))
}
func (r Resolver) LaunchWorker(ctx context.Context, self string, spec WorkerSpec) error {
	spec.ConfigPath = r.Config.Path
	spec.Controller = r.Controller
	spec.Binary = r.Binary
	spec.SharedHome = config.Expand(r.Config.Runtime.SharedCodexHome)
	spec.AccountsDir = config.Expand(r.Config.Runtime.AccountsDir)
	b, e := json.Marshal(spec)
	if e != nil {
		return e
	}
	p := filepath.Join(r.Config.StateDir(), "workers", spec.Session+".json")
	if e = config.AtomicWrite(p, b, 0600); e != nil {
		return e
	}
	if e = PrivateSocketDir(filepath.Dir(spec.Socket)); e != nil {
		return e
	}
	cmd := exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", spec.Session, "-c", spec.Cwd, ShellQuote(self)+" __worker "+ShellQuote(p))
	b, e = cmd.CombinedOutput()
	if e != nil {
		return fmt.Errorf("start tmux: %s: %w", SafeText(string(b)), e)
	}
	return nil
}
func ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func HasSession(ctx context.Context, s string) bool {
	return s != "" && exec.CommandContext(ctx, "tmux", "has-session", "-t", "="+s).Run() == nil
}
func KillSession(ctx context.Context, s string) error {
	if s == "" {
		return nil
	}
	return exec.CommandContext(ctx, "tmux", "kill-session", "-t", "="+s).Run()
}
