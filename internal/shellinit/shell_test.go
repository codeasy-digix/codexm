package shellinit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReloadRetiresOnlyLegacyCodexWrapper(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("requires zsh")
	}
	dir := t.TempDir()
	adapter := filepath.Join(dir, "shell.zsh")
	if err := os.WriteFile(adapter, []byte(Zsh), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte("#!/bin/sh\nprintf 'native:<%s>\\n' \"$1\"\nexit 17\n"), 0700); err != nil {
		t.Fatal(err)
	}
	script := `
codex() { command codex-accounts __native "$@"; }
codex_account() { return 93; }
export CODEX_HOME=/existing/account CODEX_ACCOUNT=chosen
source "$ADAPTER"
(( ! $+functions[codex] && ! $+functions[codex_account] )) || exit 71
[[ "$CODEX_HOME:$CODEX_ACCOUNT" == /existing/account:chosen ]] || exit 72
codex 'preserve spaces'
[[ $? -eq 17 ]] || exit 73
codex() { print -r -- custom-user-function; }
source "$ADAPTER"
[[ "$(codex)" == custom-user-function ]] || exit 74
`
	cmd := exec.Command("zsh", "-f", "-c", script)
	cmd.Env = append(os.Environ(), "ADAPTER="+adapter, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("legacy retirement: %v: %s", err, out)
	}
}

func TestResultRefusesCollisionsAndUnsafeParents(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	target := filepath.Join(dir, "keep")
	os.WriteFile(target, []byte("preserved"), 0600)
	for _, name := range []string{"keep", "symlink"} {
		if name == "symlink" {
			os.Symlink(target, filepath.Join(dir, name))
		}
		t.Setenv(ResultEnv, filepath.Join(dir, name))
		if f, err := OpenResult(); err == nil {
			f.Close()
			t.Fatal("result overwrote an existing path")
		}
	}
	b, _ := os.ReadFile(target)
	if string(b) != "preserved" {
		t.Fatal("result collision damaged existing contents")
	}
	os.Chmod(dir, 0755)
	t.Setenv(ResultEnv, filepath.Join(dir, "new"))
	if f, err := OpenResult(); err == nil {
		f.Close()
		t.Fatal("non-private parent accepted")
	}
}

func TestZshTreatsReceiptAsDataAndRejectsPartialAndFailedResults(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("requires zsh")
	}
	dir := t.TempDir()
	adapter := filepath.Join(dir, "shell.zsh")
	os.WriteFile(adapter, []byte(Zsh), 0600)
	fake := `#!/bin/sh
printf 'stdout stays live\n'
printf 'stderr stays live\n' >&2
case "$1" in
  literal) printf 'codexm-native-account-v1\000chosen\000/%s\000/shared\000' 'literal $(touch INJECTED); ` + "`touch INJECTED`" + ` "quoted"' > "$CODEXM_SHELL_RESULT" ;;
  partial) printf 'codexm-native-account-v1\000chosen\000/new' > "$CODEXM_SHELL_RESULT" ;;
  failed) printf 'codexm-native-account-v1\000chosen\000/new\000/shared\000' > "$CODEXM_SHELL_RESULT"; exit 47 ;;
  extra) printf 'codexm-native-account-v1\000chosen\000/new\000/shared\000trailing' > "$CODEXM_SHELL_RESULT" ;;
  *) printf 'codexm-native-account-v1\000chosen\000/new\000/shared\000' > "$CODEXM_SHELL_RESULT" ;;
esac
`
	os.WriteFile(filepath.Join(dir, "codexm"), []byte(fake), 0700)
	script := `
set -eu
source "$ADAPTER"
export CODEX_HOME=/old CODEX_ACCOUNT=old CODEX_SQLITE_HOME=/old-shared
for arg in partial extra failed; do
  if codexm "$arg"; then exit 70; else
    result_exit=$?
    if [[ "$arg" == failed ]]; then [[ $result_exit -eq 47 ]]; fi
  fi
  [[ "$CODEX_HOME" == /old && "$CODEX_ACCOUNT" == old && "$CODEX_SQLITE_HOME" == /old-shared ]]
done
codexm literal
[[ "$CODEX_HOME" == '/literal $(touch INJECTED); ` + "`touch INJECTED`" + ` "quoted"' ]]
[[ ! -e INJECTED ]]
export CODEX_ACCOUNT=old CODEX_SQLITE_HOME=/old-shared
readonly CODEX_HOME
if codexm readonly; then exit 71; fi
[[ "$CODEX_ACCOUNT" == old && "$CODEX_SQLITE_HOME" == /old-shared ]]
`
	cmd := exec.Command("zsh", "-f", "-c", script)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ADAPTER="+adapter, "TMPDIR="+dir, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("shell data/rollback flow failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "stdout stays live") || !strings.Contains(string(out), "stderr stays live") {
		t.Fatal("shell captured command streams")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "codexm-shell.*"))
	if len(leftovers) != 0 {
		t.Fatal("shell did not clean its result files")
	}
}
