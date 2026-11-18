package nativecli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"slices"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

var errWebLoginDraining = errors.New("기존 작업과 대기 입력이 끝나면 웹 계정을 자동으로 갱신합니다. 새 작업은 잠시 뒤 보내 주세요.")

func (w *WebChat) connection(ctx context.Context, account string) (*webConnection, error) {
	if account == "" {
		account = "default"
	}
	if account != "default" {
		if err := accounts.ValidateNativeAccount(w.c, account); err != nil {
			return nil, err
		}
		if slices.Contains(w.c.AccountRegistry.Removed, account) {
			return nil, errors.New("selected account was removed")
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.ctx.Err(); err != nil {
		return nil, err
	}
	identity := accounts.InspectIdentity(w.c, account)
	if old := w.connections[account]; old != nil {
		changed := old.login.Revision != identity.Revision
		if !old.retiring && changed && old.inUse == 0 && !time.Now().Before(old.checkAfter) {
			old.checkAfter = time.Now().Add(time.Second)
			if old.drained(ctx) {
				old.retiring = true
				old.client.Close() // EOF only; this is a verified idle web-owned child.
			}
		}
		if !old.retiring {
			select {
			case <-old.client.Done:
				return nil, errors.New("official server disconnected; inspect unfinished web work")
			default:
				old.inUse++
				return old, nil
			}
		}
		select {
		case <-old.client.ProcessDone:
			delete(w.connections, account)
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
			return nil, errors.New("웹 계정을 갱신 중입니다. 잠시 뒤 다시 시도해 주세요.")
		}
		identity = accounts.InspectIdentity(w.c, account)
	}

	// Pin managed defaults to their existing named home. A switch of the native
	// symlink cannot make the old web child read or refresh a different login.
	runtimeAccount := account
	if account == "default" {
		if linked := accounts.LinkedNativeDefault(w.c); linked != "" {
			runtimeAccount = linked
		}
	}
	if runtimeAccount != "default" {
		if err := cr.EnsureAccountHome(w.c, runtimeAccount); err != nil {
			return nil, err
		}
	}
	r, err := Resolver(w.c)
	if err != nil {
		return nil, err
	}
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	shared, _ := json.Marshal(home)
	cmd := exec.Command(r.Binary, "--no-daemon", "-c", "sqlite_home="+string(shared), "-c", `cli_auth_credentials_store="file"`,
		"-c", "features.default_mode_request_user_input=true", "app-server", "--listen", "stdio://")
	cmd.Env, cmd.Dir = environment(w.c, runtimeAccount), home
	if err := os.MkdirAll(home, 0700); err != nil {
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
	if accounts.InspectIdentity(w.c, account).Revision != identity.Revision {
		client.Close()
		return nil, errors.New("연결하는 동안 저장된 계정이 바뀌었습니다. 다시 시도해 주세요.")
	}
	w.generation++
	c := &webConnection{client: client, process: process, account: account, login: identity,
		generation: w.generation, inUse: 1, pending: map[string]cr.Message{}}
	w.connections[account] = c
	go c.events(w.ctx)
	return c, nil
}

func (w *WebChat) releaseConnection(c *webConnection) {
	w.mu.Lock()
	c.inUse--
	w.mu.Unlock()
}

func (w *WebChat) loginChanged(c *webConnection) bool {
	return accounts.InspectIdentity(w.c, c.account).Revision != c.login.Revision
}

// Use authoritative server state rather than the browser, an idle event, or
// writer-file presence. Another loaded chat can still be active on this child.
func (c *webConnection) drained(ctx context.Context) bool {
	select {
	case <-c.client.ProcessDone:
		return true
	default:
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	c.mu.Lock()
	pending := len(c.pending)
	c.mu.Unlock()
	if pending != 0 {
		return false
	}
	loaded, err := webLoadedThreads(ctx, c.client)
	if err != nil {
		return false
	}
	for id := range loaded {
		var queue struct{ Data []json.RawMessage }
		err := c.client.Call(ctx, "thread/queue/list", map[string]any{"threadId": id, "limit": 1}, &queue)
		if err != nil || len(queue.Data) != 0 {
			return false
		}
		thread, err := c.client.Read(ctx, id, false)
		if err != nil || thread.ID != id || thread.Status.Type != "idle" {
			return false
		}
	}
	return true
}

func webLoadedThreads(ctx context.Context, client *cr.Client) (map[string]struct{}, error) {
	result := map[string]struct{}{}
	cursor := ""
	seen := map[string]bool{}
	for range 100 {
		var page struct {
			Data       []string
			NextCursor string
		}
		args := map[string]any{"limit": 100}
		if cursor != "" {
			args["cursor"] = cursor
		}
		if err := client.Call(ctx, "thread/loaded/list", args, &page); err != nil {
			return nil, err
		}
		for _, id := range page.Data {
			result[id] = struct{}{}
		}
		if page.NextCursor == "" {
			return result, nil
		}
		if seen[page.NextCursor] {
			return nil, errors.New("official loaded-thread cursor did not advance")
		}
		cursor = page.NextCursor
		seen[cursor] = true
	}
	return nil, errors.New("official loaded-thread listing exceeded the page limit")
}
