package nativecli

import (
	"encoding/binary"
	"os"
	"reflect"
	"testing"
)

func TestExecModeUsesArgumentsNotPromptWords(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"codex", "--no-daemon", "exec", "resume", fixtureID}, true},
		{[]string{"codex", "-c", `model="exec"`, "--model=fixture", "e", "a prompt"}, true},
		{[]string{"codex", "-cmode=fixture", "-mfixture", "exec", "--json", "app-server"}, true},
		{[]string{"codex", "-c", "exec", "an interactive prompt"}, false},
		{[]string{"codex", "resume", fixtureID, "exec"}, false},
		{[]string{"codex", "app-server", "--exec", "exec"}, false},
		{[]string{"codex", "--", "exec"}, false},
		{[]string{"codex", "a prompt containing exec"}, false},
		{[]string{"codex", "--future-option", "exec"}, false},
		{[]string{"codex", "--image", "fixture.png", "exec"}, false},
		{[]string{"codex", "--model"}, false},
		{[]string{"codex", "--no-daemon=exec", "exec"}, false},
		{nil, false},
	} {
		if got := isExecArguments(tc.args); got != tc.want {
			t.Errorf("args=%q: got %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestDarwinArgumentsExcludeEnvironmentAndPreserveBoundaries(t *testing.T) {
	args := []string{"/fixture/codex", "--no-daemon", "exec", "", "a prompt with spaces"}
	raw := make([]byte, 4)
	binary.NativeEndian.PutUint32(raw, uint32(len(args)))
	raw = append(raw, []byte("/fixture/codex\x00\x00\x00")...)
	for _, arg := range args {
		raw = append(raw, arg...)
		raw = append(raw, 0)
	}
	raw = append(raw, []byte("PRIVATE_FIXTURE=must-not-enter-argv\x00")...)
	got, err := decodeDarwinArguments(raw)
	if err != nil || !reflect.DeepEqual(got, args) {
		t.Fatalf("argument boundaries changed: %q %v", got, err)
	}
	for _, bad := range [][]byte{nil, {0, 0, 0, 0}, raw[:8], raw[:len(raw)-45]} {
		if _, err := decodeDarwinArguments(bad); err == nil {
			t.Fatal("truncated arguments were accepted")
		}
	}
}

func TestNativeProcessArgumentsReadCurrentInvocation(t *testing.T) {
	got, err := processArguments(os.Getpid())
	if err != nil || !reflect.DeepEqual(got, os.Args) {
		t.Fatalf("native argument read differs from invocation: %v", err)
	}
}
