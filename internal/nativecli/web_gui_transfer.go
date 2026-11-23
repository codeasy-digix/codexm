package nativecli

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/codeasy-digix/codexm/internal/catalog"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

var errWebGUITransferPending = errors.New("이 채팅을 다른 창으로 인계하고 있습니다.")
var errWebGUITransferChanged = errors.New("인계할 작업이나 실행 창이 바뀌었습니다. 현재 상태를 확인한 뒤 다시 선택해 주세요.")

const guiHandoffPausePrefix = "gui-handoff-"

func guiHandoffPause(operation string) string {
	return fmt.Sprintf("%s%x", guiHandoffPausePrefix, sha256.Sum256([]byte(operation)))
}

func (w *WebChat) guiTransferActive(thread string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.guiTransfers[thread]
}

// A transfer reserves only this chat. Its queue stays on disk, including the
// previous pause and the login attached to each saved input.
func (w *WebChat) pauseGUITransferQueue(thread, operation string) error {
	paused, err := w.readQueuePause(thread)
	if err != nil || paused != "" {
		return err
	}
	return config.AtomicWrite(w.queuePausePath(thread), []byte(guiHandoffPause(operation)), 0600)
}

func (w *WebChat) finishGUITransferQueue(thread, operation string, compatible func(webQueuedInput) bool) error {
	paused, err := w.readQueuePause(thread)
	if err != nil || paused != guiHandoffPause(operation) && (compatible == nil || !strings.HasPrefix(paused, guiHandoffPausePrefix)) {
		return err
	}
	if compatible != nil {
		q, err := w.readOwnedQueue(thread)
		if err != nil {
			return err
		}
		for _, input := range q {
			if input.State != "held" && !compatible(input) {
				return nil // Keep the original entry and wait for an explicit queue decision.
			}
		}
	}
	return os.Remove(w.queuePausePath(thread))
}

func (w *WebChat) watchGUITransfer(c *webConnection, thread string, finish func(context.Context)) {
	w.mu.Lock()
	if w.guiTransfers == nil {
		w.guiTransfers = make(map[string]bool)
	}
	w.guiTransfers[thread] = true
	c.inUse++
	w.mu.Unlock()
	go func() {
		defer w.releaseConnection(c)
		defer func() {
			w.mu.Lock()
			delete(w.guiTransfers, thread)
			w.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(w.ctx, 2*webRelayTimeout+15*time.Second)
		defer cancel()
		finish(ctx)
	}()
}

func waitGUITransfer(ctx context.Context, c config.Config, id string) (webGUIRelayReceipt, error) {
	for ctx.Err() == nil {
		receipt, err := readGUIRelay(c, id)
		if err != nil || (receipt.State != "pending" && receipt.State != "claimed") {
			return receipt, err
		}
		if !time.Now().Before(receipt.ExpiresAt) {
			expireGUIRelay(c, id)
			continue
		}
		select {
		case <-ctx.Done():
			return receipt, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return webGUIRelayReceipt{}, ctx.Err()
}

func (w *WebChat) webGUIHandoffEntry(ctx context.Context, thread string) error {
	if !w.webOnly || w.guiRelayThread(thread) == "" {
		return errors.New("이 웹 설치에서 GUI 인계를 사용할 수 없습니다.")
	}
	entry, err := catalog.Lookup(ctx, config.Expand(w.c.Runtime.SharedCodexHome), thread)
	if err != nil || entry.RolloutPath == "" || entry.Archived {
		return errors.New("저장된 채팅을 연 뒤 GUI로 인계할 수 있습니다.")
	}
	return nil
}

func (w *WebChat) moveGUIChatToWeb(ctx context.Context, c *webConnection, r Request, p webParams) (any, error) {
	if err := w.webGUIHandoffEntry(ctx, p.Thread); err != nil {
		return nil, err
	}
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	gui := w.guiInputOwner(ctx, p.Thread, c.process.PID)
	if gui == nil {
		return nil, errWebGUITransferChanged
	}
	activity := w.readWebProcessActivity(ctx, home, p.Thread, gui.Writer)
	if activity.State != "idle" && (activity.State != "running" || activity.TurnID != p.Turn) {
		return nil, errWebGUITransferChanged
	}
	if err := w.verifyWebTransferTarget(ctx, c); err != nil {
		return nil, err
	}
	if err := w.preparePrompt(ctx, c, &p); err != nil {
		return nil, err
	}
	if p.Model != "" || p.Effort != "" {
		if err := c.validateTurnModel(ctx, p.Model, p.Effort); err != nil {
			return nil, err
		}
	}
	if p.Goal {
		if _, err := readGoalForChange(ctx, c.client, p.Thread, goalObjective(p.Text, true)); err != nil {
			return nil, err
		}
	}
	if err := verifyWebReleaseArchive(ctx, c.client, p.Thread); err != nil {
		return nil, err
	}
	if err := w.pauseGUITransferQueue(p.Thread, r.Operation); err != nil {
		return nil, err
	}
	if activity.State != "running" {
		p.Turn = ""
	}
	id, err := w.queueGUIRelay(ctx, c, r, p, *gui, p.Turn, "archive")
	if err != nil {
		_ = w.finishGUITransferQueue(p.Thread, r.Operation, nil)
		return nil, err
	}
	w.watchGUITransfer(c, p.Thread, func(ctx context.Context) { w.finishGUIToWeb(ctx, c, r, p, id) })
	return map[string]string{"mode": "relay", "status": "pending", "relay_id": id, "handoff": "web"}, nil
}

func (w *WebChat) finishGUIToWeb(ctx context.Context, c *webConnection, r Request, p webParams, id string) {
	receipt, err := waitGUITransfer(ctx, w.c, id)
	if err != nil {
		return
	}
	unlock, err := lockChatMutation(ctx, w.c, p.Thread)
	if err != nil {
		return
	}
	defer unlock()
	switch receipt.State {
	case "expired", "rejected":
		_ = w.finishGUITransferQueue(p.Thread, r.Operation, nil)
		return
	case "released":
	default:
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	err = w.startReleasedGUIChat(ctx, c, r, p)
	state, failure := "accepted", ""
	if err == nil {
		_ = w.finishGUITransferQueue(p.Thread, r.Operation, func(input webQueuedInput) bool {
			return input.GUIWriter == nil && input.Identity == queueLoginKey(c)
		})
	} else {
		state, failure = "rejected", cr.SafeText(err.Error())
		if errors.Is(err, errWebInputUnconfirmed) {
			state = "unknown"
		}
	}
	_ = updateGUIRelay(w.c, id, func(saved *webGUIRelayReceipt) error {
		if saved.State == "released" {
			saved.State, saved.Failure = state, failure
		}
		return nil
	})
}

func (w *WebChat) startReleasedGUIChat(ctx context.Context, c *webConnection, r Request, p webParams) error {
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	if err := restoreGUITransferThread(ctx, c.client, home, p.Thread); err != nil {
		return err
	}
	if err := w.verifyWebTransferTarget(ctx, c); err != nil {
		return err
	}
	if err := w.prepare(ctx, c, p.Thread); err != nil {
		return err
	}
	owner, err := w.openInputOwnerFor(ctx, p.Thread, c.process.PID, false)
	if err != nil {
		return err
	}
	defer owner.release()
	// The source turn was released. Send one literal input to this same chat.
	p.Turn = ""
	if p.Goal {
		_, err = w.deliverGoal(ctx, owner, c, r, p)
	} else {
		_, err = w.deliverWebInput(ctx, owner, c, r, p)
	}
	return err
}

func restoreGUITransferThread(ctx context.Context, client *cr.Client, home, thread string) error {
	archived, err := catalog.Archived(ctx, home, thread)
	if err != nil || !archived || cr.WriterBusy(home, thread) {
		return errors.New("원래 창의 보관과 잠금 해제를 함께 확인할 수 없습니다. 채팅 상태를 확인해 주세요.")
	}
	if err := callWebInput(ctx, client, "thread/unarchive", map[string]any{"threadId": thread}, nil); err != nil {
		return err
	}
	archived, err = catalog.Archived(ctx, home, thread)
	if err != nil || archived {
		return errors.New("채팅 복원을 확인할 수 없습니다. 보관 목록을 확인해 주세요.")
	}
	return nil
}

func (w *WebChat) moveWebChatToGUI(ctx context.Context, c *webConnection, r Request, p webParams) (any, error) {
	if err := w.webGUIHandoffEntry(ctx, p.Thread); err != nil {
		return nil, err
	}
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	owners, err := webHandoffWriters(ctx, home, p.Thread)
	if err != nil || len(owners) > 1 {
		return nil, errWebOtherOwner
	}
	if len(owners) == 1 && owners[0] != c.process.PID {
		if gui, err := w.identifyGUI(ctx, owners[0]); err == nil {
			activity := w.readWebProcessActivity(ctx, home, p.Thread, gui.Writer)
			return w.submitGUIRelay(ctx, c, r, p, gui, activity.TurnID)
		}
	}
	if err := verifyWebReleaseArchive(ctx, c.client, p.Thread); err != nil {
		return nil, err
	}
	gui, err := w.prepareGUITransfer(ctx, home, w.guiRelayThread(p.Thread))
	if err != nil {
		return nil, err
	}
	if len(owners) == 1 && owners[0] == gui.Writer.PID {
		return nil, errWebGUITransferChanged
	}
	if err := w.pauseGUITransferQueue(p.Thread, r.Operation); err != nil {
		return nil, err
	}
	if len(owners) == 1 {
		if err := w.releaseForGUI(ctx, c, p, owners[0]); err != nil {
			if errors.Is(err, errWebOtherOwner) || errors.Is(err, errWebGUITransferChanged) {
				_ = w.finishGUITransferQueue(p.Thread, r.Operation, nil)
			}
			return nil, err
		}
	}
	if cr.WriterBusy(home, p.Thread) || !sameProcess(ctx, gui.Process) || !sameProcess(ctx, gui.Writer) {
		return nil, errWebGUITransferChanged
	}
	// The official desktop tool loads a released chat when delivering input.
	// A deep link alone may show history without its app-server taking the lock.
	id, err := w.queueGUIRelay(ctx, c, r, p, gui, "", "acquire")
	if err != nil {
		return nil, err
	}
	w.watchGUITransfer(c, p.Thread, func(ctx context.Context) {
		receipt, err := waitGUITransfer(ctx, w.c, id)
		if err != nil || receipt.State != "acquired" {
			return
		}
		unlock, err := lockChatMutation(ctx, w.c, p.Thread)
		if err != nil {
			return
		}
		defer unlock()
		probe, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		state, failure := "accepted", ""
		if err := waitWebGUIWriter(probe, home, p.Thread, gui.Writer.PID); err != nil || !sameProcess(probe, gui.Writer) {
			state, failure = "unknown", "입력은 GUI로 전달됐지만 새 제어권을 확인할 수 없습니다. 데스크톱 채팅을 확인해 주세요."
		} else {
			_ = openWebGUIThread(probe, gui.App, p.Thread)
			_ = w.finishGUITransferQueue(p.Thread, r.Operation, func(input webQueuedInput) bool {
				return input.GUIWriter != nil && *input.GUIWriter == gui.Writer
			})
		}
		_ = updateGUIRelay(w.c, id, func(saved *webGUIRelayReceipt) error {
			if saved.State == "acquired" {
				saved.State, saved.Failure = state, failure
			}
			return nil
		})
	})
	return map[string]string{"mode": "relay", "status": "pending", "relay_id": id, "handoff": "gui"}, nil
}

func (w *WebChat) releaseForGUI(ctx context.Context, c *webConnection, p webParams, pid int) error {
	probe, cancel := context.WithTimeout(ctx, 4*time.Second)
	source, err := w.openInputOwnerFor(probe, p.Thread, pid, true)
	cancel()
	home := config.Expand(w.c.Runtime.SharedCodexHome)
	if err != nil {
		if w.isGUIInputWriter(ctx, pid) {
			return errWebGUITransferChanged
		}
		launch, err := readLast(w.c, p.Thread)
		if err != nil || launch.Owner.PID != pid || verifyIndependentCaller(ctx, pid) != nil || verifyOwnedCLI(ctx, w.c, launch) != nil {
			return errWebOtherOwner
		}
		activity := w.readWebProcessActivity(ctx, home, p.Thread, *launch.Owner)
		if activity.State != "idle" && (activity.State != "running" || activity.TurnID != p.Turn) {
			return errWebGUITransferChanged
		}
		if err := verifyWebReleaseArchive(ctx, c.client, p.Thread); err != nil {
			return err
		}
		if err := verifyOwnedCLI(ctx, w.c, launch); err != nil {
			return errWebOtherOwner
		}
		// Only an explicitly selected CLI with a current CodexM launch receipt
		// and a single writer can use this route. Another CLI needs owner RPC.
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return err
		}
		wait, done := context.WithTimeout(ctx, 12*time.Second)
		defer done()
		return waitExecRelease(wait, home, p.Thread, execOwner{Process: *launch.Owner, Key: launch.OwnerKey})
	}
	defer source.release()
	if err := verifyWebReleaseArchive(ctx, source.client, p.Thread); err != nil {
		return err
	}
	archived, err := w.releaseWebChat(ctx, source, p.Thread, true, func(_ context.Context, turn string) error {
		if p.Turn != turn {
			return errWebGUITransferChanged
		}
		return nil
	})
	if err != nil || !archived {
		return err
	}
	return restoreGUITransferThread(ctx, c.client, home, p.Thread)
}

func (w *WebChat) prepareGUITransfer(ctx context.Context, home, relay string) (guiOwner, error) {
	probe, cancel := context.WithTimeout(ctx, 9*time.Second)
	defer cancel()
	owners, err := webHandoffWriters(probe, home, relay)
	if err != nil || len(owners) > 1 {
		return guiOwner{}, errWebForeignInput
	}
	if len(owners) == 1 {
		return w.identifyGUI(probe, owners[0])
	}
	desktop, err := findWebDesktop(probe)
	app := desktop.app
	if errors.Is(err, errWebDesktopNotRunning) {
		for _, candidate := range []string{"/Applications/Codex.app", "/Applications/ChatGPT.app"} {
			if verifyGUIApp(probe, candidate) == nil {
				app = candidate
				break
			}
		}
	} else if err != nil {
		return guiOwner{}, err
	}
	if app == "" {
		return guiOwner{}, errWebDesktopNotRunning
	}
	if archived, err := catalog.Archived(probe, home, relay); err != nil || archived {
		return guiOwner{}, errors.New("설정된 GUI 중계 채팅을 찾을 수 없습니다.")
	}
	if err := openWebGUIThread(probe, app, relay); err != nil {
		return guiOwner{}, err
	}
	for probe.Err() == nil {
		owners, err := webHandoffWriters(probe, home, relay)
		if err != nil || len(owners) > 1 {
			return guiOwner{}, errWebForeignInput
		}
		if len(owners) == 1 {
			owner, err := w.identifyGUI(probe, owners[0])
			if err == nil && owner.App == app && (desktop.main.pid == 0 || desktop.main.pid == owner.Process.PID) {
				return owner, nil
			}
			return guiOwner{}, errWebGUITransferChanged
		}
		select {
		case <-probe.Done():
		case <-time.After(150 * time.Millisecond):
		}
	}
	return guiOwner{}, errors.New("데스크톱에서 중계 채팅을 열 수 없습니다.")
}

func openWebGUIThread(ctx context.Context, app, thread string) error {
	if !threadID.MatchString(thread) || !filepath.IsAbs(app) {
		return errWebForeignInput
	}
	if err := exec.CommandContext(ctx, "/usr/bin/open", "-g", "-a", app, "codex://threads/"+thread).Run(); err != nil {
		return errors.New("Codex 데스크톱에서 채팅을 열 수 없습니다.")
	}
	return nil
}

func waitWebGUIWriter(ctx context.Context, home, thread string, pid int) error {
	for ctx.Err() == nil {
		owners, err := webHandoffWriters(ctx, home, thread)
		if err != nil || len(owners) > 1 {
			return errWebGUITransferChanged
		}
		if len(owners) == 1 {
			if owners[0] != pid {
				return errWebGUITransferChanged
			}
			return nil
		}
		select {
		case <-ctx.Done():
		case <-time.After(150 * time.Millisecond):
		}
	}
	return errors.New("데스크톱이 이 채팅의 제어권을 얻었는지 확인할 수 없습니다.")
}

func guiArchivePrompt(exe, configPath, id string) string {
	claim, complete := guiRelayCommands(exe, configPath, id)
	return fmt.Sprintf(`CodexM GUI handoff. The authenticated user explicitly confirmed releasing only the exact local chat in this one-time request so CodexM can reopen the same chat. Run this as one functions.exec call. Do not retry any tool call or change any other chat. CodexM restores and resumes the chat after the GUI releases it.

const claim = await tools.exec_command({cmd: %s, max_output_tokens: 500});
if (claim.exit_code !== 0) { text("Handoff request unavailable"); exit(); }
let outcome = "rejected";
try {
  const input = JSON.parse(claim.output);
  const result = await tools.mcp__codex_app__read_thread({threadId: input.thread_id, hostId: "local", turnLimit: 1, includeOutputs: false, maxOutputCharsPerItem: 80});
  const snapshot = JSON.parse(result.content.find(item => item.type === "text").text);
  const turn = snapshot.turns?.[0];
  if (!result.isError && snapshot.thread?.id === input.thread_id &&
      (input.expected_turn ? turn?.id === input.expected_turn : snapshot.thread.status?.type === "idle")) {
    outcome = "unknown";
    const result = await tools.mcp__codex_app__set_thread_archived({threadId: input.thread_id, hostId: "local", archived: true});
    const reply = JSON.parse(result.content.find(item => item.type === "text").text);
    if (!result.isError && reply.threadId === input.thread_id && reply.archived === true) outcome = "accepted";
  }
} catch {}
const completion = await tools.exec_command({cmd: %s + outcome, max_output_tokens: 500});
text(completion.exit_code === 0 ? outcome : "unknown");

End after reporting the saved outcome in one line.`, claim, complete)
}
