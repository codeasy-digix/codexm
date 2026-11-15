package nativecli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/codeasy-digix/codexm/internal/config"
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

type WebProject struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Roots []struct {
		Path string `json:"path"`
	} `json:"roots"`
}

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

func (w *WebChat) pin(id string, value bool) (any, error) {
	if !threadID.MatchString(id) {
		return nil, errors.New("complete thread UUID required")
	}
	err := w.editPreferences(func(p *webPreferences) error {
		p.Pinned = slices.DeleteFunc(p.Pinned, func(x string) bool { return x == id })
		if value {
			if len(p.Pinned) >= 200 {
				return errors.New("at most 200 web pins are supported; unpin a chat first")
			}
			p.Pinned = append(p.Pinned, id)
		}
		return nil
	})
	return map[string]any{"thread_id": id, "pinned": value}, err
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

func (w *WebChat) catalog(ctx context.Context, c *webConnection, p webParams) (any, error) {
	limit, err := webPageLimit(p.Limit, 100, 50)
	if err != nil {
		return nil, err
	}
	params := map[string]any{"limit": limit, "sortKey": "recency_at", "sortDirection": "desc", "useStateDbOnly": true, "archived": p.Archived}
	if p.Search != "" {
		params["searchTerm"] = p.Search
	}
	if p.Cursor != "" {
		params["cursor"] = p.Cursor
	}
	if p.Project != "" {
		params["projectId"] = p.Project
	}
	if p.Cwd != "" {
		params["cwd"] = p.Cwd
	}
	var page struct {
		Data       []WebThread `json:"data"`
		NextCursor *string     `json:"nextCursor"`
	}
	if err := c.client.Call(ctx, "thread/list", params, &page); err != nil {
		return nil, err
	}
	prefs, err := w.preferences()
	if err != nil {
		return nil, err
	}
	metadata := map[string]bool{}
	lastAccounts := map[string]string{}
	if list, e := List(ctx, w.c); e == nil {
		for _, entry := range list.Sessions {
			metadata[entry.ID] = entry.Archived
			lastAccounts[entry.ID] = entry.Account
		}
	}
	for i := range page.Data {
		page.Data[i].Pinned = slices.Contains(prefs.Pinned, page.Data[i].ID)
		page.Data[i].Archived = p.Archived
		page.Data[i].Account = lastAccounts[page.Data[i].ID]
		if account := prefs.Accounts[page.Data[i].ID]; account != "" {
			page.Data[i].Account = account
		}
	}
	// Pinned is a small local presentation preference, independent of GUI and
	// account-server settings. It never modifies another client's global state.
	pinned := []WebThread{}
	if p.Cursor == "" {
		for _, id := range prefs.Pinned {
			if metadata[id] {
				continue
			}
			var r struct {
				Thread WebThread `json:"thread"`
			}
			if err := c.client.Call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &r); err == nil {
				r.Thread.Pinned = true
				r.Thread.Account = lastAccounts[id]
				if account := prefs.Accounts[id]; account != "" {
					r.Thread.Account = account
				}
				pinned = append(pinned, r.Thread)
			}
		}
	}
	projects := []WebProject{}
	if p.Cursor == "" {
		cursor := ""
		for n := 0; n < 10; n++ {
			var r struct {
				Data       []WebProject `json:"data"`
				NextCursor *string      `json:"nextCursor"`
			}
			args := map[string]any{"limit": 100}
			if cursor != "" {
				args["cursor"] = cursor
			}
			if err := c.client.Call(ctx, "project/list", args, &r); err != nil {
				return nil, err
			}
			projects = append(projects, r.Data...)
			if r.NextCursor == nil || *r.NextCursor == "" {
				break
			}
			cursor = *r.NextCursor
		}
	}
	accountList, err := Accounts(w.c)
	if err != nil {
		return nil, err
	}
	return map[string]any{"threads": page.Data, "nextCursor": page.NextCursor, "projects": projects, "pinned": pinned, "accounts": accountList, "defaultCwd": webDefaultCwd()}, nil
}
