package nativecli

// One requested account replacement, not a scheduler. The temporary tmux
// helper survives the requesting CLI and exits after verifying its replacement.
import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"golang.org/x/sys/unix"
)

const ContinuePrompt = "이 채팅에서 진행하던 미완료 작업을 계속 진행하세요. 기존 사용자 지시, 중지·승인 대기 상태와 목표·예산 한도를 유지하세요. 중단 직전 외부 작업의 결과가 불확실하면 실제 상태를 확인하고 중복 실행하지 마세요. 이미 완료된 작업을 반복하거나 새로운 목표를 만들지 마세요."

func authKey(c config.Config, account string) (string, error) {
	if account != "default" && !config.Nickname.MatchString(account) {
		return "", errors.New("invalid account nickname")
	}
	home := config.Expand(c.Runtime.SharedCodexHome)
	if account != "default" {
		home = filepath.Join(config.Expand(c.Runtime.AccountsDir), account)
	}
	b, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		return "", errors.New("file-backed ChatGPT authentication is required for account handoff")
	}
	var a struct {
		Tokens struct {
			Account string `json:"account_id"`
			ID      string `json:"id_token"`
		} `json:"tokens"`
	}
	if json.Unmarshal(b, &a) != nil || a.Tokens.Account == "" {
		return "", errors.New("ChatGPT account identity unavailable")
	}
	var claims struct {
		Subject string `json:"sub"`
		Email   string `json:"email"`
	}
	parts := strings.Split(a.Tokens.ID, ".")
	if len(parts) == 3 {
		if payload, e := base64.RawURLEncoding.DecodeString(parts[1]); e == nil {
			_ = json.Unmarshal(payload, &claims)
		}
	}
	h := sha256.Sum256([]byte(a.Tokens.Account + "\x00" + claims.Subject + "\x00" + claims.Email))
	return hex.EncodeToString(h[:]), nil
}

func trackLaunch(ctx context.Context, c config.Config, l *Launch) error {
	p, err := inspectProcess(ctx, os.Getpid())
	if err != nil {
		return err
	}
	key, err := cr.NativeProcessIdentity(p.PID)
	if err != nil {
		return err
	}
	l.Owner, l.OwnerKey = &p, key
	l.AuthKey, _ = authKey(c, l.Account) // Normal native/API-key use still works.
	return nil
}

func readLast(c config.Config, id string) (Launch, error) {
	var l Launch
	if !threadID.MatchString(id) {
		return l, errors.New("a complete chat UUID is required")
	}
	b, err := os.ReadFile(receiptPath(c, id))
	if err != nil {
		return l, err
	}
	if err = json.Unmarshal(b, &l); err != nil {
		return l, err
	}
	if l.Thread != id || !l.Ready || l.Owner == nil || l.OwnerKey == "" {
		return l, errors.New("this CLI has no current codexm ownership receipt; reopen with the updated codexm first; no process was stopped")
	}
	return l, nil
}

// An old receipt, PID alone, or open rollout is never permission to terminate.
func verifyOwnedCLI(ctx context.Context, c config.Config, l Launch) error {
	if l.Owner == nil || l.OwnerKey == "" {
		return errors.New("missing CLI process identity")
	}
	key, err := cr.NativeProcessIdentity(l.Owner.PID)
	if err != nil || key != l.OwnerKey {
		return errors.New("original CLI has exited or its PID changed")
	}
	p, err := inspectProcess(ctx, l.Owner.PID)
	if err != nil {
		return err
	}
	executable := p.Executable
	if runtime.GOOS == "linux" {
		executable, err = os.Readlink(fmt.Sprintf("/proc/%d/exe", p.PID))
		if err != nil {
			return err
		}
	}
	executable, err = filepath.EvalSymlinks(executable)
	expected, expectedErr := filepath.EvalSymlinks(l.Binary)
	if err != nil || expectedErr != nil || executable != expected {
		return errors.New("recorded PID no longer runs the launched Codex executable")
	}
	if !cr.WriterBusy(config.Expand(c.Runtime.SharedCodexHome), l.Thread) {
		return errors.New("original CLI does not own the requested chat")
	}
	lockDir, err := filepath.EvalSymlinks(filepath.Join(config.Expand(c.Runtime.SharedCodexHome), "thread-writer-locks"))
	if err != nil {
		return err
	}
	probe, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	b, err := exec.CommandContext(probe, "lsof", "-nP", "-a", "-p", strconv.Itoa(p.PID), "-F", "pn").Output()
	if err != nil {
		return errors.New("cannot verify CLI writer files; lsof is required")
	}
	loaded := map[string]bool{}
	pidMatches := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "p") {
			pidMatches = line == "p"+strconv.Itoa(p.PID)
		}
		if pidMatches && strings.HasPrefix(line, "n") {
			file := strings.TrimPrefix(line, "n")
			if filepath.Dir(file) == lockDir && strings.HasSuffix(file, ".lock") {
				loaded[strings.TrimSuffix(filepath.Base(file), ".lock")] = true
			}
		}
	}
	if len(loaded) != 1 || !loaded[l.Thread] {
		return errors.New("CLI owns another or multiple chats; nothing was stopped")
	}
	last, err := readLast(c, l.Thread)
	if err != nil || last.OwnerKey != l.OwnerKey || last.Account != l.Account || last.Binary != l.Binary || last.AuthKey != l.AuthKey {
		return errors.New("launch ownership receipt changed")
	}
	return nil
}

type Usage struct {
	Account   string    `json:"account"`
	FiveHour  *int      `json:"five_hour_remaining_percent"`
	Week      *int      `json:"weekly_remaining_percent"`
	CheckedAt time.Time `json:"checked_at"`
	Error     string    `json:"error,omitempty"`
}

func usageFromSnapshot(s accounts.Snapshot, model string) Usage {
	u := Usage{Account: s.Account, CheckedAt: s.CheckedAt, Error: s.Error}
	if !s.Authenticated && u.Error == "" {
		u.Error = "authentication not verified"
	}
	b := s.Single
	if x, ok := s.Buckets["codex"]; ok {
		b = x
	}
	for _, x := range s.Buckets {
		if model != "" && x.NormalModel == model {
			b = x
			break
		}
	}
	for _, w := range []*accounts.Window{b.Primary, b.Secondary} {
		if w == nil || w.Minutes == nil || w.UsedPercent < 0 || w.UsedPercent > 100 {
			continue
		}
		v := 100 - w.UsedPercent
		switch *w.Minutes {
		case 300:
			u.FiveHour = &v
		case 10080:
			u.Week = &v
		}
	}
	if u.Error == "" && (u.FiveHour == nil || u.Week == nil) {
		u.Error = "both 5h and weekly limits must be known"
	}
	if s.State(model) == "exhausted" && u.Error == "" && *u.FiveHour > 0 && *u.Week > 0 {
		u.Error = "account usage is blocked"
	}
	return u
}
func ReadUsage(ctx context.Context, c config.Config, account, model string) Usage {
	u := Usage{Account: account, CheckedAt: time.Now()}
	if account != "default" {
		if e := accounts.ValidateNativeAccount(c, account); e != nil {
			u.Error = e.Error()
			return u
		}
	}
	r, e := Resolver(c)
	if e != nil {
		u.Error = e.Error()
		return u
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return usageFromSnapshot(accounts.New(r).Read(ctx, account), model)
}
func usageLow(u Usage, threshold int) (bool, error) {
	if threshold < 0 || threshold > 100 {
		return false, errors.New("threshold must be between 0 and 100")
	}
	if u.Error != "" || u.FiveHour == nil || u.Week == nil {
		return false, fmt.Errorf("cannot determine %s limits: %s", u.Account, u.Error)
	}
	return *u.FiveHour <= threshold || *u.Week <= threshold, nil
}

type SwitchRequest struct {
	ID         string `json:"request_id"`
	Session    string `json:"helper_session"`
	Source     Launch `json:"source"`
	Target     Launch `json:"target"`
	TargetAuth string `json:"target_auth"`
	Caller     int    `json:"caller"`
	CallerKey  string `json:"caller_key"`
	Created    int64  `json:"created"`
}
type SwitchStatus struct {
	Request  string `json:"request_id"`
	Thread   string `json:"thread_id"`
	From     string `json:"from_account"`
	To       string `json:"to_account"`
	Phase    string `json:"phase"`
	Error    string `json:"error,omitempty"`
	Terminal string `json:"terminal,omitempty"`
	Updated  int64  `json:"updated"`
}

func switchDir(c config.Config, id string) string { return filepath.Join(root(c), "switches", id) }
func switchSession(c config.Config, id string) string {
	hash := sha256.Sum256([]byte(config.Expand(c.Runtime.SharedCodexHome) + "\x00" + id))
	return "codexm-handoff-" + hex.EncodeToString(hash[:8])
}
func TargetTerminal(c config.Config, s SwitchRequest) string {
	return terminalName(c, s.Target.Thread, s.Target.Account)
}

// Only a real foreground terminal may take the existing per-execution GUI
// confirmation path. An agent/tool subprocess must never consent for its GUI.
func TryGUIHandoff(ctx context.Context, c config.Config, id, account string) (bool, error) {
	if _, e := findGUIOwner(ctx, c, id); e != nil {
		return false, nil
	}
	l, e := Plan(ctx, c, id, account)
	if e != nil {
		return true, e
	}
	low, e := usageLow(ReadUsage(ctx, c, account, l.Model), 5)
	if e != nil {
		return true, e
	}
	if low {
		return true, errors.New("target account has 5% or less remaining")
	}
	l.Continue = true
	return true, Resume(ctx, c, l)
}
func switchStatus(c config.Config, s SwitchRequest, phase string, err error) error {
	status := SwitchStatus{Request: s.ID, Thread: s.Source.Thread, From: s.Source.Account, To: s.Target.Account, Phase: phase, Error: cr.SafeText(errorText(err)), Terminal: terminalName(c, s.Target.Thread, s.Target.Account), Updated: time.Now().Unix()}
	if e := writeJSON(filepath.Join(switchDir(c, s.ID), "result.json"), status); e != nil {
		return e
	}
	return writeJSON(filepath.Join(root(c), "switch-status", s.Source.Thread+".json"), status)
}
func HandoffStatus(c config.Config, id string) (SwitchStatus, error) {
	var s SwitchStatus
	if !threadID.MatchString(id) {
		return s, errors.New("a complete chat UUID is required")
	}
	b, e := os.ReadFile(filepath.Join(root(c), "switch-status", id+".json"))
	if e != nil {
		return s, e
	}
	e = json.Unmarshal(b, &s)
	return s, e
}

// Returns only acceptance, never claims completion. No old CLI is signalled
// until the helper is ready, this process arms it, and this process exits.
func RequestHandoff(ctx context.Context, c config.Config, self, id, account string, ifLow int) (SwitchRequest, error) {
	var s SwitchRequest
	source, e := readLast(c, id)
	if e != nil {
		return s, e
	}
	if e = verifyOwnedCLI(ctx, c, source); e != nil {
		return s, e
	}
	if source.Account == account {
		return s, errors.New("select a different account")
	}
	target, e := Plan(ctx, c, id, account)
	if e != nil {
		return s, e
	}
	sourceKey, e := authKey(c, source.Account)
	if e != nil || source.AuthKey == "" || sourceKey != source.AuthKey {
		return s, errors.New("running account identity is unknown or changed; no CLI was stopped")
	}
	targetKey, e := authKey(c, account)
	if e != nil {
		return s, e
	}
	if sourceKey == targetKey {
		return s, errors.New("both names identify the same login; select another account")
	}
	if ifLow >= 0 {
		low, e := usageLow(ReadUsage(ctx, c, source.Account, source.Model), ifLow)
		if e != nil {
			return s, e
		}
		if !low {
			return s, errors.New("current account is above the requested threshold; no handoff requested")
		}
	}
	low, e := usageLow(ReadUsage(ctx, c, account, target.Model), 5)
	if e != nil {
		return s, e
	}
	if low {
		return s, errors.New("target account has 5% or less remaining; original CLI was preserved")
	}
	if old, e := readSession(c, terminalName(c, id, account)); e == nil {
		screen, e := Screen(ctx, c, old.Session)
		if e != nil {
			return s, e
		}
		if screen.Alive {
			return s, errors.New("target terminal already exists; original CLI was preserved")
		}
	}
	if _, e = exec.LookPath("tmux"); e != nil {
		return s, errors.New("tmux is required for a handoff that survives its caller")
	}
	nonce := make([]byte, 16)
	if _, e = rand.Read(nonce); e != nil {
		return s, e
	}
	s.ID = hex.EncodeToString(nonce)
	s.Session = switchSession(c, id)
	s.Source, s.Target, s.TargetAuth = source, target, targetKey
	s.Caller = os.Getpid()
	s.CallerKey, e = cr.NativeProcessIdentity(s.Caller)
	if e != nil {
		return s, e
	}
	s.Created = time.Now().Unix()
	dir := switchDir(c, s.ID)
	if e = os.MkdirAll(dir, 0700); e != nil {
		return s, e
	}
	file := filepath.Join(dir, "request.json")
	if e = writeJSON(file, s); e != nil {
		return s, e
	}
	args := []string{"new-session", "-d", "-s", s.Session, "-c", os.TempDir()}
	for _, key := range []string{"PATH", "HOME", "LANG", "LC_ALL", "LC_CTYPE", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME"} {
		if v, ok := os.LookupEnv(key); ok {
			args = append(args, "-e", key+"="+v)
		}
	}
	args = append(args, self, "--config", c.Path, "__handoff", file)
	if _, e = tmux(ctx, CodexTmuxServer, args...); e != nil {
		return s, e
	}
	wait, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	for {
		if markerExists(filepath.Join(dir, "ready")) {
			break
		}
		if markerExists(filepath.Join(dir, "result.json")) {
			return s, errors.New("handoff preflight failed; inspect codexm handoff-status " + id)
		}
		select {
		case <-wait.Done():
			return s, errors.New("handoff helper did not acknowledge readiness; no shutdown authorized")
		case <-time.After(50 * time.Millisecond):
		}
	}
	if e = switchStatus(c, s, "accepted", nil); e != nil {
		return s, e
	}
	if e = config.AtomicWrite(filepath.Join(dir, "armed"), []byte("one requested account replacement\n"), 0600); e != nil {
		return s, e
	}
	return s, nil
}

func RunHandoff(ctx context.Context, c config.Config, self, file string) (resultErr error) {
	id := filepath.Base(filepath.Dir(file))
	decoded, e := hex.DecodeString(id)
	if e != nil || len(decoded) != 16 || file != filepath.Join(switchDir(c, id), "request.json") {
		return errors.New("invalid handoff request path")
	}
	i, e := os.Lstat(file)
	if e != nil || !i.Mode().IsRegular() || i.Mode().Perm()&0077 != 0 {
		return errors.New("handoff request must be a private regular file")
	}
	st, ok := i.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Getuid()) {
		return errors.New("foreign handoff request")
	}
	b, e := os.ReadFile(file)
	if e != nil {
		return e
	}
	var s SwitchRequest
	if e = json.Unmarshal(b, &s); e != nil {
		return e
	}
	if s.ID != id || !threadID.MatchString(s.Source.Thread) || s.Target.Thread != s.Source.Thread || s.Session != switchSession(c, s.Source.Thread) || time.Since(time.Unix(s.Created, 0)) < 0 || time.Since(time.Unix(s.Created, 0)) > time.Minute {
		return errors.New("stale or invalid handoff request")
	}
	if pane := os.Getenv("TMUX_PANE"); pane == "" {
		return errors.New("handoff helper must run in its owned tmux pane")
	} else {
		identity, e := tmux(ctx, CodexTmuxServer, "display-message", "-p", "-t", pane, "#{session_name}|#{pane_pid}")
		if e != nil || strings.TrimSpace(identity) != fmt.Sprintf("%s|%d", s.Session, os.Getpid()) {
			return errors.New("handoff helper pane ownership changed")
		}
	}
	defer func() {
		if resultErr != nil {
			_ = switchStatus(c, s, "failed", resultErr)
		}
	}()
	// Never leave a dead helper pane or reuse a claimed request.
	_, _ = tmux(ctx, CodexTmuxServer, "set-option", "-w", "-t", s.Session, "remain-on-exit", "off")
	if e = os.Mkdir(filepath.Join(switchDir(c, id), "claimed"), 0700); e != nil {
		return errors.New("handoff request was already claimed; not replayed")
	}
	gate := filepath.Join(root(c), "switch-"+s.Source.Thread+".lock")
	fd, e := unix.Open(gate, unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return e
	}
	defer unix.Close(fd)
	if e = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); e != nil {
		return errors.New("another handoff is in progress")
	}
	ctx, cancel := context.WithTimeout(ctx, 70*time.Second)
	defer cancel()
	if e = verifyOwnedCLI(ctx, c, s.Source); e != nil {
		return e
	}
	if e = config.AtomicWrite(filepath.Join(switchDir(c, id), "ready"), []byte("ready\n"), 0600); e != nil {
		return e
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		caller, e := cr.NativeProcessIdentity(s.Caller)
		if markerExists(filepath.Join(switchDir(c, id), "armed")) && (e != nil || caller != s.CallerKey) {
			break
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return errors.New("caller did not finish and arm this handoff; original CLI preserved")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Revalidate everything immediately before the only destructive action.
	target, e := Plan(ctx, c, s.Target.Thread, s.Target.Account)
	if e != nil {
		return e
	}
	key, e := authKey(c, target.Account)
	if e != nil || key != s.TargetAuth {
		return errors.New("target authentication changed; original CLI preserved")
	}
	key, e = authKey(c, s.Source.Account)
	if e != nil || key != s.Source.AuthKey {
		return errors.New("source authentication changed; original CLI preserved")
	}
	if e = verifyOwnedCLI(ctx, c, s.Source); e != nil {
		return e
	}
	if e = switchStatus(c, s, "stopping", nil); e != nil {
		return e
	}
	if e = syscall.Kill(s.Source.Owner.PID, syscall.SIGTERM); e != nil {
		return e
	}
	deadline = time.Now().Add(15 * time.Second)
	for {
		key, e = cr.NativeProcessIdentity(s.Source.Owner.PID)
		if (e != nil || key != s.Source.OwnerKey) && !cr.WriterBusy(config.Expand(c.Runtime.SharedCodexHome), s.Source.Thread) {
			break
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return errors.New("old CLI or writer did not exit; no force-kill or second CLI was started")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if e = switchStatus(c, s, "starting", nil); e != nil {
		return e
	}
	l, e := OpenContinuation(ctx, c, self, target.Thread, target.Account, true, s.ID)
	if e != nil {
		return e
	}
	deadline = time.Now().Add(35 * time.Second)
	for {
		launched, e := readLast(c, target.Thread)
		if e == nil && launched.SwitchID == s.ID && launched.Account == target.Account && verifyOwnedCLI(ctx, c, launched) == nil {
			followTerminal(ctx, c, s.Source, l)
			return switchStatus(c, s, "started", nil)
		}
		screen, e := Screen(ctx, c, l.Session)
		if e != nil || !screen.Alive {
			return errors.New("replacement CLI exited before acquiring the chat; no prompt was retried; inspect " + AttachCommand(l))
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return errors.New("replacement ownership not confirmed; inspect " + AttachCommand(l) + "; do not repeat uncertain continuation")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func followTerminal(ctx context.Context, c config.Config, old, next Launch) {
	if old.Session == "" || old.TmuxServer != CodexTmuxServer {
		return
	}
	// Only our single, now-dead pane qualifies for cleanup/client following.
	p, e := tmux(ctx, old.TmuxServer, "list-panes", "-s", "-t", "="+old.Session, "-F", "#{pane_id}|#{pane_dead}|#{pane_pid}")
	if e != nil || strings.TrimSpace(p) != fmt.Sprintf("%s|1|%d", old.Pane, old.Owner.PID) {
		return
	}
	clients, e := tmux(ctx, old.TmuxServer, "list-clients", "-t", "="+old.Session, "-F", "#{client_name}")
	if e != nil {
		return
	}
	for _, client := range strings.Fields(clients) {
		if _, e = tmux(ctx, old.TmuxServer, "switch-client", "-c", client, "-t", "="+next.Session); e != nil {
			return
		}
	}
	_, _ = tmux(ctx, old.TmuxServer, "kill-session", "-t", "="+old.Session)
}
