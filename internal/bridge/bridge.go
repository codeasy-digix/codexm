// Package bridge is the versioned, bounded stdio transport used by Meteor.
package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/codeasy-digix/codexm/internal/application"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"golang.org/x/sys/unix"
)

type Request struct {
	Version     int             `json:"protocolVersion"`
	ID          string          `json:"id"`
	Method      string          `json:"method"`
	Owner       string          `json:"owner"`
	OperationID string          `json:"operation_id,omitempty"`
	Params      json.RawMessage `json:"params"`
}
type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}
type Response struct {
	Version int    `json:"protocolVersion"`
	ID      string `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
	Method  string `json:"method,omitempty"`
	Params  any    `json:"params,omitempty"`
}

func classify(err error) *Error {
	message := cr.SafeText(err.Error())
	code := "request_failed"
	for _, c := range []string{"invalid_input", "authentication_required", "version_mismatch", "operation_conflict", "outcome_unknown", "login_expired", "service_unavailable", "unsupported", "busy"} {
		if strings.Contains(message, c+":") {
			code = c
			break
		}
	}
	if strings.Contains(message, "revision") || strings.Contains(message, "changed since") || strings.Contains(message, "settings changed") {
		code = "settings_conflict"
	}
	if strings.Contains(message, "external") || strings.Contains(message, "writer") {
		code = "external_writer"
	}
	return &Error{code, message, code == "service_unavailable" || code == "busy"}
}

func Run(ctx context.Context, c config.Config, self string, in io.Reader, out io.Writer) error {
	if err := os.MkdirAll(c.StateDir(), 0700); err != nil {
		return err
	}
	fd, err := unix.Open(filepath.Join(c.StateDir(), "web-bridge.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	lock := os.NewFile(uintptr(fd), "bridge lock")
	defer lock.Close()
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.New("another web bridge owns this state directory")
	}
	app := application.New(ctx, c, self)
	defer app.Close()
	return Serve(ctx, app, in, out)
}

func Serve(ctx context.Context, app *application.App, in io.Reader, out io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	finished := make(chan struct{})
	defer close(finished)
	if closer, ok := in.(io.Closer); ok {
		go func() {
			select {
			case <-ctx.Done():
				_ = closer.Close()
			case <-finished:
			}
		}()
	}
	if closer, ok := out.(io.Closer); ok {
		go func() {
			select {
			case <-ctx.Done():
				_ = closer.Close()
			case <-finished:
			}
		}()
	}
	queue := make(chan Response, 64)
	writerDone := make(chan error, 1)
	go func() {
		enc := json.NewEncoder(out)
		for {
			select {
			case <-ctx.Done():
				writerDone <- ctx.Err()
				return
			case reply, ok := <-queue:
				if !ok {
					writerDone <- nil
					return
				}
				if err := enc.Encode(reply); err != nil {
					cancel()
					writerDone <- err
					return
				}
			}
		}
	}()
	emit := func(reply Response) {
		reply.Version = application.ProtocolVersion
		select {
		case queue <- reply:
		case <-ctx.Done():
		default:
			cancel()
		}
	}
	var workers, watchers sync.WaitGroup
	limit := make(chan struct{}, 16)
	var watchCancel context.CancelFunc
	var watchMu sync.Mutex
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var req Request
		if json.Unmarshal(scanner.Bytes(), &req) != nil || req.ID == "" || len(req.ID) > 128 {
			emit(Response{Error: &Error{Code: "invalid_input", Message: "invalid bridge request"}})
			continue
		}
		if req.Version != application.ProtocolVersion {
			emit(Response{ID: req.ID, Error: &Error{Code: "version_mismatch", Message: "web and bridge protocol versions differ"}})
			continue
		}
		select {
		case limit <- struct{}{}:
		case <-ctx.Done():
			break
		}
		if ctx.Err() != nil {
			break
		}
		workers.Add(1)
		go func(req Request) {
			defer workers.Done()
			defer func() { <-limit }()
			call, done := context.WithTimeout(ctx, 120*time.Second)
			defer done()
			if req.Method == "watch" {
				watchMu.Lock()
				defer watchMu.Unlock()
				if watchCancel != nil {
					watchCancel()
				}
				watchCtx, stop := context.WithCancel(ctx)
				watchCancel = stop
				watchers.Add(1)
				go func() { defer watchers.Done(); watch(watchCtx, app, req.Params, emit) }()
				emit(Response{ID: req.ID, Result: true})
				return
			}
			result, err := app.Call(call, req.Owner, req.Method, req.OperationID, req.Params)
			reply := Response{ID: req.ID, Result: result}
			if err != nil {
				reply.Result = nil
				reply.Error = classify(err)
			}
			emit(reply)
		}(req)
	}
	workers.Wait()
	watchMu.Lock()
	if watchCancel != nil {
		watchCancel()
	}
	watchMu.Unlock()
	app.Close()
	watchers.Wait()
	close(queue)
	select {
	case err := <-writerDone:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	return scanner.Err()
}
func watch(ctx context.Context, app *application.App, raw json.RawMessage, emit func(Response)) {
	var cursor struct {
		Instance string `json:"instance"`
		After    int64  `json:"after"`
	}
	if json.Unmarshal(raw, &cursor) != nil {
		emit(Response{Method: "resync", Params: map[string]string{"reason": "invalid cursor"}})
		return
	}
	timer := time.NewTicker(250 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		args, _ := json.Marshal(cursor)
		result, err := app.Call(ctx, "", "changes.replay", "", args)
		if err != nil {
			emit(Response{Method: "resync", Params: map[string]string{"reason": "service unavailable"}})
			return
		}
		b, _ := json.Marshal(result)
		var replay struct {
			Instance string `json:"instance"`
			Sequence int64  `json:"sequence"`
			Resync   bool   `json:"resync"`
			Entries  []struct {
				Sequence int64 `json:"sequence"`
			} `json:"entries"`
		}
		if json.Unmarshal(b, &replay) != nil {
			return
		}
		if replay.Resync {
			emit(Response{Method: "resync", Params: result})
			return
		}
		if len(replay.Entries) > 0 {
			emit(Response{Method: "changes", Params: result})
			cursor.After = replay.Entries[len(replay.Entries)-1].Sequence
		}
	}
}
