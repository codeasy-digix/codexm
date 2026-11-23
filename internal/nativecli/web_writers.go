package nativecli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/config"
)

type webWriter struct {
	pid      int
	started  time.Time
	activity webActivity
	path     string
	file     os.FileInfo
	readTo   int64
}

type webWriterMonitor struct {
	mu       sync.Mutex
	writers  map[string]webWriter
	checked  time.Time
	scanning bool
	known    bool
	detailID string
	detail   webWriter // Last focused chat; subsequent reads consume only appended records.
}

// One bounded background observation serves all browser views. Consecutive
// reads of web activity neither launch a Codex runtime nor run lsof per row.
func (w *WebChat) writerSnapshot() (map[string]webWriter, bool) {
	m := &w.writers
	m.mu.Lock()
	defer m.mu.Unlock()
	age := time.Since(m.checked)
	if !m.scanning && age > 1200*time.Millisecond {
		m.scanning = true
		home := config.Expand(w.c.Runtime.SharedCodexHome)
		previous := m.writers
		detailID, detail := m.detailID, m.detail
		go func() {
			ctx, cancel := context.WithTimeout(w.ctx, 5*time.Second)
			defer cancel()
			writers, err := observeWebWriters(ctx, home, previous, detailID, detail)
			m.mu.Lock()
			m.writers, m.known = writers, err == nil
			m.checked, m.scanning = time.Now(), false
			m.mu.Unlock()
		}()
	}
	if !m.known || age > 6*time.Second {
		return nil, false
	}
	return m.writers, true
}

func observeWebWriters(ctx context.Context, home string, previous map[string]webWriter, detailID string, detail webWriter) (map[string]webWriter, error) {
	owners, err := scanWebLockOwners(ctx, home)
	if err != nil {
		return nil, err
	}
	writers := make(map[string]webWriter, len(owners))
	starts := make(map[int]time.Time)
	sessionDir, _ := filepath.EvalSymlinks(filepath.Join(home, "sessions"))
	root, _ := os.OpenRoot(sessionDir)
	if root != nil {
		defer root.Close()
	}
	probed := 0
	for id, pid := range owners {
		writer := webWriter{pid: pid, activity: webActivity{Source: "external", State: "unknown"}}
		if root != nil && pid > 0 && ctx.Err() == nil && (probed < 32 || id == detailID) {
			probed++
			start, checked := starts[pid]
			if !checked {
				if process, err := inspectProcess(ctx, pid); err == nil {
					start, _ = time.ParseInLocation("Mon Jan _2 15:04:05 2006", process.Started, time.Local)
				}
				starts[pid] = start
			}
			writer.started = start
			if !start.IsZero() {
				prior := previous[id]
				if id == detailID && detail.pid == pid && detail.started.Equal(start) &&
					(prior.activity.TurnID == "" || detail.readTo >= prior.readTo) {
					prior = detail
				}
				readWebWriter(ctx, root, home, id, &writer, prior, maxWebRolloutTail)
			}
		}
		writers[id] = writer
	}
	return writers, nil
}

func scanWebLockOwners(ctx context.Context, home string) (map[string]int, error) {
	owners := make(map[string]int)
	dir, err := filepath.EvalSymlinks(filepath.Join(home, "thread-writer-locks"))
	if os.IsNotExist(err) {
		return owners, nil
	}
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "lsof", "-nP", "+d", dir, "-F0pn").Output()
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1 || len(exit.Stderr) != 0) {
		return nil, err
	}
	var pid int
	for part := range strings.SplitSeq(string(output), "\x00") {
		part = strings.TrimLeft(part, "\n")
		if len(part) < 2 {
			continue
		}
		if part[0] == 'p' {
			pid, _ = strconv.Atoi(part[1:])
		}
		if part[0] != 'n' || pid <= 1 {
			continue
		}
		id, lock := strings.CutSuffix(filepath.Base(part[1:]), ".lock")
		if !lock || !threadID.MatchString(id) {
			continue
		}
		if old, ok := owners[id]; ok && old != pid {
			owners[id] = 0
		} else {
			owners[id] = pid
		}
	}
	return owners, nil
}

const maxWebRolloutTail = 4 << 20

func readWebWriter(ctx context.Context, root *os.Root, home, id string, writer *webWriter, previous webWriter, tailLimit int64) {
	if previous.pid == writer.pid && previous.started.Equal(writer.started) {
		writer.path = previous.path
	}
	if writer.path == "" {
		path, err := catalog.RolloutPath(ctx, home, id)
		if err != nil || !filepath.IsAbs(path) {
			return
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil || !strings.HasSuffix(filepath.Base(path), "-"+id+".jsonl") {
			return
		}
		writer.path, _ = filepath.Rel(root.Name(), path)
	}
	if !filepath.IsLocal(writer.path) {
		writer.path = ""
		return
	}
	file, err := root.OpenFile(writer.path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		writer.path = ""
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	writer.file = info
	start := max(0, info.Size()-maxWebRolloutTail)
	earliest := max(0, info.Size()-tailLimit)
	partialHead := start != 0
	if previous.file != nil && previous.started.Equal(writer.started) && previous.pid == writer.pid && os.SameFile(previous.file, info) {
		if info.Size() == previous.file.Size() && info.ModTime().Equal(previous.file.ModTime()) {
			writer.activity, writer.readTo = previous.activity, previous.readTo
			return
		}
		if info.Size() > previous.file.Size() && previous.readTo > 0 && previous.readTo >= start {
			start, partialHead = previous.readTo, false
			writer.activity = previous.activity
		}
	}
	for ctx.Err() == nil {
		buffer := make([]byte, info.Size()-start)
		n, err := file.ReadAt(buffer, start)
		if err != nil && !errors.Is(err, io.EOF) {
			return
		}
		end := bytes.LastIndexByte(buffer[:n], '\n')
		writer.readTo = start + int64(end+1)
		if next, ok := lastWebRolloutActivity(buffer[:n], partialHead, writer.started, writer.activity); ok {
			writer.activity = next
			return
		}
		if !partialHead || start <= earliest || writer.activity.TurnID != "" {
			return
		}
		// A large tool result may hide the last boundary. Only a targeted
		// input check gets a larger budget; sidebar polls keep the small tail.
		start = max(earliest, info.Size()-2*(info.Size()-start))
		partialHead = start > 0
	}
}

// A lock alone only proves that a client has the chat open. An app tool can
// start a real turn in this chat while attributing it to another chat's root.
func lastWebRolloutActivity(buffer []byte, partialHead bool, holderStarted time.Time, prior webActivity) (webActivity, bool) {
	var ended map[string]bool
	for end := bytes.LastIndexByte(buffer, '\n'); end >= 0; {
		start := bytes.LastIndexByte(buffer[:end], '\n')
		if start < 0 && partialHead {
			break
		}
		line := buffer[start+1 : end]
		end = start
		if !bytes.Contains(line, []byte(`"event_msg"`)) || !(bytes.Contains(line, []byte(`"task_started"`)) ||
			bytes.Contains(line, []byte(`"task_complete"`)) || bytes.Contains(line, []byte(`"turn_aborted"`))) {
			continue
		}
		var event struct {
			Type      string
			Timestamp time.Time
			Payload   struct {
				Type        string
				TurnID      string `json:"turn_id"`
				RootTurn    string `json:"root_turn_id"`
				Attribution struct {
					Trigger string `json:"turn_trigger"`
				} `json:"turn_attribution"`
			}
		}
		p := &event.Payload
		if json.Unmarshal(line, &event) != nil || event.Type != "event_msg" || p.TurnID == "" {
			continue
		}
		root := p.RootTurn == "" || p.RootTurn == p.TurnID
		activity := webActivity{Source: "external", TurnID: p.TurnID}
		switch p.Type {
		case "task_started":
			if !root && p.Attribution.Trigger != "app_tool_send_message" && p.Attribution.Trigger != "app_tool_create_thread" {
				continue
			}
			activity.State = "unknown"
			if ended[p.TurnID] {
				activity.State = "idle"
			} else if !event.Timestamp.IsZero() && !holderStarted.IsZero() && !event.Timestamp.Before(holderStarted) {
				activity.State = "running"
			}
		case "task_complete", "turn_aborted":
			if !root && prior.TurnID != p.TurnID {
				if ended == nil {
					ended = make(map[string]bool)
				}
				ended[p.TurnID] = true
				continue
			}
			activity.State = "idle"
		default:
			continue
		}
		return activity, true
	}
	return webActivity{}, false
}
