package nativecli

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/codeasy-digix/codexm/internal/config"
)

// Mixed pre-migration desktop data: GUI identities and explicit assignments,
// native sections, older indexed chats, exec writers and attached child chats.
func desktopCatalogFixture(t *testing.T) (*WebChat, string) {
	t.Helper()
	c := config.Defaults()
	c.Runtime.SharedCodexHome = t.TempDir()
	c.Runtime.AccountsDir = t.TempDir()
	c.Storage.StateDir = t.TempDir()
	c.Runtime.CodexBinary = "/must-not-start-for-a-catalog"
	db, err := sql.Open("sqlite", filepath.Join(c.Runtime.SharedCodexHome, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE threads(id TEXT PRIMARY KEY,name TEXT,title TEXT,cwd TEXT,source TEXT,updated_at INTEGER,created_at INTEGER,archived INTEGER,project_id TEXT,thread_section_id TEXT,section_position INTEGER,attached_thread_parent_id TEXT);
 CREATE TABLE projects(id TEXT PRIMARY KEY,name TEXT,position INTEGER);CREATE TABLE project_roots(project_id TEXT,path TEXT,position INTEGER);CREATE TABLE thread_sections(id TEXT PRIMARY KEY,name TEXT);
 INSERT INTO projects VALUES('native-a','Main',0),('native-b','Nested',1);
 INSERT INTO project_roots VALUES('native-a','/workspace/main',0),('native-b','/workspace/main/nested',0);
 INSERT INTO thread_sections VALUES('01984de2-8f74-7c91-a3b2-5c5e937cf318','Pinned');`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 145; i++ {
		id := fmt.Sprintf("10000000-0000-4000-8000-%012d", i)
		source := "vscode"
		if i == 2 {
			source = "exec"
		}
		if i == 144 {
			source = `{"subagent":{"thread_spawn":{"parent_thread_id":"10000000-0000-4000-8000-000000000000"}}}`
		}
		cwd := "/workspace/main"
		if i == 3 {
			cwd += "/nested"
		}
		archived := i >= 135 && i < 143
		project, section, parent := "", "", ""
		if i == 2 {
			project = "native-a"
		}
		if i < 3 {
			section = "01984de2-8f74-7c91-a3b2-5c5e937cf318"
		}
		if i == 143 {
			parent = "10000000-0000-4000-8000-000000000000"
		}
		_, err = db.Exec(`INSERT INTO threads VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, fmt.Sprintf("Saved chat %03d", i), "old title", cwd, source, 1700000000+i, 1600000000+i, archived, project, section, 2-i, parent)
		if err != nil {
			t.Fatal(err)
		}
	}
	g := map[string]any{"local-projects": map[string]any{"gui-a": map[string]any{"id": "gui-a", "name": "Main", "rootPaths": []string{"/workspace/main"}}, "gui-b": map[string]any{"id": "gui-b", "name": "Nested", "rootPaths": []string{"/workspace/main/nested"}}}, "project-order": []string{"gui-b", "gui-a"}, "app-server-project-id-by-legacy-project-id-by-host": map[string]any{"local:" + c.Runtime.SharedCodexHome: map[string]string{"gui-a": "native-a", "gui-b": "native-b"}}, "thread-project-assignments": map[string]any{"10000000-0000-4000-8000-000000000000": map[string]string{"projectKind": "local", "projectId": "gui-b"}, "10000000-0000-4000-8000-000000000004": map[string]string{"projectKind": "local", "projectId": "gui-b"}}, "thread-project-membership-host-ids": map[string]string{"10000000-0000-4000-8000-000000000004": "remote"}, "projectless-thread-ids": []string{"10000000-0000-4000-8000-000000000001"}}
	b, _ := json.Marshal(g)
	if err = os.WriteFile(filepath.Join(c.Runtime.SharedCodexHome, ".codex-global-state.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	w := NewWebChat(context.Background(), c)
	t.Cleanup(w.Close)
	return w, c.Runtime.SharedCodexHome
}
func parityPage(t *testing.T, w *WebChat, p webParams) map[string]any {
	t.Helper()
	r, err := w.catalog(context.Background(), nil, p)
	if err != nil {
		t.Fatal(err)
	}
	return r.(map[string]any)
}
func TestWebDesktopCatalogParityAndCompletePagination(t *testing.T) {
	w, home := desktopCatalogFixture(t)
	before, _ := os.ReadFile(filepath.Join(home, "state_5.sqlite"))
	page := parityPage(t, w, webParams{Limit: 17})
	projects := page["projects"].([]WebProject)
	if len(projects) != 2 || projects[0].ID != "gui-b" || projects[0].NativeID != "native-b" {
		t.Fatal("GUI project aliases/order lost", projects)
	}
	pins := page["pinned"].([]WebThread)
	if len(pins) != 3 || pins[0].ID != "10000000-0000-4000-8000-000000000002" {
		t.Fatal("native exec pin/order lost", pins)
	}
	ids := map[string]WebThread{}
	for n := 0; ; n++ {
		if n > 10 {
			t.Fatal("cursor did not finish")
		}
		for _, r := range page["threads"].([]WebThread) {
			if _, ok := ids[r.ID]; ok {
				t.Fatal("duplicate page ID", r.ID)
			}
			ids[r.ID] = r
		}
		next := page["nextCursor"].(*string)
		if next == nil {
			break
		}
		page = parityPage(t, w, webParams{Limit: 17, Cursor: *next})
	}
	if len(ids) != 135 {
		t.Fatalf("missing history: got %d want 135", len(ids))
	}
	for id, project := range map[string]string{"10000000-0000-4000-8000-000000000000": "gui-b", "10000000-0000-4000-8000-000000000001": "", "10000000-0000-4000-8000-000000000002": "gui-a", "10000000-0000-4000-8000-000000000003": "gui-b", "10000000-0000-4000-8000-000000000004": ""} {
		if ids[id].Project != project {
			t.Fatal("incorrect membership", id, ids[id].Project, project)
		}
	}
	filtered := parityPage(t, w, webParams{Project: "gui-b"})
	if len(filtered["threads"].([]WebThread)) != 2 {
		t.Fatal("legacy GUI membership did not filter")
	}
	archived := parityPage(t, w, webParams{Archived: true})
	if len(archived["threads"].([]WebThread)) != 8 {
		t.Fatal("archived chats omitted")
	}
	if len(w.connections) != 0 {
		t.Fatal("passive catalog started an official server")
	}
	after, _ := os.ReadFile(filepath.Join(home, "state_5.sqlite"))
	if !reflect.DeepEqual(before, after) {
		t.Fatal("catalog changed native data")
	}
}
func TestWebCatalogRejectsCursorForAnotherViewAndCorruptMetadata(t *testing.T) {
	w, home := desktopCatalogFixture(t)
	p := parityPage(t, w, webParams{Limit: 1})
	cursor := p["nextCursor"].(*string)
	if _, err := w.catalog(context.Background(), nil, webParams{Cursor: *cursor, Archived: true}); err == nil {
		t.Fatal("accepted stale filter cursor")
	}
	if err := os.WriteFile(filepath.Join(home, ".codex-global-state.json"), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := w.catalog(context.Background(), nil, webParams{}); err == nil {
		t.Fatal("corrupt GUI state became empty success")
	}
}
