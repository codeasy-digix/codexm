package nativecli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/sys/unix"
)

type handoffCall struct {
	method string
	params map[string]any
}

func TestPrepareUsesExistingDaemonAndWaitsForRelease(t *testing.T) {
	c := fixture(t)
	l, err := Plan(context.Background(), c, fixtureID, "default")
	if err != nil {
		t.Fatal(err)
	}
	lockDir := filepath.Join(c.Runtime.SharedCodexHome, "thread-writer-locks")
	if err = os.MkdirAll(lockDir, 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(lockDir, fixtureID+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	// Keep the bound Unix path within macOS' socket-name length limit.
	short, err := os.MkdirTemp("", "cm-rpc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(short)
	listener, err := net.Listen("unix", filepath.Join(short, "sock"))
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(c.Runtime.SharedCodexHome, "app-server-control", "app-server-control.sock")
	if err = os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(listener.Addr().String(), socket); err != nil {
		t.Fatal(err)
	}
	info, _ := json.Marshal(map[string]string{"status": "running", "socketPath": socket})
	if err = os.WriteFile(filepath.Join(os.Getenv("HOME"), "daemon-version.json"), info, 0600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$HOME/commands\"\nif [ \"$1\" = app-server ]; then cat \"$HOME/daemon-version.json\"; fi\n"
	if err = os.WriteFile(l.Binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	archived := make(chan string, 1)
	upgrader := websocket.Upgrader{}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, e := upgrader.Upgrade(w, r, nil)
		if e != nil {
			return
		}
		defer conn.Close()
		for {
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params map[string]any  `json:"params"`
			}
			if conn.ReadJSON(&req) != nil {
				return
			}
			if len(req.ID) == 0 {
				continue
			}
			var result any = map[string]any{}
			switch req.Method {
			case "initialize":
			case "thread/loaded/list":
				result = map[string]any{"data": []string{fixtureID}}
			case "thread/archive":
				id, _ := req.Params["threadId"].(string)
				archived <- id
				if id == fixtureID {
					_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
				}
			default:
				return
			}
			if conn.WriteJSON(map[string]any{"id": req.ID, "result": result}) != nil {
				return
			}
		}
	})}
	defer server.Close()
	go server.Serve(listener)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = Prepare(ctx, c, l); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-archived:
		if id != fixtureID {
			t.Fatal("archived another chat")
		}
	default:
		t.Fatal("no owner archive")
	}
	commands, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), "commands"))
	if string(commands) != "app-server daemon version\n--no-daemon unarchive "+fixtureID+"\n" {
		t.Fatalf("unexpected CLI operations: %q", commands)
	}
}

type handoffFixture struct {
	pages []string
	err   error
	calls []handoffCall
}

func (f *handoffFixture) Call(_ context.Context, method string, params, out any) error {
	f.calls = append(f.calls, handoffCall{method, params.(map[string]any)})
	if method == "thread/archive" {
		return f.err
	}
	if method != "thread/loaded/list" {
		return errors.New("unexpected RPC mutation")
	}
	if len(f.pages) == 0 {
		return errors.New("list unavailable")
	}
	b := f.pages[0]
	f.pages = f.pages[1:]
	return json.Unmarshal([]byte(b), out)
}

func TestHandoffTargetsOnlyLoadedChatAndNeverReplaysArchive(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		pages                []string
		archiveErr           error
		wantArchive, wantErr bool
	}{
		{"target on second page", []string{`{"data":["other"],"nextCursor":"page2"}`, `{"data":["` + fixtureID + `"],"nextCursor":null}`}, nil, true, false},
		{"different owner", []string{`{"data":["other"]}`}, nil, false, false},
		{"list unavailable", nil, nil, false, false},
		{"cursor loop", []string{`{"data":[],"nextCursor":"same"}`, `{"data":[],"nextCursor":"same"}`}, nil, false, false},
		{"uncertain archive", []string{`{"data":["` + fixtureID + `"]}`}, errors.New("transport closed after request"), true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &handoffFixture{pages: tc.pages, err: tc.archiveErr}
			released, err := archiveLoadedThread(context.Background(), f, fixtureID)
			if (err != nil) != tc.wantErr || released != (tc.wantArchive && !tc.wantErr) {
				t.Fatalf("released=%v error=%v", released, err)
			}
			archives := 0
			for _, call := range f.calls {
				if call.method == "thread/archive" {
					archives++
					if !reflect.DeepEqual(call.params, map[string]any{"threadId": fixtureID}) {
						t.Fatal("wrong target", call)
					}
				}
			}
			want := 0
			if tc.wantArchive {
				want = 1
			}
			if archives != want {
				t.Fatal("archive omitted or replayed", f.calls)
			}
			if tc.name == "target on second page" && f.calls[1].params["cursor"] != "page2" {
				t.Fatal("pagination cursor was lost")
			}
		})
	}
}

func TestForeignWriterIsNotArchivedOrUnlocked(t *testing.T) {
	c := fixture(t)
	l, err := Plan(context.Background(), c, fixtureID, "default")
	if err != nil {
		t.Fatal(err)
	}
	lockDir := filepath.Join(c.Runtime.SharedCodexHome, "thread-writer-locks")
	if err = os.MkdirAll(lockDir, 0700); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(lockDir, fixtureID+".lock")
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	before, _ := f.Stat()
	dbPath := filepath.Join(c.Runtime.SharedCodexHome, "state_5.sqlite")
	dbBefore, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	err = Prepare(context.Background(), c, l)
	if err == nil || !strings.Contains(err.Error(), fixtureID) || !strings.Contains(err.Error(), "resume was not started") {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), "argv"))
	if string(args) != "app-server\ndaemon\nversion\n" {
		t.Fatalf("unexpected external command %q", args)
	}
	after, err := os.Stat(lock)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("native lock was replaced or removed", err)
	}
	dbAfter, _ := os.ReadFile(dbPath)
	if !bytes.Equal(dbBefore, dbAfter) {
		t.Fatal("native DB changed")
	}
	if _, err = os.Stat(receiptPath(c, fixtureID)); !os.IsNotExist(err) {
		t.Fatal("failed handoff recorded as launched")
	}
}
