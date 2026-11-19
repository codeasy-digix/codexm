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
	{"web.models", false, false}, {"web.activity", false, false},
	{"web.thread", false, false}, {"web.usage", false, false},
	{"web.create", true, false}, {"web.send", true, true}, {"web.interrupt", true, false},
	{"web.queueUpdate", true, false}, {"web.queueCancel", true, false},
	{"web.pin", true, false}, {"web.move", true, false}, {"web.archive", true, false},
	{"web.delete", true, false}, {"web.projectCreate", true, false},
	{"web.pending", false, false}, {"web.respond", true, false},
	{"web.guiQuitPreview", false, false}, {"web.guiQuit", true, false},
	{"web.guiRelayStatus", false, false},
	{"web.completions", false, false},
	{"web.historyVersion", false, false},
	{"web.historySources", false, false},
	{"web.historyPage", false, false},
	{"web.dispatchQueued", false, false},
	{"web.catalogIndex", false, false},
	{"web.directories", false, false},
	{"web.skills", false, false},
	{"web.artifacts", false, false}, {"web.artifactRegister", true, false}, {"web.artifact", false, false},
	{"web.goalChange", true, true},
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
	Thread          string          `json:"thread_id"`
	Account         string          `json:"account"`
	Cursor          string          `json:"cursor"`
	Turn            string          `json:"turn_id"`
	Item            string          `json:"item_id"`
	Queued          string          `json:"queued_id"`
	ExpectedText    *string         `json:"expected_text"`
	Text            string          `json:"text"`
	Mode            string          `json:"mode"`
	Model           string          `json:"model"`
	Effort          string          `json:"effort"`
	Cwd             string          `json:"cwd"`
	Project         string          `json:"project_id"`
	Name            string          `json:"name"`
	Search          string          `json:"search"`
	Archived        bool            `json:"archived"`
	Pinned          bool            `json:"pinned"`
	Limit           int             `json:"limit"`
	Request         string          `json:"request_id"`
	Response        json.RawMessage `json:"response"`
	Confirmation    string          `json:"confirmation"`
	After           int64           `json:"after_ms"`
	HistoryRevision string          `json:"history_revision"`
	SkillIDs        []string        `json:"skills"`
	Goal            bool            `json:"goal"`
	SwitchAccount   bool            `json:"switch_account"`
	ExpectedGoal    *webGoalRef     `json:"expected_goal"`
	FilePath        string          `json:"file_path"`
	Artifact        string          `json:"artifact_id"`
	nativeInput     []map[string]any
	selectedSkills  []webSkill
}

type webConnection struct {
	client     *cr.Client
	process    processIdentity
	account    string
	loginName  string
	login      accounts.LoginIdentity
	generation uint64
	// The connection pool lock guards usage and retirement.
	inUse       int
	retiring    bool
	checkAfter  time.Time
	mu          sync.Mutex
	pending     map[string]cr.Message
	models      []webModel
	modelsAt    time.Time
	live        map[string]*webLiveEntry
	predictions map[string]webPrediction
}

type WebChat struct {
	ctx           context.Context
	cancel        context.CancelFunc
	c             config.Config
	webOnly       bool
	queueCursor   string // Accessed by the serial web bridge only.
	catalogExport *webCatalogExport
	mu            sync.Mutex
	connections   map[string]*webConnection
	generation    uint64
	writers       webWriterMonitor
	usageCache    webUsageCache
	guiQuit       webGUIQuit
}

func NewWebChat(ctx context.Context, c config.Config) *WebChat {
	ctx, cancel := context.WithCancel(ctx)
	return &WebChat{ctx: ctx, cancel: cancel, c: c, webOnly: os.Getenv("CODEXM_WEB_ONLY") == "true",
		connections: map[string]*webConnection{}}
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

func (c *webConnection) events(ctx context.Context) {
	for {
		select {
		case event := <-c.client.Events:
			c.observeLiveEvent(event)
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
	if w.loginChanged(c) {
		return errWebLoginDraining
	}
	return c.verifyExecutionLogin(ctx)
}

func (c *webConnection) verifyExecutionLogin(ctx context.Context) error {
	identity := c.login
	if identity.Key == "" {
		return nil
	} // API-key/custom providers have no ChatGPT claims.
	key, email, err := liveWebLogin(ctx, c.client)
	if err != nil {
		return err
	}
	if key == "" || !strings.EqualFold(email, identity.Email) {
		return errors.New("the web server's live ChatGPT user/workspace could not be verified; no account was changed")
	}
	if key != identity.Key {
		return errors.New("the web server's live user/workspace differs from the selected login")
	}
	return nil
}

func liveWebLogin(ctx context.Context, client *cr.Client) (string, string, error) {
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
	if err := client.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &live); err != nil {
		return "", "", err
	}
	if live.Account != nil && live.Account.Type == "chatgpt" && live.User != "" && live.Routing != nil && live.Routing.Workspace != "" {
		key := sha256.Sum256([]byte(live.User + "\x00" + live.Routing.Workspace))
		return hex.EncodeToString(key[:]), live.Account.Email, nil
	}
	return "", "", nil
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
	if len(p.Confirmation) > 128 {
		return nil, errors.New("invalid confirmation")
	}
	if len(p.Queued) > 256 || (p.ExpectedText != nil && len(*p.ExpectedText) > 128*1024) {
		return nil, errors.New("web queue input is too large")
	}
	if len(p.Cursor) > 4096 || len(p.Text) > 128*1024 || len(p.Name) > 200 || len(p.Search) > 500 || len(p.Turn) > 256 || len(p.Item) > 256 || len(p.Project) > 256 || len(p.Cwd) > 4096 || len(p.Request) > 256 || len(p.Response) > 128*1024 || len(p.Model) > 200 || len(p.Effort) > 64 || len(p.HistoryRevision) > 64 {
		return nil, errors.New("web chat input is too large")
	}
	call := func() (any, error) { return w.dispatch(ctx, r, p) }
	if !method.Mutation {
		return call()
	}
	return mutate(w.c, r, call, true)
}

func (w *WebChat) dispatch(ctx context.Context, r Request, p webParams) (any, error) {
	switch r.Method {
	case "web.catalog":
		return w.catalog(ctx, nil, p)
	case "web.catalogIndex":
		return w.catalogIndex(ctx, p)
	case "web.directories":
		return webDirectories(p.Cwd)
	case "web.activity":
		return w.activity(p.Thread, p.Account), nil
	case "web.thread":
		return w.thread(ctx, p.Thread)
	case "web.usage":
		return w.usage(p.Account)
	case "web.artifacts", "web.artifactRegister", "web.artifact":
		return w.artifacts(ctx, r.Method, p)
	case "web.guiQuitPreview":
		return w.guiQuit.preview(ctx, r.Owner, findWebDesktop)
	case "web.guiQuit":
		err := w.guiQuit.quit(ctx, r.Owner, p.Confirmation, findWebDesktop, stopWebDesktop)
		return err == nil, err
	case "web.guiRelayStatus":
		return webGUIRelayStatus(w.c, r.Owner, p.Thread, p.Request)
	case "web.completions":
		return w.readCompletions(ctx, p.After)
	case "web.historyVersion":
		return w.historyVersion(ctx, p.Thread)
	case "web.historySources":
		return w.historySources(ctx)
	case "web.dispatchQueued":
		return true, w.dispatchQueued(ctx)
	}
	connection, err := w.connection(ctx, p.Account)
	if err != nil {
		return nil, err
	}
	defer w.releaseConnection(connection)
	switch r.Method {
	case "web.models":
		return connection.availableModels(ctx)
	case "web.skills":
		return w.listPromptSkills(ctx, connection, p.Thread)
	case "web.history":
		return w.history(ctx, connection, p)
	case "web.historyPage":
		return w.readHistoryPage(ctx, connection, p)
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
	owners, err := webHandoffWriters(ctx, config.Expand(w.c.Runtime.SharedCodexHome), id)
	if err != nil {
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
	loaded, err := webLoadedThreads(ctx, c.client)
	if err != nil {
		return err
	}
	if _, found := loaded[id]; found {
		return nil
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
