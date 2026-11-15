package nativecli

// The web frontend is an official app-server client. It does not revive the old
// worker/scheduler or own model logic, and never takes over a foreign writer.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

var WebMethods = []Method{
	{"web.catalog", false, false}, {"web.history", false, false}, {"web.items", false, false}, {"web.item", false, false},
	{"web.create", true, false}, {"web.send", true, true}, {"web.interrupt", true, false},
	{"web.pin", true, false}, {"web.move", true, false}, {"web.archive", true, false},
	{"web.delete", true, false}, {"web.projectCreate", true, false},
	{"web.pending", false, false}, {"web.respond", true, false},
}

func webSharedMethod(name string) bool {
	return !strings.HasPrefix(name, "terminal.") && !strings.HasPrefix(name, "chats.") && name != "accounts.activate"
}

func webBridgeMethods() []Method {
	methods := []Method{}
	for _, method := range Methods {
		if webSharedMethod(method.Name) {
			methods = append(methods, method)
		}
	}
	return append(methods, WebMethods...)
}

type webParams struct {
	Thread   string          `json:"thread_id"`
	Account  string          `json:"account"`
	Cursor   string          `json:"cursor"`
	Turn     string          `json:"turn_id"`
	Item     string          `json:"item_id"`
	Text     string          `json:"text"`
	Mode     string          `json:"mode"`
	Cwd      string          `json:"cwd"`
	Project  string          `json:"project_id"`
	Name     string          `json:"name"`
	Search   string          `json:"search"`
	Archived bool            `json:"archived"`
	Pinned   bool            `json:"pinned"`
	Limit    int             `json:"limit"`
	Request  string          `json:"request_id"`
	Response json.RawMessage `json:"response"`
}

type webConnection struct {
	client   *cr.Client
	process  processIdentity
	account  string
	revision string
	mu       sync.Mutex
	pending  map[string]cr.Message
}

type WebChat struct {
	ctx         context.Context
	cancel      context.CancelFunc
	c           config.Config
	mu          sync.Mutex
	connections map[string]*webConnection
}

func NewWebChat(ctx context.Context, c config.Config) *WebChat {
	ctx, cancel := context.WithCancel(ctx)
	return &WebChat{ctx: ctx, cancel: cancel, c: c, connections: map[string]*webConnection{}}
}

func (w *WebChat) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cancel()
	for _, connection := range w.connections {
		connection.client.Close()
		select {
		case <-connection.client.ProcessDone:
		case <-time.After(3 * time.Second):
			// Only a verified server started by this WebChat can be signalled.
			if sameProcess(context.Background(), connection.process) {
				_ = syscall.Kill(connection.process.PID, syscall.SIGTERM)
			}
		}
	}
}

func (w *WebChat) connection(ctx context.Context, account string) (*webConnection, error) {
	if account == "" {
		account = "default"
	}
	if account != "default" {
		if err := accounts.ValidateNativeAccount(w.c, account); err != nil {
			return nil, err
		}
		for _, removed := range w.c.AccountRegistry.Removed {
			if account == removed {
				return nil, errors.New("selected account was removed")
			}
		}
		if err := cr.EnsureAccountHome(w.c, account); err != nil {
			return nil, err
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	identity := accounts.InspectIdentity(w.c, account)
	if connection := w.connections[account]; connection != nil {
		select {
		case <-connection.client.Done:
			return nil, errors.New("official server disconnected; inspect unfinished work before restarting the web service")
		default:
		}
		if connection.revision != identity.Revision {
			return nil, errors.New("saved login changed; the running web server keeps its original authentication; finish its work before restarting")
		}
		return connection, nil
	}
	r, err := Resolver(w.c)
	if err != nil {
		return nil, err
	}
	shared, _ := json.Marshal(config.Expand(w.c.Runtime.SharedCodexHome))
	cmd := exec.Command(r.Binary, "--no-daemon", "-c", "sqlite_home="+string(shared), "-c", `cli_auth_credentials_store="file"`, "app-server", "--listen", "stdio://")
	cmd.Env = environment(w.c, account)
	cmd.Dir = config.Expand(w.c.Runtime.SharedCodexHome)
	if err := os.MkdirAll(cmd.Dir, 0700); err != nil {
		return nil, err
	}
	initCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client, err := cr.StartStdio(initCtx, cmd)
	if err != nil {
		return nil, err
	}
	process, err := inspectProcess(ctx, cmd.Process.Pid)
	if err != nil {
		client.Close()
		return nil, err
	}
	connection := &webConnection{client: client, process: process, account: account, revision: identity.Revision, pending: map[string]cr.Message{}}
	w.connections[account] = connection
	go connection.events(w.ctx)
	return connection, nil
}

func (c *webConnection) events(ctx context.Context) {
	for {
		select {
		case event := <-c.client.Events:
			if event.Method == "serverRequest/resolved" {
				var resolved struct {
					ID json.RawMessage `json:"requestId"`
				}
				if json.Unmarshal(event.Params, &resolved) == nil {
					c.mu.Lock()
					delete(c.pending, string(resolved.ID))
					c.mu.Unlock()
				}
			}
			if event.Method == "turn/completed" {
				var completed struct {
					Thread string `json:"threadId"`
				}
				if json.Unmarshal(event.Params, &completed) == nil {
					c.mu.Lock()
					for key, request := range c.pending {
						var p struct {
							Thread string `json:"threadId"`
						}
						if json.Unmarshal(request.Params, &p) == nil && p.Thread == completed.Thread {
							delete(c.pending, key)
						}
					}
					c.mu.Unlock()
				}
			}
			if len(event.ID) > 0 {
				c.mu.Lock()
				allowed := event.Method == "item/commandExecution/requestApproval" || event.Method == "item/fileChange/requestApproval" || event.Method == "item/tool/requestUserInput"
				if allowed && len(c.pending) < 64 {
					c.pending[string(event.ID)] = event
				} else {
					_ = c.client.Reject(event.ID, "request is unsupported or the pending approval limit was reached; no approval was granted")
				}
				c.mu.Unlock()
			}
		case <-c.client.Done:
			return
		case <-ctx.Done():
			return
		}
	}
}

// Compare the server's live user/workspace before model input. A nickname or
// default symlink is not proof of the authentication cached in a running server.
func (w *WebChat) verifyLogin(ctx context.Context, c *webConnection) error {
	identity := accounts.InspectIdentity(w.c, c.account)
	if identity.Revision != c.revision {
		return errors.New("saved login changed; finish existing web work before restarting with that account")
	}
	if identity.Key == "" {
		return nil
	} // API-key/custom providers have no ChatGPT claims.
	var live struct {
		Account *struct {
			Type  string `json:"type"`
			Email string `json:"email"`
		} `json:"account"`
		User    string `json:"userId"`
		Routing *struct {
			Workspace string `json:"chatgptAccountId"`
		} `json:"workspaceRouting"`
	}
	if err := c.client.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &live); err != nil {
		return err
	}
	if live.Account == nil || live.Account.Type != "chatgpt" || !strings.EqualFold(live.Account.Email, identity.Email) || live.User == "" || live.Routing == nil || live.Routing.Workspace == "" {
		return errors.New("the web server's live ChatGPT user/workspace could not be verified; no account was changed")
	}
	key := sha256.Sum256([]byte(live.User + "\x00" + live.Routing.Workspace))
	if hex.EncodeToString(key[:]) != identity.Key {
		return errors.New("the web server's live user/workspace differs from the selected login")
	}
	return nil
}

func webPageLimit(value, maximum, def int) (int, error) {
	if value == 0 {
		return def, nil
	}
	if value < 1 || value > maximum {
		return 0, fmt.Errorf("page limit must be between 1 and %d", maximum)
	}
	return value, nil
}

func (w *WebChat) Dispatch(ctx context.Context, r Request) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if r.Version != 1 {
		return nil, errors.New("unsupported bridge protocol")
	}
	var method *Method
	for i := range WebMethods {
		if WebMethods[i].Name == r.Method {
			method = &WebMethods[i]
			break
		}
	}
	if method == nil {
		return nil, errors.New("unsupported web chat method")
	}
	var p webParams
	if err := json.Unmarshal(r.Params, &p); err != nil {
		return nil, err
	}
	if p.Thread != "" && !threadID.MatchString(p.Thread) {
		return nil, errors.New("complete local thread UUID required")
	}
	if len(p.Cursor) > 4096 || len(p.Text) > 128*1024 || len(p.Name) > 200 || len(p.Search) > 500 || len(p.Turn) > 256 || len(p.Item) > 256 || len(p.Project) > 256 || len(p.Cwd) > 4096 || len(p.Request) > 256 || len(p.Response) > 128*1024 {
		return nil, errors.New("web chat input is too large")
	}
	call := func() (any, error) { return w.dispatch(ctx, r, p) }
	if !method.Mutation {
		return call()
	}
	return mutate(w.c, r, call, true)
}

func (w *WebChat) dispatch(ctx context.Context, r Request, p webParams) (any, error) {
	if r.Method == "web.catalog" {
		return w.catalog(ctx, nil, p)
	}
	connection, err := w.connection(ctx, p.Account)
	if err != nil {
		return nil, err
	}
	switch r.Method {
	case "web.catalog":
		return w.catalog(ctx, connection, p)
	case "web.history":
		return w.history(ctx, connection, p)
	case "web.items":
		return w.items(ctx, connection, p)
	case "web.item":
		return w.item(ctx, connection, p)
	case "web.pending":
		return w.pending(connection, p.Thread), nil
	case "web.respond":
		return w.respond(ctx, connection, p)
	}
	return w.change(ctx, connection, r, p)
}

func (w *WebChat) verifyOwner(ctx context.Context, c *webConnection, id string, require bool) error {
	if !sameProcess(ctx, c.process) {
		return errors.New("official web server process changed")
	}
	owners, err := writerProcesses(ctx, config.Expand(w.c.Runtime.SharedCodexHome), id)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(owners) == 0 && !require {
		return nil
	}
	if len(owners) != 1 || owners[0] != c.process.PID {
		return errors.New("this chat is owned by another GUI/CLI; release it in that window to use direct intervention; queued input remains available")
	}
	return nil
}

func (w *WebChat) prepare(ctx context.Context, c *webConnection, id string) error {
	if id == "" {
		return errors.New("thread UUID required")
	}
	if err := w.verifyOwner(ctx, c, id, false); err != nil {
		return err
	}
	// A new native thread has no rollout until its first turn. Loading it again
	// would fail and is unnecessary: the creating server already owns it.
	cursor := ""
	for {
		var loaded struct {
			Data       []string `json:"data"`
			NextCursor *string  `json:"nextCursor"`
		}
		args := map[string]any{"limit": 100}
		if cursor != "" {
			args["cursor"] = cursor
		}
		if err := c.client.Call(ctx, "thread/loaded/list", args, &loaded); err != nil {
			return err
		}
		for _, thread := range loaded.Data {
			if thread == id {
				return nil
			}
		}
		if loaded.NextCursor == nil || *loaded.NextCursor == "" {
			break
		}
		if *loaded.NextCursor == cursor {
			return errors.New("official loaded-thread cursor did not advance")
		}
		cursor = *loaded.NextCursor
	}
	var resumed struct{ Thread struct{ ID string } }
	if err := c.client.Call(ctx, "thread/resume", map[string]any{"threadId": id, "excludeTurns": true}, &resumed); err != nil {
		return err
	}
	if resumed.Thread.ID != id {
		return errors.New("official resume returned a different thread")
	}
	return w.verifyOwner(ctx, c, id, true)
}

func webDefaultCwd() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return string(os.PathSeparator)
	}
	if info, err := os.Stat(filepath.Join(home, "Codex")); err == nil && info.IsDir() {
		return filepath.Join(home, "Codex")
	}
	return home
}
