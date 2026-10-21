package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	cr "github.com/codeasy-digix/codexm/internal/runtime"
)

func TestOfficialNativeTerminalAccountUsesSharedDatabaseWithoutArgumentInjection(t *testing.T) {
	if os.Getenv("CODEXM_NATIVE_TEST") != "1" {
		t.Skip("requires isolated official CLI integration")
	}
	binary := os.Getenv("CODEXM_NATIVE_RUNTIME")
	if binary == "" {
		t.Fatal("official runtime missing")
	}
	c := nativeDefaultFixture(t)
	if err := cr.EnsureAccountHome(c, "other"); err != nil {
		t.Fatal(err)
	}
	other := syntheticChatGPT("other-id")
	if err := os.WriteFile(filepath.Join(c.Runtime.AccountsDir, "other", "auth.json"), other, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SetNativeDefault(c, "other"); err != nil {
		t.Fatal(err)
	}
	// Deliberately break only the isolated global login after linking it. A new
	// native launch must still use the selected named file, without OAuth/network.
	if err := os.WriteFile(filepath.Join(c.Runtime.AccountsDir, "other", "auth.json"), []byte("isolated global canary"), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(c.Runtime.SharedCodexHome, "config.toml"))
	if err := ValidateNativeAccount(c, "chosen"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	login := exec.CommandContext(ctx, binary, "--no-daemon", "login", "status")
	login.Env = cr.Env(c, "chosen", "")
	if out, err := login.CombinedOutput(); err != nil || !bytes.Contains(out, []byte("ChatGPT")) {
		t.Fatal("official native login status did not use the selected named file", err)
	}
	// These are ordinary official arguments. Only CODEX_HOME/CODEX_SQLITE_HOME
	// choose auth and history; no -c sqlite_home or native command wrapper is used.
	cmd := exec.CommandContext(ctx, binary, "--no-daemon", "app-server", "--listen", "stdio://")
	cmd.Env = cr.Env(c, "chosen", "")
	cmd.Dir = c.Runtime.SharedCodexHome
	p, err := cr.StartStdio(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		p.Close()
		select {
		case <-p.ProcessDone:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-p.ProcessDone
		}
	}()
	var settings struct {
		Config map[string]json.RawMessage `json:"config"`
	}
	if err = p.Call(ctx, "config/read", map[string]any{"includeLayers": false}, &settings); err != nil {
		t.Fatal(err)
	}
	if err = p.Call(ctx, "thread/list", map[string]any{"limit": 1}, nil); err != nil {
		t.Fatal(err)
	}
	dbs, _ := filepath.Glob(filepath.Join(c.Runtime.SharedCodexHome, "state_*.sqlite"))
	private, _ := filepath.Glob(filepath.Join(c.Runtime.AccountsDir, "chosen", "state_*.sqlite"))
	if len(dbs) == 0 || len(private) != 0 {
		t.Fatal("official native environment split the shared conversation database")
	}
	if got := CurrentNickname(c, filepath.Join(c.Runtime.AccountsDir, "chosen"), ""); got != "chosen" {
		t.Fatal("terminal selection was lost")
	}
	if got := CurrentNickname(c, c.Runtime.SharedCodexHome, ""); got != "other" {
		t.Fatal("terminal selection changed native default")
	}
	after, _ := os.ReadFile(filepath.Join(c.Runtime.SharedCodexHome, "config.toml"))
	if !bytes.Equal(before, after) {
		t.Fatal("native terminal account selection rewrote shared configuration")
	}
}
