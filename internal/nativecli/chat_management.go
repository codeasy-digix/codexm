package nativecli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

type ChatArchiveResult struct {
	Thread   string `json:"thread_id"`
	Archived bool   `json:"archived"`
	Changed  bool   `json:"changed"`
}

// A selected catalog row can be archived/restored without resuming it, logging
// in, or requiring its project folder to still exist. Only official operations
// change native metadata; an inaccessible writer is never terminated here.
func SetChatArchived(ctx context.Context, c config.Config, id string, archived bool) (ChatArchiveResult, error) {
	result := ChatArchiveResult{Thread: id, Archived: archived}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	unlock, err := lockChatMutation(ctx, c, id)
	if err != nil {
		return result, err
	}
	defer unlock()
	current, err := archivedState(ctx, c, id)
	if err != nil {
		return result, err
	}
	if current == archived {
		return result, nil
	}
	r, err := Resolver(c)
	if err != nil {
		return result, err
	}
	shared := config.Expand(c.Runtime.SharedCodexHome)
	command := "unarchive"
	if archived {
		command = "archive"
	}
	done := false
	if cr.WriterBusy(shared, id) {
		if archived {
			// A confirmed archive may interrupt exactly this thread through its
			// existing owner. Failed/uncertain RPCs are never retried via the CLI.
			done, err = archiveOnSharedDaemon(ctx, c, Launch{Thread: id, Binary: r.Binary, Cwd: shared, Account: "default"})
			if err != nil {
				return result, err
			}
		}
		if done {
			wait, cancel := context.WithTimeout(ctx, 5*time.Second)
			err = cr.WaitWriterRelease(wait, shared, id)
			cancel()
			if err != nil {
				return result, errors.New("archive was requested but its writer is still busy; refresh the list before retrying")
			}
		} else if cr.WriterBusy(shared, id) {
			return result, errors.New("this chat is owned by another GUI/CLI; archive or close it in its owning window, then refresh and retry. No GUI or CLI was stopped")
		}
	}
	if !done {
		cmd := exec.CommandContext(ctx, r.Binary, "--no-daemon", command, id)
		cmd.Dir, cmd.Env = shared, environment(c, "default")
		out, e := cmd.CombinedOutput()
		if e != nil {
			return result, fmt.Errorf("official %s did not complete; refresh the list before retrying: %w: %s", command, e, cr.SafeText(strings.TrimSpace(string(out))))
		}
	}
	actual, err := archivedState(ctx, c, id)
	if err != nil || actual != archived {
		return result, fmt.Errorf("official %s returned but the new chat state could not be confirmed; refresh the list before retrying", command)
	}
	result.Changed = true
	return result, nil
}
