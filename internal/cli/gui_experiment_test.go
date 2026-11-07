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
