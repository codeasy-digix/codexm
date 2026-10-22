package catalog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeasy-digix/codexm/internal/jobs"
)

func writer(t *testing.T, filename, schema string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0;" + schema); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestReadOnlyCatalogSeesLiveWALWithoutChangingNativeFiles(t *testing.T) {
	home := t.TempDir()
	filename := filepath.Join(home, "state_5.sqlite")
	db := writer(t, filename, `CREATE TABLE threads(id TEXT PRIMARY KEY,title TEXT,name TEXT,cwd TEXT,source TEXT,updated_at INTEGER,updated_at_ms INTEGER,archived INTEGER,model TEXT,reasoning_effort TEXT,tokens_used INTEGER);
INSERT INTO threads VALUES('cli-old','old title','saved title','/work','cli',1,1000,0,'model','high',42);`)
	snapshot := func() [][32]byte {
		var out [][32]byte
		for _, p := range []string{filename, filename + "-wal"} {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, sha256.Sum256(b))
		}
		return out
	}
	before := snapshot()
	list, err := Read(context.Background(), home)
	if err != nil || len(list.Sessions) != 1 {
		t.Fatal(err, len(list.Sessions))
	}
	e := list.Sessions[0]
	if e.Title != "saved title" || e.UpdatedAt != 1000 || e.Tokens != 42 || e.Account != "" || e.Managed {
		t.Fatal(e)
	}
	after := snapshot()
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("native database/WAL changed during read")
		}
	}

	ro, err := OpenReadOnly(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	for _, query := range []string{"UPDATE threads SET title='changed'", "CREATE TABLE manager_jobs(id TEXT)"} {
		if _, err = ro.Exec(query); err == nil {
			t.Fatal("native write accepted", query)
		}
	}
	// A live writer keeps an uncommitted row private; the reader does not block it.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO threads VALUES('app-new','archive',NULL,'/work','vscode',2,2000,1,NULL,NULL,0)`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	list, err = Read(ctx, home)
	if err != nil || len(list.Sessions) != 1 {
		t.Fatal("reader did not see committed snapshot", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal("reader interfered with writer", err)
	}
	list, err = Read(ctx, home)
	if err != nil || len(list.Sessions) != 2 || list.Sessions[0].ID != "app-new" || !list.Sessions[0].Archived {
		t.Fatal("fresh WAL/archived app row missing", err, list)
	}
}

func TestNativeSchemaVersionsAndMissingIndexAreSafe(t *testing.T) {
	home := t.TempDir()
	list, err := Read(context.Background(), home)
	if err != nil || list.Database != "" || len(list.Sessions) != 0 {
		t.Fatal(err, list)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Fatal("missing index created files")
	}
	if db, e := OpenReadOnly(filepath.Join(home, "missing.sqlite")); e == nil {
		db.Close()
		t.Fatal("read-only open created database")
	}
	writer(t, filepath.Join(home, "state_9.sqlite"), `CREATE TABLE threads(id TEXT,title TEXT,updated_at INTEGER); INSERT INTO threads VALUES('older','older',1);`)
	writer(t, filepath.Join(home, "state_10.sqlite"), `CREATE TABLE threads(id TEXT,title TEXT,updated_at INTEGER,new_optional_column TEXT); INSERT INTO threads VALUES('newer','newer',3,'ignored');`)
	list, err = Read(context.Background(), home)
	if err != nil || len(list.Sessions) != 1 || list.Sessions[0].ID != "newer" || list.Sessions[0].UpdatedAt != 3000 {
		t.Fatal("schema version/optional fields", err, list)
	}
	writer(t, filepath.Join(home, "state_11.sqlite"), `CREATE TABLE threads(renamed_id TEXT);`)
	_, err = Read(context.Background(), home)
	if err == nil || !strings.Contains(err.Error(), "database left unchanged") {
		t.Fatal("incompatible schema not reported", err)
	}
}

func TestMergeDoesNotImportOrDiscloseStoredInstructions(t *testing.T) {
	list := List{Sessions: []Entry{{ID: "native", ThreadID: "native", Title: "current native name", UpdatedAt: 2000, Archived: true, Tokens: 7}, {ID: "unmanaged", ThreadID: "unmanaged", Title: "app chat", UpdatedAt: 1000}}}
	job := jobs.Job{ID: "job", ThreadID: "native", Title: "old name", State: "complete", Account: "account-secondary", Managed: true, UpdatedAt: time.UnixMilli(1000), LastInput: "secret instruction", Goal: &jobs.Goal{Objective: "secret objective"}}
	list = Merge(list, []jobs.Job{job, {ID: "group", Title: "group", Kind: "group", UpdatedAt: time.UnixMilli(500)}})
	if len(list.Sessions) != 3 || list.Sessions[0].JobID != "job" || list.Sessions[0].Title != "current native name" || list.Sessions[1].JobID != "" || list.Sessions[1].Account != "" {
		t.Fatal(list)
	}
	b, err := json.Marshal(list)
	if err != nil || strings.Contains(string(b), "secret") {
		t.Fatal("list disclosed conversation/goal content", err)
	}
	if job.Title != "old name" || job.State != "complete" {
		t.Fatal("merge changed persisted job")
	}
}

func TestUntitledCatalogUsesReadableMetadataWithoutChangingIDs(t *testing.T) {
	for _, test := range []struct {
		name, id, nativeName, title, source, nickname, role, path, expected string
		parent, expectedSource                                              string
	}{
		{name: "ordinary", id: "native-untitled", source: "cli", expected: "제목 없는 대화 · native-untitled", expectedSource: "cli"},
		{name: "long-id", id: "00000000-7860-72f5-9977-eabf90c9bec5", source: "vscode", expected: "제목 없는 대화 · 01a12345…89ab", expectedSource: "vscode"},
		{name: "source-nickname", id: "child", source: `{"subagent":{"thread_spawn":{"parent_thread_id":"parent","agent_nickname":"Turing","agent_role":null,"agent_path":"/root/review"}}}`, expected: "하위 작업 · Turing", parent: "parent", expectedSource: "subagent"},
		{name: "column-nickname", id: "column-child", source: `{"subagent":{"thread_spawn":{"agent_nickname":"source-name"}}}`, nickname: "column-name", expected: "하위 작업 · column-name", expectedSource: "subagent"},
		{name: "role", id: "role-child", source: `{"subagent":{"thread_spawn":{"agent_role":"reviewer"}}}`, expected: "하위 작업 · reviewer", expectedSource: "subagent"},
		{name: "string-variant", id: "review-child", source: `{"subagent":"review"}`, expected: "하위 작업 · review", expectedSource: "subagent"},
		{name: "legacy-variant", id: "legacy-child", source: `{"subAgent":{"threadSpawn":{"parent_thread_id":"parent","agent_nickname":"Ada"}}}`, expected: "하위 작업 · Ada", parent: "parent", expectedSource: "subagent"},
		{name: "child-without-label", id: "unnamed-child", source: `{"subagent":{"thread_spawn":{"parent_thread_id":"parent"}}}`, expected: "하위 작업 · unnamed-child", parent: "parent", expectedSource: "subagent"},
		{name: "actual-name", id: "named", nativeName: "Actual native name", title: "Older title", source: `{"subagent":"review"}`, nickname: "Unused label", expected: "Actual native name", expectedSource: "subagent"},
		{name: "actual-title", id: "titled", title: "Actual native title", source: "cli", expected: "Actual native title", expectedSource: "cli"},
		{name: "blank-name", id: "whitespace", nativeName: " \n\t", title: "Actual native title", source: "cli", expected: "Actual native title", expectedSource: "cli"},
		{name: "malformed-source", id: "bad-json", source: `{"subagent":`, expected: "제목 없는 대화 · bad-json", expectedSource: `{"subagent":`},
		{name: "other-json-source", id: "remote", source: `{"remote":{"host":"example"}}`, expected: "제목 없는 대화 · remote", expectedSource: `{"remote":{"host":"example"}}`},
		{name: "invalid-subagent", id: "false-child", source: `{"subagent":false}`, expected: "제목 없는 대화 · false-child", expectedSource: `{"subagent":false}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			db := writer(t, filepath.Join(home, "state_5.sqlite"), `CREATE TABLE threads(id TEXT PRIMARY KEY,name TEXT,title TEXT,source TEXT,agent_nickname TEXT,agent_role TEXT,agent_path TEXT);`)
			if _, err := db.Exec(`INSERT INTO threads VALUES(?,?,?,?,?,?,?)`, test.id, test.nativeName, test.title, test.source, test.nickname, test.role, test.path); err != nil {
				t.Fatal(err)
			}
			list, err := Read(context.Background(), home)
			if err != nil || len(list.Sessions) != 1 {
				t.Fatal(err, len(list.Sessions))
			}
			e := list.Sessions[0]
			if e.ID != test.id || e.ThreadID != test.id || e.Title != test.expected || e.Source != test.expectedSource || e.ParentThreadID != test.parent {
				t.Fatalf("unexpected projection: %#v", e)
			}
			if test.name == "source-nickname" && (e.AgentNickname != "Turing" || e.AgentPath != "/root/review") {
				t.Fatal("source agent metadata lost", e)
			}
			if e.Managed || e.JobID != "" {
				t.Fatal("readable fallback imported the conversation")
			}
			var storedName, storedTitle, storedSource string
			if err = db.QueryRow(`SELECT name,title,source FROM threads WHERE id=?`, test.id).Scan(&storedName, &storedTitle, &storedSource); err != nil || storedName != test.nativeName || storedTitle != test.title || storedSource != test.source {
				t.Fatal("fallback modified native metadata", err)
			}
		})
	}
}

func TestReadableFallbackKeepsEveryRowAndOptionalSchema(t *testing.T) {
	home := t.TempDir()
	writer(t, filepath.Join(home, "state_5.sqlite"), `CREATE TABLE threads(id TEXT,title TEXT);
INSERT INTO threads VALUES('same-id',''),('same-id',''),('distinct-a','same title'),('distinct-b','same title');`)
	list, err := Read(context.Background(), home)
	if err != nil || len(list.Sessions) != 4 {
		t.Fatal("catalog hid or deleted native rows", err, len(list.Sessions))
	}
	ids := map[string]int{}
	for _, entry := range list.Sessions {
		ids[entry.ID]++
		if entry.ThreadID != entry.ID {
			t.Fatal("fallback rewrote conversation identity")
		}
	}
	if ids["same-id"] != 2 || ids["distinct-a"] != 1 || ids["distinct-b"] != 1 {
		t.Fatal("fallback deduplicated or invented conversation IDs", ids)
	}
}

func TestFallbackLabelsAreBoundedAndControlFree(t *testing.T) {
	nickname := "  Agent\n\t\x1b[31m\u202e " + strings.Repeat("가", 100)
	title := unnamedTitle(Entry{ID: "native", Source: "subagent", AgentNickname: nickname})
	if !strings.HasPrefix(title, "하위 작업 · Agent ") || strings.ContainsAny(title, "\x1b\n\r\t\u202e") || len([]rune(strings.TrimPrefix(title, "하위 작업 · "))) > 64 {
		t.Fatal("agent fallback label is not safe and bounded")
	}
}

func TestMergeRetainsAgentMetadataAndPrefersRealTitlesOverFallback(t *testing.T) {
	list := List{Sessions: []Entry{{ID: "native", ThreadID: "native", Title: "하위 작업 · Turing", Source: "subagent", ParentThreadID: "parent", AgentNickname: "Turing", AgentRole: "reviewer", AgentPath: "/root/review", nativeUntitled: true}}}
	list = Merge(list, []jobs.Job{{ID: "job", ThreadID: "native", Title: "Named managed conversation", Managed: true}})
	entry := list.Sessions[0]
	if entry.Title != "Named managed conversation" || entry.ParentThreadID != "parent" || entry.AgentNickname != "Turing" || entry.AgentRole != "reviewer" || entry.AgentPath != "/root/review" {
		t.Fatal("decorating managed metadata lost an actual title or agent relationship", entry)
	}
	list = Merge(list, []jobs.Job{{ID: "job", ThreadID: "native", Title: "Updated managed title", Managed: true}})
	if list.Sessions[0].Title != "Updated managed title" {
		t.Fatal("a cached fallback froze the previous managed title")
	}
}
