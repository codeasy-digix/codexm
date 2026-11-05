// Package nativecli lists local chats and hands execution to the official CLI.
// It never starts a worker, owns a model turn, or changes native database rows.
package nativecli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/store"
	"github.com/pelletier/go-toml/v2"
	"golang.org/x/sys/unix"
)

var threadID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Load accepts only the compact runtime/storage options. Old scheduling policy
// remains on disk for existing workers but cannot affect new native launches.
func Load(filename string) (config.Config, error) {
	c := config.Defaults()
	if filename == "" {
		filename = config.DefaultPath()
	}
	c.Path = config.Expand(filename)
	var selected struct {
		Runtime struct {
			Shared   string `toml:"shared_codex_home"`
			Accounts string `toml:"accounts_dir"`
			Binary   string `toml:"codex_binary"`
			Node     string `toml:"web_node_binary"`
		} `toml:"runtime"`
		Storage struct {
			State string `toml:"state_dir"`
		} `toml:"storage"`
		Registry struct {
			Removed []string `toml:"removed"`
		} `toml:"account_registry"`
	}
	b, err := os.ReadFile(c.Path)
	if err != nil && !os.IsNotExist(err) {
		return c, err
	}
	if len(b) > 0 {
		if err = toml.Unmarshal(b, &selected); err != nil {
			return c, err
		}
	}
	for dst, value := range map[*string]string{&c.Runtime.SharedCodexHome: selected.Runtime.Shared, &c.Runtime.AccountsDir: selected.Runtime.Accounts, &c.Runtime.CodexBinary: selected.Runtime.Binary, &c.Runtime.WebNodeBinary: selected.Runtime.Node, &c.Storage.StateDir: selected.Storage.State} {
		if value != "" {
			if !filepath.IsAbs(config.Expand(value)) {
				return c, errors.New("runtime and storage paths must be absolute")
			}
			*dst = value
		}
	}
	c.AccountRegistry.Removed = selected.Registry.Removed
	return c, nil
}

// Resolver uses the currently installed official command, not a manager engine
// pin. Resolving npm's official launcher is shared with the existing account code.
func Resolver(c config.Config) (cr.Resolver, error) {
	if c.Runtime.CodexBinary == "" {
		if p, err := exec.LookPath("codex"); err == nil {
			c.Runtime.CodexBinary = p
		}
	}
	return cr.Locate(c)
}

type Account struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
	Current bool   `json:"current"`
}

func Accounts(c config.Config) ([]Account, error) {
	names, err := accounts.SavedLogins(c)
	if os.IsNotExist(err) {
		err = nil
	}
	result := []Account{}
	def := accounts.CurrentNickname(c, config.Expand(c.Runtime.SharedCodexHome), "")
	cur := accounts.CurrentNickname(c, os.Getenv("CODEX_HOME"), "")
	for _, n := range names {
		hidden := false
		for _, x := range c.AccountRegistry.Removed {
			if x == n {
				hidden = true
				break
			}
		}
		if hidden {
			continue
		}
		result = append(result, Account{n, n == def, n == cur})
	}
	return result, err
}

type Launch struct {
	Thread      string           `json:"thread_id"`
	Title       string           `json:"title,omitempty"`
	Account     string           `json:"account"`
	Cwd         string           `json:"cwd"`
	Session     string           `json:"session,omitempty"`
	TmuxServer  string           `json:"tmux_server,omitempty"`
	TmuxSession string           `json:"tmux_session,omitempty"`
	Window      string           `json:"window,omitempty"`
	External    bool             `json:"external,omitempty"`
	Pane        string           `json:"pane,omitempty"`
	At          int64            `json:"at"`
	Binary      string           `json:"binary"`
	Config      string           `json:"config"`
	Ready       bool             `json:"ready"`
	Error       string           `json:"error,omitempty"`
	Continue    bool             `json:"continue,omitempty"`
	Owner       *processIdentity `json:"owner,omitempty"`
	OwnerKey    string           `json:"owner_key,omitempty"`
	AuthKey     string           `json:"auth_key,omitempty"`
	SwitchID    string           `json:"switch_id,omitempty"`
	Model       string           `json:"model,omitempty"`
}

func root(c config.Config) string { return filepath.Join(c.StateDir(), "native-cli") }
func receiptPath(c config.Config, id string) string {
	return filepath.Join(root(c), "last", id+".json")
}
func writeReceipt(c config.Config, l Launch) error {
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	return config.AtomicWrite(receiptPath(c, l.Thread), b, 0600)
}
func List(ctx context.Context, c config.Config) (catalog.List, error) {
	list, err := catalog.Read(ctx, config.Expand(c.Runtime.SharedCodexHome))
	if err != nil {
		return list, err
	}
	prior := map[string]string{}
	if db, e := store.ReadOnly(c.StateDir()); e == nil {
		defer db.DB.Close()
		if jobs, e := db.Jobs(); e == nil {
			for _, j := range jobs {
				prior[j.ThreadID] = j.Account
			}
		}
	}
	for i := range list.Sessions {
		e := &list.Sessions[i]
		e.Account = prior[e.ID]
		if !threadID.MatchString(e.ID) {
			continue
		}
		var l Launch
		if b, e2 := os.ReadFile(receiptPath(c, e.ID)); e2 == nil && json.Unmarshal(b, &l) == nil && l.Ready {
			e.Account = l.Account
		}
	}
	return list, nil
}

func Plan(ctx context.Context, c config.Config, id, account string) (Launch, error) {
	l := Launch{Thread: id, Account: account, At: time.Now().UnixMilli(), Config: c.Path}
	if !threadID.MatchString(id) {
		return l, errors.New("a complete chat UUID is required")
	}
	if account != "default" && (!config.Nickname.MatchString(account) || account == "") {
		return l, errors.New("choose a saved account or default")
	}
	list, err := List(ctx, c)
	if err != nil {
		return l, err
	}
	found := false
	for _, e := range list.Sessions {
		if e.ID == id {
			l.Cwd = e.Cwd
			l.Title = e.Title
			l.Model = e.Model
			found = true
			break
		}
	}
	if !found {
		return l, errors.New("chat is not in this machine's local conversation index")
	}
	if !filepath.IsAbs(l.Cwd) {
		return l, errors.New("chat working directory is missing; restore the project before resuming")
	}
	if i, e := os.Stat(l.Cwd); e != nil || !i.IsDir() {
		return l, fmt.Errorf("chat working directory is unavailable: %s", l.Cwd)
	}
	if account != "default" {
		if err = accounts.ValidateNativeAccount(c, account); err != nil {
			return l, err
		}
		if err = cr.EnsureAccountHome(c, account); err != nil {
			return l, err
		}
	}
	r, err := Resolver(c)
	if err != nil {
		return l, err
	}
	l.Binary = r.Binary
	return l, nil
}

func environment(c config.Config, account string) []string {
	if account == "default" {
		account = ""
	}
	all := cr.Env(c, account, "")
	out := make([]string, 0, len(all))
	for _, s := range all {
		k, _, _ := strings.Cut(s, "=")
		if k != "CODEX_THREAD_ID" && k != "CODEX_TURN_ID" && !strings.HasPrefix(k, "CODEXM_JOB_") && !strings.HasPrefix(k, "CODEXM_NATIVE_") {
			out = append(out, s)
		}
	}
	return out
}

func launchEnvironment(c config.Config, l Launch) []string {
	return append(environment(c, l.Account), "CODEXM_NATIVE_THREAD_ID="+l.Thread, "CODEXM_NATIVE_ACCOUNT="+l.Account)
}

// Prepare leaves resumable chats alone. Only archived chats need unarchive;
// a live writer must release ownership before a new native CLI can take over.
// Never edit a native DB, delete a lock, terminate another writer, or fork.
func Prepare(ctx context.Context, c config.Config, l Launch) error {
	return prepare(ctx, c, l, true)
}

func prepare(ctx context.Context, c config.Config, l Launch, releaseOwner bool) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	unlock, err := lockChatMutation(ctx, c, l.Thread)
	if err != nil {
		return err
	}
	defer unlock()
	archived, err := archivedState(ctx, c, l.Thread)
	if err != nil {
		return err
	}
	if cr.WriterBusy(config.Expand(c.Runtime.SharedCodexHome), l.Thread) {
		if !releaseOwner {
			return &ForeignWriterError{Thread: l.Thread}
		}
		// A separate --no-daemon archive cannot release a GUI/CLI's writer.
		// Ask the existing shared server only if it actually has this thread.
		released, err := archiveOnSharedDaemon(ctx, c, l)
		if err != nil {
			return fmt.Errorf("handoff for chat %s was not completed: %w", l.Thread, err)
		}
		if !released {
			if cr.WriterBusy(config.Expand(c.Runtime.SharedCodexHome), l.Thread) {
				return &ForeignWriterError{Thread: l.Thread}
			}
			archived, err = archivedState(ctx, c, l.Thread)
			if err != nil {
				return err
			}
		} else {
			wait, stopWaiting := context.WithTimeout(ctx, 5*time.Second)
			err = cr.WaitWriterRelease(wait, config.Expand(c.Runtime.SharedCodexHome), l.Thread)
			stopWaiting()
			if err != nil {
				return fmt.Errorf("chat %s was archived but its writer is still busy; resume was not started: %w", l.Thread, err)
			}
			archived = true
		}
	}
	if archived {
		cmd := exec.CommandContext(ctx, l.Binary, "--no-daemon", "unarchive", l.Thread)
		cmd.Env = environment(c, l.Account)
		cmd.Dir = l.Cwd
		b, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("official unarchive failed; resume was not started; retry unarchive for chat %s: %w: %s", l.Thread, err, cr.SafeText(strings.TrimSpace(string(b))))
		}
	}
	return nil
}

// Serialize native metadata mutations and resume preparation for the same ID.
// This is our operation gate, never Codex's native writer lock.
func lockChatMutation(ctx context.Context, c config.Config, id string) (func(), error) {
	if !threadID.MatchString(id) {
		return nil, errors.New("invalid chat UUID")
	}
	gate := filepath.Join(root(c), "handoffs", id+".lock")
	if err := os.MkdirAll(filepath.Dir(gate), 0700); err != nil {
		return nil, err
	}
	fd, err := unix.Open(gate, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if err != unix.EWOULDBLOCK {
			unix.Close(fd)
			return nil, err
		}
		select {
		case <-ctx.Done():
			unix.Close(fd)
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return func() { _ = unix.Flock(fd, unix.LOCK_UN); _ = unix.Close(fd) }, nil
}

func archivedState(ctx context.Context, c config.Config, id string) (bool, error) {
	list, err := List(ctx, c)
	if err != nil {
		return false, err
	}
	for _, e := range list.Sessions {
		if e.ID == id {
			return e.Archived, nil
		}
	}
	return false, errors.New("chat no longer exists in the local index")
}

func ResumeArgs(l Launch) []string {
	args := []string{l.Binary, "--no-daemon", "--no-alt-screen", "resume", l.Thread}
	if l.Continue {
		args = append(args, ContinuePrompt)
	}
	return args
}
func Resume(ctx context.Context, c config.Config, l Launch) error {
	if existing, err := ReusableTerminal(ctx, c, l.Thread, l.Account); err != nil {
		return err
	} else if existing != nil {
		if l.Continue {
			return errors.New("chat already has a live CLI; --continue was not sent again; use handoff for an account change")
		}
		fmt.Printf("Reconnecting to existing %s terminal (%s); its running authentication and work are preserved.\n", existing.Account, existing.Session)
		return Attach(ctx, c, existing.Session)
	}
	cleanup, err := prepareInteractive(ctx, c, l)
	if err != nil {
		return err
	}
	defer cleanup()
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, _ = filepath.EvalSymlinks(self)
	running, err := OpenContinuation(ctx, c, self, l.Thread, l.Account, l.Continue, l.SwitchID)
	if err != nil {
		return err
	}
	return Attach(ctx, c, running.Session)
}
