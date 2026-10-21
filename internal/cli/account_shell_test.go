package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cr "github.com/codeasy-digix/codexm/internal/runtime"
	"github.com/codeasy-digix/codexm/internal/shellinit"
)

// Run the real command in a child process so a real zsh can be its parent.
func TestAccountShellProcess(t *testing.T) {
	if os.Getenv("CODEXM_TEST_SHELL_PROCESS") != "1" {
		return
	}
	i := 0
	for i < len(os.Args) && os.Args[i] != "--" {
		i++
	}
	if err := Main(os.Args[i+1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestZshNativeSelectionDefaultAndFailurePreserveWorkerPolicy(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("requires zsh")
	}
	c := accountFixture(t)
	before, _ := os.ReadFile(c.Path)
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	launcher := "#!/bin/sh\nexec " + cr.ShellQuote(self) + " -test.run=^TestAccountShellProcess$ -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "codexm"), []byte(launcher), 0700); err != nil {
		t.Fatal(err)
	}
	adapter := filepath.Join(dir, "adapter.zsh")
	if err := os.WriteFile(adapter, []byte(shellinit.Zsh), 0600); err != nil {
		t.Fatal(err)
	}
	badConfig := filepath.Join(dir, "invalid-config.toml")
	os.WriteFile(badConfig, []byte("schema_version = broken"), 0600)
	// The official command remains a direct executable. Check arguments and status.
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\n[ \"$1\" = 'literal $() ; *' ] || exit 2\n[ \"$CODEX_ACCOUNT\" = account-secondary ] || exit 3\nexit 37\n"), 0700); err != nil {
		t.Fatal(err)
	}
	script := `
set -eu
command codexm --config "$BADCFG" shell-init zsh > "$ADAPTER"
source "$ADAPTER"
(( ! $+functions[codex] ))
codexm --config "$CFG" account account-secondary
[[ "$CODEX_ACCOUNT" == account-secondary && "$CODEX_HOME" == "$ACCOUNTS/account-secondary" && "$CODEX_SQLITE_HOME" == "$SHARED" ]]
zsh -f -c '[[ "$CODEX_ACCOUNT" == account-secondary && "$CODEX_HOME" == "$ACCOUNTS/account-secondary" ]]'
if codex 'literal $() ; *'; then exit 80; else [[ $? -eq 37 ]]; fi
source "$ADAPTER"
[[ "$CODEX_ACCOUNT" == account-secondary ]]
codexm --config "$CFG" account account-main --set-default
[[ "$CODEX_ACCOUNT" == account-secondary && "$CODEX_HOME" == "$ACCOUNTS/account-secondary" ]]
[[ "$(readlink "$SHARED/auth.json")" == "$ACCOUNTS/account-main/auth.json" ]]
if codexm --config "$CFG" account cancelled --login --device; then exit 81; fi
[[ "$CODEX_ACCOUNT" == account-secondary && "$CODEX_HOME" == "$ACCOUNTS/account-secondary" ]]
if codexm --config "$CFG" account account-main --unknown; then exit 82; fi
[[ "$CODEX_ACCOUNT" == account-secondary ]]
codexm --config "$CFG" account --list
env -u CODEX_HOME -u CODEX_ACCOUNT -u CODEX_SQLITE_HOME -u CODEX_SHARED_HOME -u CODEX_ACCOUNTS_DIR zsh -f -c '
  source "$ADAPTER"
  [[ -z "${CODEX_HOME:-}" ]]
  codexm --config "$CFG" account --list
'
`
	cmd := exec.Command("zsh", "-f", "-c", script)
	cmd.Env = append(cr.Env(c, "", ""), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "CODEXM_TEST_SHELL_PROCESS=1", "ADAPTER="+adapter, "BADCFG="+badConfig, "CFG="+c.Path, "ACCOUNTS="+c.Runtime.AccountsDir, "SHARED="+c.Runtime.SharedCodexHome, "TMPDIR="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("parent-shell account flow failed: %v\n%s", err, out)
	}
	for _, want := range []string{"Native Codex in this terminal: account-secondary", "Native Codex in this terminal: account-main", "Native default for new terminals / GUI: account-main"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing account scope %q in %s", want, out)
		}
	}
	after, _ := os.ReadFile(c.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("native commands changed codexm worker policy")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "codexm-shell.*"))
	if len(leftovers) != 0 {
		t.Fatal("shell result scratch directories were not removed")
	}
}
