package nativecli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

// Connect to the existing official daemon, without starting or restarting one.
// Authentication remains owned by that process; the selected login is used only
// by the new CLI after the handoff. GUI-owned stdio servers are separate.
func archiveOnSharedDaemon(ctx context.Context, c config.Config, l Launch) (bool, error) {
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probe, l.Binary, "app-server", "daemon", "version")
	cmd.Env, cmd.Dir = environment(c, "default"), l.Cwd
	b, err := cmd.Output()
	if err != nil {
		return false, nil // No accessible daemon is not proof of ownership.
	}
	var info struct {
		Status string `json:"status"`
		Socket string `json:"socketPath"`
	}
	if json.Unmarshal(b, &info) != nil || info.Status != "running" ||
		info.Socket != filepath.Join(config.Expand(c.Runtime.SharedCodexHome), "app-server-control", "app-server-control.sock") {
		return false, nil
	}
	// The official path can be a symlink to a short private /tmp socket. Resolve
	// it before dialing so long CODEX_HOME paths fit the Unix address limit.
	socket, err := filepath.EvalSymlinks(info.Socket)
	if err != nil {
		return false, nil
	}
	st, err := os.Stat(socket)
	if err != nil || st.Mode()&os.ModeSocket == 0 {
		return false, nil
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return false, nil
	}
	// The control socket speaks WebSocket, not the JSON-lines stdio protocol.
	client, err := cr.DialUnix(probe, socket)
	if err != nil {
		return false, nil
	}
	defer client.Close()
	return archiveLoadedThread(ctx, client, l.Thread)
}

type handoffRPC interface {
	Call(context.Context, string, any, any) error
}

func archiveLoadedThread(ctx context.Context, client handoffRPC, thread string) (bool, error) {
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cursor := ""
	seen := map[string]bool{}
	for {
		var loaded struct {
			Data       []string `json:"data"`
			NextCursor string   `json:"nextCursor"`
		}
		params := map[string]any{"limit": 100}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := client.Call(probe, "thread/loaded/list", params, &loaded); err != nil {
			return false, nil
		}
		for _, id := range loaded.Data {
			if id != thread {
				continue
			}
			if err := client.Call(ctx, "thread/archive", map[string]any{"threadId": thread}, nil); err != nil {
				// The request may have reached the server. Do not silently retry it
				// through another process or claim that a new resume is safe.
				return false, fmt.Errorf("owning daemon archive did not complete; check this chat's state before retrying: %w", err)
			}
			return true, nil
		}
		cursor = loaded.NextCursor
		if cursor == "" || seen[cursor] {
			return false, nil
		}
		seen[cursor] = true
	}
}
