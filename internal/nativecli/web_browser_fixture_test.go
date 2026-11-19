package nativecli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/accounts"
	"github.com/codeasy-digix/codexm/internal/config"
	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

// Opt-in browser fixture kept alive only while the external Playwright job
// runs. It uses the real official runtime and a local model, never user chats.
func TestWebChatBrowserFixture(t *testing.T) {
	output := os.Getenv("CODEXM_WEB_BROWSER_FIXTURE")
	if !filepath.IsAbs(output) {
		t.Skip("requires an isolated browser fixture output directory")
	}
	f := newGUIControlFixtureDuration(t, "unix", 45*time.Minute, func(f *guiControlFixture) {
		setupWebFixtureLogins(t, f, "first-browser-login", "second-browser-login")
		if _, err := accounts.SetNativeDefault(f.c, "first-browser-login"); err != nil {
			t.Fatal(err)
		}
	})
	f.mu.Lock()
	f.responseText = "## 작업을 확인했습니다\n\n브라우저에서 표시하는 **구조화된 응답**입니다.\n\n" +
		"- 최근 대화부터 불러옵니다.\n- 명령 실행 과정은 접어서 표시합니다.\n\n" +
		"| 항목 | 표시 위치 | 처리 방식 | 개수 | 확인 사항 |\n" +
		"| --- | --- | --- | ---: | --- |\n" +
		"| 대화 이력 | 본문 | 최근 기록 우선 | 8 | 좁은 화면에서는 표 안에서 가로로 이동할 수 있습니다. |\n" +
		"| 작업 과정 | 세부 항목 | 필요할 때 읽기 | 12 | 코드와 **마크다운**을 표시합니다. |\n\n" +
		"```rust\nfn main() { println!(\"CodexM\"); }\n```\n\n<script>window.__unsafe = true</script>"
	f.commandFixture = true
	responseText := f.responseText
	f.mu.Unlock()
	for _, dir := range []string{"browse-target", filepath.Join(".agents", "skills", "fixture-proof")} {
		if err := os.MkdirAll(filepath.Join(f.cmd.Dir, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(f.cmd.Dir, ".agents", "skills", "fixture-proof", "SKILL.md"),
		[]byte("---\nname: fixture-proof\ndescription: Verify the fixture's skills menu.\n---\n\nSKILL_FIXTURE_BODY\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var project struct{ Project struct{ ID string } }
	if err := f.client.Call(f.ctx, "project/create", map[string]any{"name": "브라우저 테스트 프로젝트", "roots": []map[string]string{{"path": f.cmd.Dir}}, "idempotencyKey": "00000000-0000-4000-8000-000000000061"}, &project); err != nil {
		t.Fatal(err)
	}
	var first struct{ Thread struct{ ID string } }
	if err := f.client.Call(f.ctx, "thread/start", map[string]any{"cwd": f.cmd.Dir, "historyMode": "paginated", "projectId": project.Project.ID}, &first); err != nil {
		t.Fatal(err)
	}
	visualizations := filepath.Join(f.c.Runtime.SharedCodexHome, "visualizations", "2026", "01", "02", first.Thread.ID)
	if err := os.MkdirAll(visualizations, 0700); err != nil {
		t.Fatal(err)
	}
	comparison := filepath.Join(visualizations, "fixture-comparison.html")
	if err := os.WriteFile(comparison, []byte("<!doctype html><h1>Browser artifact fixture</h1><script>window.__unsafe=true</script>"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 26; i++ {
		if i == 26 {
			payload, _ := json.Marshal(map[string]string{"path": comparison, "mode": "wide"})
			f.mu.Lock()
			f.responseText += "\n\nvisualize" + string(payload) + "\n\n![작업 폴더 이미지](./workspace-image.png)"
			f.mu.Unlock()
		}
		f.start(first.Thread.ID, fmt.Sprintf("페이지 이력 %02d", i))
		f.completed(first.Thread.ID, i)
	}
	f.mu.Lock()
	f.responseText = responseText
	f.mu.Unlock()
	if err := f.client.Call(f.ctx, "thread/name/set", map[string]any{"threadId": first.Thread.ID, "name": "긴 이력 페이지 테스트"}, nil); err != nil {
		t.Fatal(err)
	}
	second := f.thread()
	f.start(second, "새 요청 테스트")
	f.completed(second, 1)
	if err := f.client.Call(f.ctx, "thread/name/set", map[string]any{"threadId": second, "name": "진행 중 개입 테스트"}, nil); err != nil {
		t.Fatal(err)
	}
	archived := f.thread()
	f.start(archived, "보관 fixture")
	f.completed(archived, 1)
	if err := f.client.Call(f.ctx, "thread/name/set", map[string]any{"threadId": archived, "name": "보관된 fixture 채팅"}, nil); err != nil {
		t.Fatal(err)
	}
	command := f.thread()
	f.start(command, "WEB_COMMAND_FIXTURE")
	f.completed(command, 1)
	if err := f.client.Call(f.ctx, "thread/name/set", map[string]any{"threadId": command, "name": "긴 명령 출력 검증"}, nil); err != nil {
		t.Fatal(err)
	}
	catchup := f.thread()
	f.start(catchup, "브라우저가 열리기 전 이력")
	f.completed(catchup, 1)
	if err := f.client.Call(f.ctx, "thread/name/set", map[string]any{"threadId": catchup, "name": "오프라인 갱신 테스트"}, nil); err != nil {
		t.Fatal(err)
	}
	editableQueue := f.thread()
	f.start(editableQueue, "대기열 테스트의 초기 메시지")
	f.completed(editableQueue, 1)
	if err := f.client.Call(f.ctx, "thread/name/set", map[string]any{"threadId": editableQueue, "name": "대기 메시지 관리 테스트"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{first.Thread.ID, second, archived, command, catchup, editableQueue} {
		if err := f.client.Call(f.ctx, "thread/archive", map[string]any{"threadId": id}, nil); err != nil {
			t.Fatal(err)
		}
		if id != archived {
			if err := f.client.Call(f.ctx, "thread/unarchive", map[string]any{"threadId": id}, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Populate a real queue without loading this chat. The browser can edit
	// and recall the pending item independently of the separate steer fixture.
	if err := f.client.Call(f.ctx, "thread/queue/add", map[string]any{
		"threadId": editableQueue, "clientUserMessageId": "00000000-0000-4000-8000-000000000072",
		"input": cr.TextInput("큐에서 수정할 원래 요청"),
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.client.Call(f.ctx, "thread/resume", map[string]any{"threadId": second, "excludeTurns": true}, nil); err != nil {
		t.Fatal(err)
	}
	activeTurn := f.start(second, "GUI_FIXTURE_ACTIVE_FOREIGN_BROWSER")
	const relayID = "d2e5be419ab34fd0b00e10b11c4caab1"
	const relayText = "현재 GUI 작업에 전달할 원문\n<문자 그대로> `echo fixture`"
	relayConfig := f.c
	relayConfig.Runtime.WebGUIRelay = "00000000-0000-4000-8000-000000000069"
	if err := saveGUIRelay(relayConfig, relayID, webGUIRelayReceipt{
		Relay: relayConfig.Runtime.WebGUIRelay, Thread: second, Text: relayText,
		ObservedTurn: activeTurn, State: "pending", ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	f.c.Path = filepath.Join(f.cmd.Dir, "web-fixture.toml")
	if err := f.c.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"home": f.cmd.Dir, "config": f.c.Path, "state": filepath.Join(f.c.Storage.StateDir, "web"), "thread": first.Thread.ID, "second": second, "archived": archived, "project": project.Project.ID, "catchup": catchup})
	if err := os.WriteFile(filepath.Join(output, "fixture.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	foreignReleased, progressReleased, catchupSaved, relayAccepted := false, false, false, false
	catalogSteps := []struct{ event, title string }{
		{"login", "원본에서 갱신한 목록 · 접속"},
		{"open", "원본에서 갱신한 목록 · 채팅 진입"},
		{"periodic", "원본에서 갱신한 목록 · 주기"},
	}
	catalogStep := 0
	for {
		select {
		case <-f.ctx.Done():
			t.Fatal("browser fixture timed out")
		case <-ticker.C:
			if catalogStep < len(catalogSteps) {
				step := catalogSteps[catalogStep]
				if _, err := os.Stat(filepath.Join(output, "rename-catalog-"+step.event)); err == nil {
					if err := f.client.Call(f.ctx, "thread/name/set", map[string]any{"threadId": catchup, "name": step.title}, nil); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(output, "catalog-renamed-"+step.event), nil, 0600); err != nil {
						t.Fatal(err)
					}
					catalogStep++
				}
			}
			if _, err := os.Stat(filepath.Join(output, "accept-synthetic-relay")); err == nil && !relayAccepted {
				// Simulate the GUI tool's successful acknowledgement only.
				// The persisted receipt and web history are read by the real bridge.
				check := func(context.Context, config.Config, webGUIRelayReceipt) error { return nil }
				if err := claimGUIRelay(f.ctx, relayConfig, relayID, check, io.Discard); err != nil {
					t.Fatal(err)
				}
				if err := completeGUIRelay(relayConfig, relayConfig.Runtime.WebGUIRelay, relayID, "accepted"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(output, "synthetic-relay-accepted"), []byte("accepted"), 0600); err != nil {
					t.Fatal(err)
				}
				relayAccepted = true
			}
			if _, err := os.Stat(filepath.Join(output, "append-offline-history")); err == nil && !catchupSaved {
				if err := f.client.Call(f.ctx, "thread/resume", map[string]any{"threadId": catchup, "excludeTurns": true}, nil); err != nil {
					t.Fatal(err)
				}
				for i := 1; i <= 18; i++ {
					f.start(catchup, fmt.Sprintf("웹을 닫은 동안 추가된 이력 %02d", i))
					f.completed(catchup, i+1)
				}
				if err := os.WriteFile(filepath.Join(output, "appended-offline-history"), []byte("complete"), 0600); err != nil {
					t.Fatal(err)
				}
				catchupSaved = true
			}
			if _, err := os.Stat(filepath.Join(output, "show-foreign-progress")); err == nil && !progressReleased {
				close(f.foreignUpdate)
				progressReleased = true
			}
			if _, err := os.Stat(filepath.Join(output, "release-foreign")); err == nil && !foreignReleased {
				close(f.releaseForeign)
				foreignReleased = true
			}
			if _, err := os.Stat(filepath.Join(output, "release-model")); err == nil {
				f.once.Do(func() { close(f.release) })
			}
			if _, err := os.Stat(filepath.Join(output, "stop-fixture")); err == nil {
				return
			}
		}
	}
}
