//go:build gui_experiment

package cli

import "testing"

func TestGUIExperimentDoesNotInterceptNativeCommands(t *testing.T) {
	for _, command := range []string{"", "resume", "account", "handoff", "terminal", "list"} {
		if handled, err := runGUIExperiment(command, nil, ""); handled || err != nil {
			t.Fatalf("changed existing command %q", command)
		}
	}
	if handled, err := runGUIExperiment("gui", nil, "/fixture/config.toml"); !handled || err == nil {
		t.Fatal("GUI mode must reject CLI account/config overrides")
	}
}

func TestGUIHybridKeepsExplicitAccountIntent(t *testing.T) {
	for _, args := range [][]string{
		{"resume"},
		{"resume", "fixture", "--account", ""},
		{"resume", "fixture", "--plan", "--terminal"},
		{"new", "--message-stdin"},
		{"new", "--force"},
	} {
		if _, err := parseGUIHybrid(args); err == nil {
			t.Fatalf("accepted unsupported input %q", args)
		}
	}
	o, err := parseGUIHybrid([]string{"resume", "fixture", "--account", "default", "--plan"})
	if err != nil || o.account != "default" || !o.plan {
		t.Fatal("explicit default must still require CLI routing", o, err)
	}
	o, err = parseGUIHybrid([]string{"resume", "fixture"})
	if err != nil || o.account != "" {
		t.Fatal("omitted account must keep GUI priority", o, err)
	}
}
