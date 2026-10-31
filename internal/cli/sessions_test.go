package cli

import (
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeasy-digix/codexm/internal/config"
)

func TestSessionListWorksWithoutManagerOrSupervisor(t *testing.T) {
	root := t.TempDir()
	c := config.Defaults()
	c.Path = filepath.Join(root, "config.toml")
	c.Runtime.SharedCodexHome = filepath.Join(root, "native")
	c.Storage.StateDir = filepath.Join(root, "manager-state")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	os.Mkdir(c.Runtime.SharedCodexHome, 0700)
	db, err := sql.Open("sqlite", filepath.Join(c.Runtime.SharedCodexHome, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE threads(id TEXT,title TEXT,source TEXT,archived INTEGER,updated_at INTEGER);
INSERT INTO threads VALUES('existing-cli','한국어 대화','cli',0,2),('existing-app','old app chat','vscode',1,1);`); err != nil {
		t.Fatal(err)
	}
	read := func(args ...string) string {
		t.Helper()
		old := os.Stdout
		r, w, e := os.Pipe()
		if e != nil {
			t.Fatal(e)
		}
		os.Stdout = w
		e = Main(append([]string{"--config", c.Path, "list"}, args...))
		w.Close()
		os.Stdout = old
		b, _ := io.ReadAll(r)
		r.Close()
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	if s := read("--archived"); !strings.Contains(s, "Codex conversations: 2") || !strings.Contains(s, "existing-cli") || !strings.Contains(s, "existing-app") || strings.Contains(s, "{\n") {
		t.Fatal(s)
	}
	if s := read("--json"); !strings.Contains(s, "existing-cli") || strings.Contains(s, "existing-app") {
		t.Fatal(s)
	}
	if s := read("--search", "한국어"); !strings.Contains(s, "한국어 대화") || strings.Contains(s, "existing-app") {
		t.Fatal(s)
	}
	if err := Main([]string{"--config", c.Path, "manage", "existing-cli"}); err == nil {
		t.Fatal("retired management command accepted")
	}
	if _, err := os.Stat(c.StateDir()); !os.IsNotExist(err) {
		t.Fatal("listing created manager DB/supervisor state", err)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM threads").Scan(&count); err != nil || count != 2 {
		t.Fatal("native conversations changed", err, count)
	}
}
