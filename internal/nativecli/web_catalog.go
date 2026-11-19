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
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
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
		defer w.releaseConnection(connection)
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

type webCatalogRow struct {
	Thread        WebThread `json:"thread"`
	At            int64     `json:"at"`
	NativeProject string    `json:"nativeProject,omitempty"`
	Position      int64     `json:"position"`
}

type webCatalogMetadata struct {
	Projects         []WebProject `json:"projects"`
	OtherProjects    []WebProject `json:"otherProjects"`
	Accounts         []Account    `json:"accounts"`
	DefaultAccount   string       `json:"defaultAccount"`
	DefaultCwd       string       `json:"defaultCwd"`
	GUIQuitAvailable bool         `json:"guiQuitAvailable"`
}

type webCatalogExport struct {
	revision string
	rows     []webCatalogRow
	meta     webCatalogMetadata
}

func (w *WebChat) readCatalog(ctx context.Context) (*webCatalogExport, error) {
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
	// Use one complete native index, independent of the selected login. UpdatedAt
	// includes native recency when available, and falls back to modification time.
	rows := make([]webCatalogRow, 0, len(list.Sessions))
	for _, e := range list.Sessions {
		if e.Source == "subagent" || e.AttachedParent != "" {
			continue
		}
		if !desktop.IsPinned(e) && ((e.EmptyDraft() && desktop.Titles[e.ID] == "") || (e.Source != "cli" && e.Source != "vscode")) {
			continue
		}
		rows = append(rows, webCatalogRow{Thread: catalogWebThread(e, desktop, prefs),
			At: e.UpdatedAt, NativeProject: e.ProjectID, Position: e.SectionPosition})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].At != rows[j].At {
			return rows[i].At > rows[j].At
		}
		return rows[i].Thread.ID < rows[j].Thread.ID
	})
	accountList, err := Accounts(w.c)
	if err != nil {
		return nil, err
	}
	meta := webCatalogMetadata{Projects: desktop.Projects, OtherProjects: desktop.AdditionalProjects,
		Accounts: accountList, DefaultAccount: webDefaultAccount(w.c, accountList),
		GUIQuitAvailable: !w.webOnly && runtime.GOOS == "darwin", DefaultCwd: webDefaultCwd()}
	return &webCatalogExport{revision: strconv.FormatInt(time.Now().UnixNano(), 36), rows: rows, meta: meta}, nil
}

// Capture a consistent list once, then send bounded batches over the serial
// bridge. No browser query or account choice influences this cached snapshot.
func (w *WebChat) catalogIndex(ctx context.Context, p webParams) (any, error) {
	start := 0
	if p.Cursor == "" {
		index, err := w.readCatalog(ctx)
		if err != nil {
			return nil, err
		}
		w.catalogExport = index
	} else {
		revision, offset, ok := strings.Cut(p.Cursor, ".")
		n, err := strconv.Atoi(offset)
		if !ok || err != nil || w.catalogExport == nil || revision != w.catalogExport.revision ||
			n <= 0 || n >= len(w.catalogExport.rows) {
			return nil, errors.New("catalog export cursor expired")
		}
		start = n
	}
	index := w.catalogExport
	end := min(start+200, len(index.rows))
	result := map[string]any{"revision": index.revision, "rows": index.rows[start:end]}
	if start == 0 {
		result["meta"] = index.meta
	}
	if end < len(index.rows) {
		result["nextCursor"] = index.revision + "." + strconv.Itoa(end)
	} else {
		w.catalogExport = nil
	}
	return result, nil
}

func (w *WebChat) catalog(ctx context.Context, _ *webConnection, p webParams) (any, error) {
	limit, err := webPageLimit(p.Limit, 100, 50)
	if err != nil {
		return nil, err
	}
	index, err := w.readCatalog(ctx)
	if err != nil {
		return nil, err
	}
	items, pins := []webCatalogRow{}, []webCatalogRow{}
	search := strings.ToLower(strings.TrimSpace(p.Search))
	for _, row := range index.rows {
		t := row.Thread
		if t.Pinned && !t.Archived {
			pins = append(pins, row)
		}
		if t.Archived != p.Archived || (p.Project != "" && t.Project != p.Project && row.NativeProject != p.Project) || (p.Cwd != "" && t.Cwd != p.Cwd) {
			continue
		}
		if search == "" || strings.Contains(strings.ToLower(t.Name+" "+t.Preview), search) {
			items = append(items, row)
		}
	}
	sort.SliceStable(pins, func(i, j int) bool {
		if pins[i].Position != pins[j].Position {
			return pins[i].Position < pins[j].Position
		}
		return pins[i].Thread.ID < pins[j].Thread.ID
	})
	key, _ := json.Marshal([]any{p.Project, p.Cwd, p.Search, p.Archived, "recency_at"})
	query := fmt.Sprintf("%x", sha256.Sum256(key))
	start := 0
	if p.Cursor != "" {
		b, e := base64.RawURLEncoding.DecodeString(p.Cursor)
		var cursor webCatalogCursor
		if e != nil || json.Unmarshal(b, &cursor) != nil || cursor.Version != 1 || cursor.Query != query || !threadID.MatchString(cursor.ID) {
			return nil, errors.New("catalog cursor does not match this view; refresh the list")
		}
		start = sort.Search(len(items), func(i int) bool {
			row := items[i]
			return row.At < cursor.At || (row.At == cursor.At && row.Thread.ID > cursor.ID)
		})
	}
	end := min(start+limit, len(items))
	threads := make([]WebThread, 0, end-start)
	for _, row := range items[start:end] {
		threads = append(threads, row.Thread)
	}
	var next *string
	if end < len(items) {
		last := items[end-1]
		b, _ := json.Marshal(webCatalogCursor{1, query, last.At, last.Thread.ID})
		s := base64.RawURLEncoding.EncodeToString(b)
		next = &s
	}
	pinned := make([]WebThread, 0, len(pins))
	for _, row := range pins {
		pinned = append(pinned, row.Thread)
	}
	return map[string]any{"threads": threads, "pinned": pinned, "nextCursor": next, "total": len(items),
		"projects": index.meta.Projects, "otherProjects": index.meta.OtherProjects, "accounts": index.meta.Accounts,
		"defaultAccount": index.meta.DefaultAccount, "defaultCwd": index.meta.DefaultCwd,
		"guiQuitAvailable": index.meta.GUIQuitAvailable}, nil
}

func webDefaultAccount(c config.Config, registered []Account) string {
	for _, account := range registered {
		if account.Default {
			return account.Name
		}
	}
	if name := accounts.LinkedNativeDefault(c); name != "" {
		return name
	}
	return accounts.InspectIdentity(c, "default").Email
}
