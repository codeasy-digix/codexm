package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/codeasy-digix/codexm/internal/config"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestThousandSessionPageIsBounded(t *testing.T) {
	c := config.Defaults()
	c.Path = filepath.Join(t.TempDir(), "config.toml")
	c.Runtime.SharedCodexHome = t.TempDir()
	c.Storage.StateDir = t.TempDir()
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(c.Runtime.SharedCodexHome, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE threads(id TEXT PRIMARY KEY,title TEXT,cwd TEXT,source TEXT,updated_at INTEGER,archived INTEGER);`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := range 1000 {
		if _, err = tx.Exec("INSERT INTO threads VALUES(?,?,?,?,?,?)", fmt.Sprintf("thread-%04d", i), fmt.Sprintf("title %d", i), "/work", "cli", i, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	app := New(context.Background(), c, "")
	defer app.Close()
	for i := range 6 {
		start := time.Now()
		result, err := app.Call(context.Background(), "", "sessions.list", "", json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(start)
		b, _ := json.Marshal(result)
		var page struct {
			Sessions []json.RawMessage `json:"sessions"`
			Total    int               `json:"total"`
			Cursor   string            `json:"cursor"`
		}
		if err = json.Unmarshal(b, &page); err != nil || len(page.Sessions) != 50 || page.Total != 1000 || page.Cursor == "" {
			t.Fatal("invalid bounded page", page.Total, len(page.Sessions), err)
		}
		if i > 0 {
			t.Logf("warm 1000-session first page: %s (%d bytes)", elapsed, len(b))
			// Measure latency explicitly on a warm, otherwise idle test host.
			// Parallel race/build workloads must not make correctness tests flaky.
			if os.Getenv("CODEXM_PERF_TEST") == "1" && elapsed > time.Second {
				t.Fatal("warm page exceeded 1 second")
			}
		}
	}
}
