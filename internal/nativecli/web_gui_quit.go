package nativecli

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

type desktopProcess struct {
	pid int
	key string
}

type webDesktop struct {
	main    desktopProcess
	app     string
	servers []desktopProcess
}

type webGUIQuitTicket struct {
	token   string
	expires time.Time
	main    desktopProcess
	app     string
}

type webGUIQuit struct {
	mu      sync.Mutex
	tickets map[string]webGUIQuitTicket
}

type findWebDesktopFunc func(context.Context) (webDesktop, error)

func (q *webGUIQuit) preview(ctx context.Context, owner string, find findWebDesktopFunc) (any, error) {
	if owner == "" {
		return nil, errors.New("authenticated browser required")
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	desktop, err := find(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for browser, ticket := range q.tickets {
		if !now.Before(ticket.expires) {
			delete(q.tickets, browser)
		}
	}
	if q.tickets == nil {
		q.tickets = make(map[string]webGUIQuitTicket)
	}
	ticket := webGUIQuitTicket{token: rand.Text(), expires: now.Add(time.Minute), main: desktop.main, app: desktop.app}
	q.tickets[owner] = ticket
	return map[string]string{"confirmation": ticket.token, "application": filepath.Base(desktop.app)}, nil
}

func (q *webGUIQuit) quit(ctx context.Context, owner, confirmation string, find findWebDesktopFunc, stop func(context.Context, webDesktop) error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	ticket, ok := q.tickets[owner]
	if !ok || ticket.token != confirmation || !time.Now().Before(ticket.expires) {
		return errors.New("종료 확인이 만료됐습니다. GUI 종료 버튼에서 다시 확인해 주세요.")
	}
	delete(q.tickets, owner)
	desktop, err := find(ctx)
	if err != nil || desktop.main != ticket.main || desktop.app != ticket.app {
		return errors.New("확인 후 GUI 실행 상태가 바뀌었습니다. GUI 종료 버튼에서 다시 확인해 주세요.")
	}
	return stop(ctx, desktop)
}

func findWebDesktop(ctx context.Context) (webDesktop, error) {
	var desktop webDesktop
	if runtime.GOOS != "darwin" {
		return desktop, errors.New("GUI 종료는 macOS에서 사용할 수 있습니다.")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "/bin/ps", "-axww", "-o", "uid=,pid=,ppid=,comm=").Output()
	if err != nil {
		return desktop, errors.New("실행 중인 GUI를 확인할 수 없습니다.")
	}
	processes := map[int]processIdentity{}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != strconv.Itoa(os.Getuid()) {
			continue
		}
		pid, _ := strconv.Atoi(fields[1])
		parent, _ := strconv.Atoi(fields[2])
		if pid <= 1 {
			continue
		}
		processes[pid] = processIdentity{PID: pid, Parent: parent, Executable: strings.Join(fields[3:], " ")}
	}
	for pid, process := range processes {
		prefix, executable, ok := strings.Cut(process.Executable, ".app/Contents/MacOS/")
		if !ok || (executable != "Codex" && executable != "ChatGPT") || !filepath.IsAbs(prefix) {
			continue
		}
		app := prefix + ".app"
		key, err := cr.NativeProcessIdentity(pid)
		if err != nil || verifyGUIApp(ctx, app) != nil {
			continue
		}
		if desktop.main.pid != 0 {
			return webDesktop{}, errors.New("실행 중인 Codex GUI를 하나로 확인할 수 없습니다. 종료할 앱을 직접 확인해 주세요.")
		}
		desktop.main, desktop.app = desktopProcess{pid, key}, app
	}
	if desktop.main.pid == 0 {
		return desktop, errors.New("이 Mac에서 실행 중인 Codex GUI를 찾을 수 없습니다.")
	}
	for pid, process := range processes {
		if !strings.HasPrefix(process.Executable, desktop.app+"/Contents/") ||
			filepath.Base(process.Executable) != "codex" || !withinWebDesktop(processes, process, desktop.main.pid) {
			continue
		}
		argv, err := processArguments(pid)
		command, known := nativeCommand(argv)
		if err != nil || !known || command != "app-server" {
			continue
		}
		if key, err := cr.NativeProcessIdentity(pid); err == nil {
			desktop.servers = append(desktop.servers, desktopProcess{pid, key})
		}
	}
	return desktop, nil
}

func withinWebDesktop(processes map[int]processIdentity, process processIdentity, root int) bool {
	for depth := 0; process.Parent > 1 && depth < 12; depth++ {
		if process.Parent == root {
			return true
		}
		process = processes[process.Parent]
	}
	return false
}

func stopWebDesktop(ctx context.Context, desktop webDesktop) error {
	if desktop.main.pid <= 1 || desktop.main.key == "" {
		return errors.New("종료할 Codex GUI를 확인할 수 없습니다.")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key, err := cr.NativeProcessIdentity(desktop.main.pid)
	if err != nil || key != desktop.main.key {
		return errors.New("종료 직전에 GUI 실행 상태가 바뀌었습니다.")
	}
	if err := syscall.Kill(desktop.main.pid, syscall.SIGTERM); err != nil {
		return errors.New("Codex GUI에 종료 요청을 보내지 못했습니다.")
	}
	wait, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	// An independent CLI/daemon may still own a chat. Look only for the
	// verified app and its original bundled app-servers, which release their
	// writer locks when they exit.
	processes := append([]desktopProcess{desktop.main}, desktop.servers...)
	for _, process := range processes {
		for {
			current, err := cr.NativeProcessIdentity(process.pid)
			if errors.Is(err, os.ErrProcessDone) || os.IsNotExist(err) || (err == nil && current != process.key) {
				break
			}
			select {
			case <-wait.Done():
				return errors.New("GUI 또는 해당 app-server의 종료를 확인하지 못했습니다. 실행 상태를 확인해 주세요.")
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	return nil
}
