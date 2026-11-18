package nativecli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

type webDelivery struct {
	State  string `json:"state"`
	TurnID string `json:"turnId,omitempty"`
}

type webInputOwner struct {
	client      *cr.Client
	web         *webConnection
	pid         int
	incarnation string
	release     func()
}

var errWebForeignInput = errors.New("이 채팅을 실행하는 창에 연결할 수 없습니다. 입력은 전송되지 않았습니다.")

func webWriterPIDs(ctx context.Context, home, id string) ([]int, error) {
	pids, err := writerProcesses(ctx, home, id)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		pids, err = writerProcesses(ctx, home, id)
	}
	if os.IsNotExist(err) {
		err = nil
	}
	return pids, err
}

// Take a short-lived client lease, not the chat's writer lock. Only the native
// server already holding that lock can authorize direct input.
func (w *WebChat) openInputOwner(ctx context.Context, id string, pid int) (*webInputOwner, error) {
	incarnation, err := cr.NativeProcessIdentity(pid)
	if err != nil {
		return nil, err
	}
	target := &webInputOwner{pid: pid, incarnation: incarnation}
	w.mu.Lock()
	for _, c := range w.connections {
		if c.process.PID == pid && !c.retiring && sameProcess(ctx, c.process) {
			c.inUse++
			target.client, target.web = c.client, c
			target.release = func() { w.releaseConnection(c) }
			break
		}
	}
	w.mu.Unlock()
	if target.client == nil {
		target.client, err = w.dialExternalWriter(ctx, pid)
		if err != nil {
			return nil, err
		}
		target.release = func() { target.client.Close() }
	}
	loaded, err := webLoadedThreads(ctx, target.client)
	if _, found := loaded[id]; err != nil || !found {
		target.release()
		return nil, errWebForeignInput
	}
	return target, nil
}

func (w *WebChat) dialExternalWriter(ctx context.Context, pid int) (*cr.Client, error) {
	process, err := inspectProcess(ctx, pid)
	if err != nil {
		return nil, err
	}
	argv, err := processArguments(pid)
	if err != nil {
		return nil, err
	}
	command, known := nativeCommand(argv)
	if !known || command != "app-server" {
		return nil, errWebForeignInput
	}
	paths := webListenSockets(argv)
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	homes := []string{home}
	root := config.Expand(w.c.Runtime.AccountsDir)
	if names, err := os.ReadDir(root); err == nil {
		for _, name := range names {
			if name.IsDir() && config.Nickname.MatchString(name.Name()) && len(homes) < 65 {
				homes = append(homes, filepath.Join(root, name.Name()))
			}
		}
	}
	executable, err := filepath.EvalSymlinks(process.Executable)
	if err != nil || !filepath.IsAbs(executable) {
		return nil, errWebForeignInput
	}
	r, _ := Resolver(w.c)
	want, _ := filepath.EvalSymlinks(r.Binary)
	trusted := want != "" && want == executable
	for _, dir := range homes {
		paths = append(paths, filepath.Join(dir, "app-server-control", "app-server-control.sock"))
		managed, err := filepath.EvalSymlinks(filepath.Join(dir, "packages", "app-server-daemon", "current", "bin", "codex"))
		trusted = trusted || (err == nil && managed == executable)
	}
	seen := make(map[string]bool, len(paths))
	for _, name := range paths {
		socket, err := filepath.EvalSymlinks(name)
		if err != nil || seen[socket] {
			continue
		}
		seen[socket] = true
		info, err := os.Stat(socket)
		if err != nil || info.Mode()&os.ModeSocket == 0 {
			continue
		}
		if !trusted {
			bundle, _, found := strings.Cut(executable, ".app/Contents/")
			if !found || filepath.Base(executable) != "codex" || verifyGUIApp(ctx, bundle+".app") != nil {
				return nil, errWebForeignInput
			}
			trusted = true
		}
		// Authenticate the listener itself before initialize, even if this is
		// an official control path now pointing at a different running server.
		client, err := cr.DialUnixWriter(ctx, socket, pid)
		if err == nil {
			return client, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, errWebForeignInput
}

func webListenSockets(args []string) []string {
	var sockets []string
	for i, arg := range args {
		name, value, hasValue := strings.Cut(arg, "=")
		if name != "--listen" {
			continue
		}
		if !hasValue && i+1 < len(args) {
			value = args[i+1]
		}
		path, unix := strings.CutPrefix(value, "unix://")
		if unix && filepath.IsAbs(path) {
			sockets = append(sockets, path)
		}
	}
	return sockets
}

func (o *webInputOwner) delivery(ctx context.Context, id string) (webDelivery, error) {
	thread, err := o.client.Read(ctx, id, false)
	if err != nil {
		return webDelivery{}, err
	}
	if thread.ID != id {
		return webDelivery{}, errors.New("app-server returned a different chat")
	}
	switch thread.Status.Type {
	case "idle":
		return webDelivery{State: "idle"}, nil
	case "active":
	default:
		return webDelivery{State: "unavailable"}, nil
	}
	if len(thread.Status.ActiveFlags) != 0 {
		return webDelivery{State: "waiting"}, nil
	}
	var page struct{ Data []cr.Turn }
	args := map[string]any{"threadId": id, "limit": 1, "sortDirection": "desc", "itemsView": "notLoaded"}
	if err := o.client.Call(ctx, "thread/turns/list", args, &page); err != nil {
		return webDelivery{}, err
	}
	if len(page.Data) != 1 || page.Data[0].Status != "inProgress" || page.Data[0].ID == "" {
		return webDelivery{State: "unavailable"}, nil
	}
	return webDelivery{State: "ready", TurnID: page.Data[0].ID}, nil
}

func (w *WebChat) verifyInputOwner(ctx context.Context, owner *webInputOwner, id string, allowFree bool) error {
	pids, err := webWriterPIDs(ctx, config.Expand(w.c.Runtime.SharedCodexHome), id)
	if err != nil {
		return err
	}
	if !(len(pids) == 1 && pids[0] == owner.pid || allowFree && len(pids) == 0) {
		return errors.New("채팅의 실행 연결이 바뀌었습니다. 상태를 다시 확인해 주세요.")
	}
	incarnation, err := cr.NativeProcessIdentity(owner.pid)
	if err != nil || incarnation != owner.incarnation {
		return errors.New("채팅의 실행 연결이 바뀌었습니다. 상태를 다시 확인해 주세요.")
	}
	return nil
}

func (w *WebChat) externalDelivery(ctx context.Context, id string, pids []int) webDelivery {
	unavailable := webDelivery{State: "unavailable"}
	if len(pids) != 1 {
		return unavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	o, err := w.openInputOwner(ctx, id, pids[0])
	if err != nil {
		return w.guiRelayDelivery(ctx, id, pids[0], unavailable)
	}
	defer o.release()
	result, err := o.delivery(ctx, id)
	if err != nil {
		return unavailable
	}
	return result
}
