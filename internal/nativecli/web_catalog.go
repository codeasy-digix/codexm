package nativecli

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

type WebThread struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Preview string `json:"preview"`
	Cwd     string `json:"cwd"`
	Project string `json:"projectId"`
	Model   string `json:"model"`
	Effort  string `json:"reasoningEffort"`
	Updated int64  `json:"updatedAt"`
	Recency int64  `json:"recencyAt"`
	Status  struct {
		Type string `json:"type"`
	} `json:"status"`
	Pinned   bool   `json:"pinned"`
	Archived bool   `json:"archived"`
	External bool   `json:"externalOwner"`
	Account  string `json:"account,omitempty"`
}

type WebProject = catalog.DesktopProject

type webPreferences struct {
	Pinned   []string          `json:"pinned"`
	Accounts map[string]string `json:"last_accounts,omitempty"`
}

func (w *WebChat) preferences() (webPreferences, error) {
	p := webPreferences{Pinned: []string{}}
	b, err := os.ReadFile(filepath.Join(config.Expand(w.c.Storage.StateDir), "web-chat", "preferences.json"))
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err = json.Unmarshal(b, &p); err != nil {
		return p, err
	}
	return p, nil
}

func (w *WebChat) rememberAccount(id, account string) {
	// This is presentation metadata after an acknowledged native action. A
	// metadata failure must not change that action's outcome or replay its input.
	_ = w.editPreferences(func(p *webPreferences) error {
		if p.Accounts == nil {
			p.Accounts = map[string]string{}
		}
		p.Accounts[id] = account
		return nil
	})
}

func (w *WebChat) editPreferences(edit func(*webPreferences) error) error {
	dir := filepath.Join(config.Expand(w.c.Storage.StateDir), "web-chat")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	fd, err := syscall.Open(filepath.Join(dir, "preferences.lock"), syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(fd, syscall.LOCK_UN) }()
	p, err := w.preferences()
	if err != nil {
		return err
	}
	if err = edit(&p); err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return config.AtomicWrite(filepath.Join(dir, "preferences.json"), b, 0600)
}

type webCatalogCursor struct {
	Version int    `json:"v"`
	Query   string `json:"q"`
	At      int64  `json:"at"`
	ID      string `json:"id"`
}

func catalogWebThread(e catalog.Entry, desktop catalog.Desktop, prefs webPreferences) WebThread {
	t := WebThread{ID: e.ID, Name: desktop.Title(e), Preview: e.Preview, Cwd: e.Cwd, Project: desktop.ProjectFor(e), Model: e.Model, Effort: e.Effort, Updated: e.ModifiedAt / 1000, Recency: e.UpdatedAt / 1000, Pinned: desktop.IsPinned(e), Archived: e.Archived, Account: e.Account}
	t.Status.Type = "notLoaded"
	if account := prefs.Accounts[e.ID]; account != "" {
		t.Account = account
	}
	return t
}

// Look up one address without depending on the current sidebar page. An unsent
// draft can only be present in the web server that originally created it.
func (w *WebChat) thread(ctx context.Context, id string) (*WebThread, error) {
	if id == "" {
		return nil, errors.New("thread UUID required")
	}
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	entry, err := catalog.Lookup(ctx, home, id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	prefs, preferencesErr := w.preferences()
	if preferencesErr != nil {
		return nil, preferencesErr
	}
	if errors.Is(err, sql.ErrNoRows) {
		account := prefs.Accounts[id]
		if account == "" {
			return nil, nil
		}
		w.mu.Lock()
		existing := w.connections[account] != nil
		w.mu.Unlock()
		if !existing {
			return nil, nil
		}
		connection, err := w.connection(ctx, account)
		if err != nil {
			return nil, err
		}
		var draft struct{ Thread WebThread }
		if err := connection.client.Call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &draft); err != nil {
			var rpc *cr.RPCError
			if errors.As(err, &rpc) && rpc.Code == -32600 {
				return nil, nil
			}
			return nil, err
		}
		if draft.Thread.ID != id {
			return nil, errors.New("Codex returned a different chat")
		}
		draft.Thread.Account = account
		return &draft.Thread, nil
	}
	desktop, err := catalog.ReadDesktop(ctx, home)
	if err != nil {
		return nil, err
	}
	entries := []catalog.Entry{entry}
	fillCatalogAccounts(w.c, entries)
	t := catalogWebThread(entries[0], desktop, prefs)
	if t.Account != "" && t.Account != "default" && requireRegisteredAccount(w.c, t.Account) != nil {
		t.Account = ""
	}
	return &t, nil
}

func (w *WebChat) catalog(ctx context.Context, _ *webConnection, p webParams) (any, error) {
	limit, err := webPageLimit(p.Limit, 100, 50)
	if err != nil {
		return nil, err
	}
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	list, err := List(ctx, w.c)
	if err != nil {
		return nil, err
	}
	desktop, err := catalog.ReadDesktop(ctx, home)
	if err != nil {
		return nil, err
	}
	prefs, err := w.preferences()
	if err != nil {
		return nil, err
	}
	// Use one complete read-only native index, independent of the selected login.
	// Official recency cursors can stall on older paginated rows; never scan and
	// repair rollouts, resume a chat, or interpret a failed page as an empty list.
	type item struct {
		thread   WebThread
		at       int64
		position int64
	}
	items := []item{}
	pins := []item{}
	search := strings.ToLower(strings.TrimSpace(p.Search))
	for _, e := range list.Sessions {
		if e.Source == "subagent" || e.AttachedParent != "" {
			continue
		}
		pinned := desktop.IsPinned(e)
		if !pinned && ((e.EmptyDraft() && desktop.Titles[e.ID] == "") || (e.Source != "cli" && e.Source != "vscode")) {
			continue
		}
		t := catalogWebThread(e, desktop, prefs)
		at := e.ModifiedAt
		if desktop.ChatSort == "created_at" {
			at = e.CreatedAt
		}
		if desktop.ChatSort == "recency_at" {
			at = e.UpdatedAt
		}
		i := item{t, at, e.SectionPosition}
		if t.Pinned && !t.Archived {
			pins = append(pins, i)
		}
		if t.Archived != p.Archived || (p.Project != "" && t.Project != p.Project && e.ProjectID != p.Project) || (p.Cwd != "" && t.Cwd != p.Cwd) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(t.Name+" "+t.Preview), search) {
			continue
		}
		items = append(items, i)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].at != items[j].at {
			return items[i].at > items[j].at
		}
		return items[i].thread.ID < items[j].thread.ID
	})
	sort.SliceStable(pins, func(i, j int) bool {
		if pins[i].position != pins[j].position {
			return pins[i].position < pins[j].position
		}
		return pins[i].thread.ID < pins[j].thread.ID
	})
	key, _ := json.Marshal([]any{p.Project, p.Cwd, p.Search, p.Archived, desktop.ChatSort})
	query := fmt.Sprintf("%x", sha256.Sum256(key))
	start := 0
	if p.Cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(p.Cursor)
		var cursor webCatalogCursor
		if e != nil || json.Unmarshal(b, &cursor) != nil || cursor.Version != 1 || cursor.Query != query || !threadID.MatchString(cursor.ID) {
			return nil, errors.New("catalog cursor does not match this view; refresh the list")
		}
		start = len(items)
		for i, it := range items {
			if it.at < cursor.At || (it.at == cursor.At && it.thread.ID > cursor.ID) {
				start = i
				break
			}
		}
	}
	end := min(start+limit, len(items))
	threads := []WebThread{}
	for _, it := range items[start:end] {
		threads = append(threads, it.thread)
	}
	var next *string
	if end < len(items) {
		last := items[end-1]
		b, _ := json.Marshal(webCatalogCursor{1, query, last.at, last.thread.ID})
		s := base64.RawURLEncoding.EncodeToString(b)
		next = &s
	}
	pinned := []WebThread{}
	for _, it := range pins {
		pinned = append(pinned, it.thread)
	}
	accountList, err := Accounts(w.c)
	if err != nil {
		return nil, err
	}
	return map[string]any{"threads": threads, "nextCursor": next, "projects": desktop.Projects, "otherProjects": desktop.AdditionalProjects, "pinned": pinned, "accounts": accountList, "defaultCwd": webDefaultCwd(), "total": len(items)}, nil
}
