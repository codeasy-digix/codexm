package nativecli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in browser fixture kept alive only while the external Playwright job
// runs. It uses the real official runtime and a local model, never user chats.
func TestWebChatBrowserFixture(t *testing.T) {
	output := os.Getenv("CODEXM_WEB_BROWSER_FIXTURE")
	if !filepath.IsAbs(output) {
		t.Skip("requires an isolated browser fixture output directory")
	}
	f := newGUIControlFixtureDuration(t, "stdio", 45*time.Minute)
	f.mu.Lock()
	f.responseText = "## 작업을 확인했습니다\n\n브라우저에서 표시하는 **구조화된 응답**입니다.\n\n- 최근 대화부터 불러옵니다.\n- 명령 실행 과정은 접어서 표시합니다.\n\n```rust\nfn main() { println!(\"CodexM\"); }\n```\n\n<script>window.__unsafe = true</script>"
	f.commandFixture = true
	f.mu.Unlock()
	var project struct{ Project struct{ ID string } }
	if err := f.client.Call(f.ctx, "project/create", map[string]any{"name": "브라우저 테스트 프로젝트", "roots": []map[string]string{{"path": f.cmd.Dir}}, "idempotencyKey": "00000000-0000-4000-8000-000000000061"}, &project); err != nil {
		t.Fatal(err)
	}
	var first struct{ Thread struct{ ID string } }
	if err := f.client.Call(f.ctx, "thread/start", map[string]any{"cwd": f.cmd.Dir, "historyMode": "paginated", "projectId": project.Project.ID}, &first); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 26; i++ {
		f.start(first.Thread.ID, fmt.Sprintf("페이지 이력 %02d", i))
		f.completed(first.Thread.ID, i)
	}
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
	for _, id := range []string{first.Thread.ID, second, archived, command} {
		if err := f.client.Call(f.ctx, "thread/archive", map[string]any{"threadId": id}, nil); err != nil {
			t.Fatal(err)
		}
		if id != archived {
			if err := f.client.Call(f.ctx, "thread/unarchive", map[string]any{"threadId": id}, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := f.client.Call(f.ctx, "thread/resume", map[string]any{"threadId": second, "excludeTurns": true}, nil); err != nil {
		t.Fatal(err)
	}
	f.start(second, "GUI_FIXTURE_ACTIVE_FOREIGN_BROWSER")
	f.c.Path = filepath.Join(f.cmd.Dir, "web-fixture.toml")
	if err := f.c.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"home": f.cmd.Dir, "config": f.c.Path, "state": filepath.Join(f.c.Storage.StateDir, "web"), "thread": first.Thread.ID, "second": second, "archived": archived, "project": project.Project.ID})
	if err := os.WriteFile(filepath.Join(output, "fixture.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	foreignReleased := false
	for {
		select {
		case <-f.ctx.Done():
			t.Fatal("browser fixture timed out")
		case <-ticker.C:
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
