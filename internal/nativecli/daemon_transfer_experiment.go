//go:build gui_experiment

package nativecli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
)

type DaemonTransfer struct {
	Request   string    `json:"request_id"`
	Thread    string    `json:"thread_id"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Interrupt bool      `json:"interrupt"`
	State     string    `json:"state"`
	Updated   time.Time `json:"updated"`
}

func daemonTransferPath(c config.Config, request string) string {
	return filepath.Join(daemonRoot(c), "requests", request+".json")
}

func lockDaemonTransfers(c config.Config) (func(), error) {
	if err := os.MkdirAll(daemonRoot(c), 0700); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(filepath.Join(daemonRoot(c), "transfer.lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		syscall.Close(fd)
		return nil, errors.New("another daemon transfer is in progress; no request was sent")
	}
	return func() { _ = syscall.Flock(fd, syscall.LOCK_UN); _ = syscall.Close(fd) }, nil
}

func ReadDaemonTransfer(c config.Config, request string) (DaemonTransfer, error) {
	var r DaemonTransfer
	if !threadID.MatchString(request) {
		return r, errors.New("request ID must be a UUID")
	}
	b, err := os.ReadFile(daemonTransferPath(c, request))
	if err == nil {
		err = json.Unmarshal(b, &r)
	}
	return r, err
}

func saveDaemonTransfer(c config.Config, r *DaemonTransfer, state string) error {
	r.State, r.Updated = state, time.Now().UTC()
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return config.AtomicWrite(daemonTransferPath(c, r.Request), b, 0600)
}

func daemonTransferPreflight(ctx context.Context, client handoffRPC, id string) error {
	// Native archive includes descendants. Refuse both live and archived child
	// trees instead of letting a selected-chat operation affect other work.
	for _, archived := range []bool{false, true} {
		var r struct{ Data []json.RawMessage }
		if err := client.Call(ctx, "thread/list", map[string]any{"ancestorThreadId": id, "archived": archived, "limit": 1, "sourceKinds": []string{}, "useStateDbOnly": true}, &r); err != nil {
			return err
		}
		if len(r.Data) != 0 {
			return errors.New("chat has spawned descendants; native archive could affect them, so daemon transfer is refused")
		}
	}
	var queue struct{ Data []json.RawMessage }
	if err := client.Call(ctx, "thread/queue/list", map[string]any{"threadId": id, "limit": 1}, &queue); err != nil {
		return err
	}
	if len(queue.Data) != 0 {
		return errors.New("chat has queued input; finish or remove it in its owner before transferring")
	}
	return nil
}

// TransferDaemonChat never sends a prompt. It uses the verified source server's
// own archive RPC, waits for writer release, then restores/resumes the same ID on
// the verified destination. A partial/uncertain transaction is never replayed.
func TransferDaemonChat(ctx context.Context, c config.Config, request, id, from, to string, interrupt bool) (result DaemonTransfer, err error) {
	if !threadID.MatchString(id) || !threadID.MatchString(request) {
		return result, errors.New("complete thread and request UUIDs are required")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	unlockTransfers, err := lockDaemonTransfers(c)
	if err != nil {
		return result, err
	}
	defer unlockTransfers()
	unlock, err := lockChatMutation(ctx, c, id)
	if err != nil {
		return result, err
	}
	defer unlock()
	if previous, e := ReadDaemonTransfer(c, request); e == nil {
		if previous.Thread != id || previous.From != from || previous.To != to || previous.Interrupt != interrupt {
			return result, errors.New("request ID already belongs to different parameters")
		}
		return previous, nil // Inspection only, including unknown results.
	} else if !os.IsNotExist(e) {
		return result, e
	}
	// A new request ID must not disguise a retry of a partially executed transfer.
	guard := filepath.Join(daemonRoot(c), "chats", id+".json")
	if b, e := os.ReadFile(guard); e == nil {
		var previous string
		if json.Unmarshal(b, &previous) != nil {
			return result, errors.New("invalid daemon transfer guard")
		}
		r, e := ReadDaemonTransfer(c, previous)
		if e != nil || r.State != "completed" {
			return result, fmt.Errorf("previous transfer %s needs inspection; no automatic retry", previous)
		}
	} else if !os.IsNotExist(e) {
		return result, e
	}
	source, err := openDaemon(ctx, c, from, true)
	if err != nil {
		return result, err
	}
	defer source.client.Close()
	target, err := openDaemon(ctx, c, to, true)
	if err != nil {
		return result, err
	}
	defer target.client.Close()
	if source.info.Identity == target.info.Identity || source.info.Process.PID == target.info.Process.PID {
		return result, errors.New("source and destination must be different logins and official daemon processes")
	}
	if err = source.verify(ctx, c, id, true); err != nil {
		return result, err
	}
	if err = daemonTransferPreflight(ctx, source.client, id); err != nil {
		return result, err
	}
	thread, _, err := source.client.Recent(ctx, id, 1, "")
	if err != nil {
		return result, err
	}
	active := thread.Status.Type == "active"
	if active && !interrupt {
		return result, errors.New("selected chat is active; wait for it to finish or explicitly pass --interrupt to stop only this turn")
	}
	if !active && thread.Status.Type != "idle" {
		return result, errors.New("selected chat is not in a verified idle/active state")
	}
	turn := ""
	if active {
		if len(thread.Turns) != 1 || thread.Turns[0].Status != "inProgress" {
			return result, errors.New("cannot identify the active turn unambiguously")
		}
		turn = thread.Turns[0].ID
	}
	result = DaemonTransfer{Request: request, Thread: id, From: from, To: to, Interrupt: interrupt}
	if err = saveDaemonTransfer(c, &result, "prepared"); err != nil {
		return result, err
	}
	b, _ := json.Marshal(request)
	if err = config.AtomicWrite(guard, b, 0600); err != nil {
		return result, err
	}
	defer func() {
		if err != nil {
			if saveErr := saveDaemonTransfer(c, &result, "unknown"); saveErr != nil {
				err = errors.Join(err, saveErr)
			}
			err = fmt.Errorf("transfer %s is incomplete; inspect daemon request %s and native owners before any further action: %w", request, request, err)
		}
	}()
	if err = source.verify(ctx, c, id, true); err != nil {
		return result, err
	}
	if err = target.verify(ctx, c, "", false); err != nil {
		return result, err
	}
	if active {
		if err = saveDaemonTransfer(c, &result, "interrupting"); err != nil {
			return result, err
		}
		if err = source.client.Call(ctx, "turn/interrupt", map[string]any{"threadId": id, "turnId": turn}, nil); err != nil {
			return result, err
		}
		for {
			thread, err = source.client.Read(ctx, id, false)
			if err != nil {
				return result, err
			}
			if thread.Status.Type == "idle" {
				break
			}
			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	if err = daemonTransferPreflight(ctx, source.client, id); err != nil {
		return result, err
	}
	thread, err = source.client.Read(ctx, id, false)
	if err != nil {
		return result, err
	}
	if thread.Status.Type != "idle" {
		return result, errors.New("chat became active again; no archive sent")
	}
	if err = source.verify(ctx, c, id, true); err != nil {
		return result, err
	}
	if err = saveDaemonTransfer(c, &result, "archiving"); err != nil {
		return result, err
	}
	if err = source.client.Call(ctx, "thread/archive", map[string]any{"threadId": id}, nil); err != nil {
		return result, err
	}
	for {
		var pids []int
		pids, err = writerProcesses(ctx, config.Expand(c.Runtime.SharedCodexHome), id)
		if err != nil && !os.IsNotExist(err) {
			return result, err
		}
		if len(pids) == 0 {
			break
		}
		if len(pids) != 1 || pids[0] != source.info.Process.PID {
			return result, errors.New("a replacement writer appeared; it was preserved")
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	if err = target.verify(ctx, c, "", false); err != nil {
		return result, err
	}
	if err = saveDaemonTransfer(c, &result, "restoring"); err != nil {
		return result, err
	}
	if err = target.client.Call(ctx, "thread/unarchive", map[string]any{"threadId": id}, nil); err != nil {
		return result, err
	}
	if err = target.verify(ctx, c, id, false); err != nil {
		return result, err
	}
	if err = saveDaemonTransfer(c, &result, "resuming"); err != nil {
		return result, err
	}
	resumed, e := target.client.Resume(ctx, id)
	if e != nil {
		return result, e
	}
	if resumed.Thread.ID != id {
		return result, errors.New("official resume returned a different chat ID")
	}
	if err = target.verify(ctx, c, id, true); err != nil {
		return result, err
	}
	err = saveDaemonTransfer(c, &result, "completed")
	return result, err
}
