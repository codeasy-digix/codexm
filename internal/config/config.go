package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const Version = "0.2.1"

var Nickname = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func (c Config) DefaultWorker() string {
	if c.WorkerAccounts.DefaultAccount != "" {
		return c.WorkerAccounts.DefaultAccount
	}
	if len(c.WorkerAccounts.Priority) > 0 {
		return c.WorkerAccounts.Priority[0]
	}
	return ""
}

type Config struct {
	SchemaVersion int `toml:"schema_version" json:"schema_version"`
	Manager       struct {
		Account                  string `toml:"account" json:"account"`
		Invocation               string `toml:"invocation"`
		Model                    string `toml:"model,omitempty"`
		MaxInputChars            int    `toml:"max_input_chars"`
		MaxCallsPerIncident      int    `toml:"max_calls_per_incident"`
		MaxCallsPerHour          int    `toml:"max_calls_per_hour"`
		DecisionTimeoutSeconds   int    `toml:"decision_timeout_seconds"`
		FailureCooldownSeconds   int    `toml:"failure_cooldown_seconds"`
		AutomaticAccountFallback bool   `toml:"automatic_account_fallback"`
	} `toml:"manager"`
	WorkerAccounts struct {
		Priority                            []string `toml:"priority"`
		DefaultAccount                      string   `toml:"default_account,omitempty"`
		FallbackAccount                     string   `toml:"fallback_account"`
		FallbackRequiresAllPrimaryExhausted bool     `toml:"fallback_requires_all_primary_exhausted"`
		PreferredRecheckSeconds             int      `toml:"preferred_recheck_seconds"`
		UnknownIsExhausted                  bool     `toml:"unknown_is_exhausted"`
	} `toml:"worker_accounts"`
	Supervisor struct {
		ReconcileIntervalSeconds          int    `toml:"reconcile_interval_seconds"`
		TmuxSession                       string `toml:"tmux_session"`
		WorkerSessionPrefix               string `toml:"worker_session_prefix"`
		ExitWhenNoClientsAndNoPendingWork bool   `toml:"exit_when_no_clients_and_no_pending_work"`
	} `toml:"supervisor"`
	Execution struct {
		MaxParallelJobs                       int    `toml:"max_parallel_jobs"`
		MaxChildDepth                         int    `toml:"max_child_depth"`
		WorkspaceWritePolicy                  string `toml:"workspace_write_policy"`
		PreserveThreadSettingsOnAccountSwitch bool   `toml:"preserve_thread_settings_on_account_switch"`
	} `toml:"execution"`
	Completion struct {
		ChatOnTurnEnd                string `toml:"chat_on_turn_end"`
		TaskWithoutCriteriaOnTurnEnd string `toml:"task_without_criteria_on_turn_end"`
		GoalCompletionSource         string `toml:"goal_completion_source"`
	} `toml:"completion"`
	UI struct {
		InitialView     string `toml:"initial_view"`
		ShowInputTarget bool   `toml:"show_input_target"`
		QuitAction      string `toml:"quit_action"`
	} `toml:"ui"`
	Runtime struct {
		Transport       string `toml:"transport"`
		SharedCodexHome string `toml:"shared_codex_home"`
		AccountsDir     string `toml:"accounts_dir,omitempty"`
		Controller      string `toml:"controller,omitempty"`
		CodexBinary     string `toml:"codex_binary,omitempty"`
		EnginesDir      string `toml:"engines_dir"`
	} `toml:"runtime"`
	Storage struct {
		StateDir string `toml:"state_dir"`
	} `toml:"storage"`
	Logging struct {
		UsageSampleSeconds int `toml:"usage_sample_seconds"`
	} `toml:"logging"`
	Path string `toml:"-" json:"-"`
}

func DefaultPath() string {
	if p := os.Getenv("CODEXM_CONFIG"); p != "" {
		return Expand(p)
	}
	return filepath.Join(xdg("XDG_CONFIG_HOME", ".config"), "codexm", "config.toml")
}
func xdg(key, fallback string) string {
	if p := os.Getenv(key); filepath.IsAbs(p) {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, fallback)
}
func Expand(p string) string {
	if p == "~" || len(p) > 1 && p[:2] == "~/" {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, p[1:])
	}
	return filepath.Clean(p)
}
func Defaults() Config {
	var c Config
	c.SchemaVersion = 1
	c.Manager.Invocation = "on_demand"
	c.Manager.MaxInputChars = 16000
	c.Manager.MaxCallsPerIncident = 1
	c.Manager.MaxCallsPerHour = 12
	c.Manager.DecisionTimeoutSeconds = 120
	c.Manager.FailureCooldownSeconds = 600
	c.WorkerAccounts.Priority = []string{"account-work", "account-secondary", "account-main", "account-backup"}
	c.WorkerAccounts.FallbackAccount = "account-backup"
	c.WorkerAccounts.FallbackRequiresAllPrimaryExhausted = true
	c.WorkerAccounts.PreferredRecheckSeconds = 600
	c.Supervisor.ReconcileIntervalSeconds = 30
	c.Supervisor.TmuxSession = "codexm-supervisor"
	c.Supervisor.WorkerSessionPrefix = "codexm-job-"
	c.Supervisor.ExitWhenNoClientsAndNoPendingWork = true
	c.Execution.MaxParallelJobs = 2
	c.Execution.MaxChildDepth = 3
	c.Execution.WorkspaceWritePolicy = "exclusive"
	c.Execution.PreserveThreadSettingsOnAccountSwitch = true
	c.Completion.ChatOnTurnEnd = "idle"
	c.Completion.TaskWithoutCriteriaOnTurnEnd = "result_ready"
	c.Completion.GoalCompletionSource = "codex_goal"
	c.UI.InitialView = "agents"
	c.UI.ShowInputTarget = true
	c.UI.QuitAction = "detach"
	c.Runtime.Transport = "unix"
	c.Runtime.SharedCodexHome = "~/.codex"
	c.Runtime.AccountsDir = "~/.codex-accounts"
	c.Runtime.Controller = "" // Legacy configuration is accepted but never executed.
	c.Runtime.EnginesDir = filepath.Join(xdg("XDG_DATA_HOME", ".local/share"), "codexm", "engines")
	c.Storage.StateDir = filepath.Join(xdg("XDG_STATE_HOME", ".local/state"), "codexm")
	c.Logging.UsageSampleSeconds = 60
	return c
}
func Load(p string) (Config, error) {
	c := Defaults()
	if p == "" {
		p = DefaultPath()
	}
	c.Path = Expand(p)
	b, e := os.ReadFile(c.Path)
	if errors.Is(e, os.ErrNotExist) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	if e = toml.Unmarshal(b, &c); e != nil {
		return c, fmt.Errorf("config: %w", e)
	}
	c.Runtime.Controller = "" // Accept old files without advertising an obsolete dependency.
	return c, c.Validate()
}
func (c Config) Validate() error {
	if c.SchemaVersion != 1 {
		return fmt.Errorf("unsupported configuration schema %d", c.SchemaVersion)
	}
	if c.Manager.Account != "" && !Nickname.MatchString(c.Manager.Account) {
		return errors.New("invalid manager account")
	}
	if c.Manager.MaxInputChars < 256 || c.Manager.MaxCallsPerIncident < 1 || c.Manager.MaxCallsPerHour < 1 || c.Manager.DecisionTimeoutSeconds < 1 {
		return errors.New("manager limits must be positive")
	}
	if c.Supervisor.ReconcileIntervalSeconds < 1 || c.Execution.MaxParallelJobs < 1 || c.Execution.MaxChildDepth < 1 || c.WorkerAccounts.PreferredRecheckSeconds < 1 || c.Logging.UsageSampleSeconds < 1 {
		return errors.New("execution limits and intervals must be positive")
	}
	seen := map[string]bool{}
	for _, n := range c.WorkerAccounts.Priority {
		if !Nickname.MatchString(n) || seen[n] {
			return fmt.Errorf("invalid or duplicate worker account %q", n)
		}
		seen[n] = true
	}
	if len(seen) == 0 {
		return errors.New("at least one worker account is required")
	}
	if a := c.WorkerAccounts.DefaultAccount; a != "" && !Nickname.MatchString(a) {
		return errors.New("invalid default worker account")
	}
	if f := c.WorkerAccounts.FallbackAccount; f != "" && (!slices.Contains(c.WorkerAccounts.Priority, f) || c.WorkerAccounts.Priority[len(c.WorkerAccounts.Priority)-1] != f) {
		return errors.New("fallback account must be last in priority")
	}
	if c.WorkerAccounts.UnknownIsExhausted {
		return errors.New("unknown account state cannot be treated as exhausted")
	}
	if !filepath.IsAbs(Expand(c.Storage.StateDir)) || !filepath.IsAbs(Expand(c.Runtime.SharedCodexHome)) || !filepath.IsAbs(Expand(c.Runtime.AccountsDir)) {
		return errors.New("state and Codex paths must be absolute")
	}
	if c.Runtime.Transport != "unix" || c.Execution.WorkspaceWritePolicy != "exclusive" || !c.Execution.PreserveThreadSettingsOnAccountSwitch {
		return errors.New("unsupported runtime or workspace policy")
	}
	if !filepath.IsAbs(Expand(c.Runtime.EnginesDir)) || (c.Runtime.CodexBinary != "" && !filepath.IsAbs(Expand(c.Runtime.CodexBinary))) {
		return errors.New("engine paths must be absolute")
	}
	if c.Manager.Invocation != "on_demand" || c.Manager.AutomaticAccountFallback || c.Completion.ChatOnTurnEnd != "idle" || c.Completion.TaskWithoutCriteriaOnTurnEnd != "result_ready" || c.Completion.GoalCompletionSource != "codex_goal" || c.UI.QuitAction != "detach" {
		return errors.New("unsupported manager, completion or quit policy")
	}
	if !Nickname.MatchString(c.Supervisor.TmuxSession) || !Nickname.MatchString(c.Supervisor.WorkerSessionPrefix) {
		return errors.New("invalid tmux session name")
	}
	return nil
}
func (c Config) Save() error {
	return withConfigLock(c.Path, c.saveUnlocked)
}
func (c Config) saveUnlocked() error {
	if e := c.Validate(); e != nil {
		return e
	}
	if c.Path == "" {
		return errors.New("missing configuration path")
	}
	b, e := toml.Marshal(c)
	if e != nil {
		return e
	}
	if old, e := os.ReadFile(c.Path); e == nil {
		if e = AtomicWrite(c.Path+".previous", old, 0600); e != nil {
			return e
		}
	}
	return AtomicWrite(c.Path, b, 0600)
}
func AtomicWrite(p string, b []byte, mode os.FileMode) error {
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".codexm-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e == nil {
		e = os.Rename(name, p)
	}
	if e == nil {
		if d, err := os.Open(filepath.Dir(p)); err == nil {
			_ = d.Sync()
			_ = d.Close()
		}
	}
	return e
}
func (c Config) StateDir() string { return Expand(c.Storage.StateDir) }
func (c Config) Interval() time.Duration {
	return time.Duration(c.Supervisor.ReconcileIntervalSeconds) * time.Second
}
