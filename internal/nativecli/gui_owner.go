package nativecli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/codeasy-digix/codexm/internal/config"
)

type processIdentity struct {
	PID        int    `json:"pid"`
	Parent     int    `json:"parent"`
	Started    string `json:"started"`
	Executable string `json:"executable"`
}
type guiOwner struct {
	Process processIdentity `json:"process"`
	Writer  processIdentity `json:"writer"`
	App     string          `json:"app"`
}

func inspectProcess(ctx context.Context, pid int) (processIdentity, error) {
	p := processIdentity{PID: pid}
	if pid <= 1 {
		return p, errors.New("invalid process")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(pid), "-o", "uid=,ppid=,lstart=,comm=")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	b, err := cmd.Output()
	fields := strings.Fields(string(b))
	if err != nil || len(fields) < 8 || fields[0] != strconv.Itoa(os.Getuid()) {
		return p, errors.New("process is absent or belongs to another user")
	}
	p.Parent, err = strconv.Atoi(fields[1])
	if err != nil {
		return p, err
	}
	p.Started = strings.Join(fields[2:7], " ")
	p.Executable = strings.Join(fields[7:], " ")
	return p, nil
}
func sameProcess(ctx context.Context, p processIdentity) bool {
	now, err := inspectProcess(ctx, p.PID)
	// Exec deliberately changes the launcher's executable; PID/start remain stable.
	return err == nil && now.Started == p.Started
}
func writerProcesses(ctx context.Context, shared, id string) ([]int, error) {
	if !threadID.MatchString(id) {
		return nil, errors.New("invalid chat UUID")
	}
	file, err := filepath.EvalSymlinks(filepath.Join(shared, "thread-writer-locks", id+".lock"))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "lsof", "-nP", "-t", "--", file).Output()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return nil, fmt.Errorf("cannot verify writer: %w", err)
		}
	}
	unique := map[int]bool{}
	for _, s := range strings.Fields(string(b)) {
		pid, e := strconv.Atoi(s)
		if e != nil || pid <= 1 {
			return nil, errors.New("invalid writer PID")
		}
		unique[pid] = true
	}
	out := []int{}
	for pid := range unique {
		out = append(out, pid)
	}
	return out, nil
}
func verifyGUIApp(ctx context.Context, app string) error {
	if runtime.GOOS != "darwin" || !filepath.IsAbs(app) || !strings.HasSuffix(app, ".app") {
		return errors.New("GUI restart is supported only for the signed macOS Codex app")
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	// Validate the installed app's identity, never its private IPC or UI surface.
	requirement := `=anchor apple generic and identifier "com.openai.codex" and certificate leaf[subject.OU] = "2DC432GLL2"`
	if err := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "-R", requirement, app).Run(); err != nil {
		return errors.New("the writer's app is not a verified OpenAI Codex desktop app")
	}
	return nil
}
func findGUIOwner(ctx context.Context, c config.Config, id string) (guiOwner, error) {
	var owner guiOwner
	if runtime.GOOS != "darwin" {
		return owner, errors.New("automatic GUI restart requires macOS")
	}
	pids, err := writerProcesses(ctx, config.Expand(c.Runtime.SharedCodexHome), id)
	if err != nil || len(pids) != 1 {
		return owner, errors.New("cannot identify one GUI writer safely")
	}
	writer, err := inspectProcess(ctx, pids[0])
	if err != nil {
		return owner, err
	}
	p := writer
	for i := 0; i < 12; i++ {
		// Only a bundle's main executable qualifies; nested CLI helpers alone do not.
		if prefix, _, ok := strings.Cut(p.Executable, ".app/Contents/MacOS/"); ok {
			app := prefix + ".app"
			if p.Executable == filepath.Join(app, "Contents", "MacOS", filepath.Base(p.Executable)) && p.PID != writer.PID {
				if err := verifyGUIApp(ctx, app); err == nil && strings.HasPrefix(writer.Executable, app+"/Contents/") {
					return guiOwner{Process: p, Writer: writer, App: app}, nil
				}
			}
		}
		if p.Parent <= 1 {
			break
		}
		p, err = inspectProcess(ctx, p.Parent)
		if err != nil {
			break
		}
	}
	return owner, errors.New("writer is not a verified child of the current user's Codex GUI; exit its CLI or archive in its owner")
}
