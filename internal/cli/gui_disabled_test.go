//go:build !gui_experiment

package cli

import "testing"

func TestGUIExperimentIsAbsentFromNormalBuild(t *testing.T) {
	for _, command := range []string{"gui", "daemon"} {
		if handled, err := runGUIExperiment(command, []string{"send"}, ""); handled || err != nil || guiExperimentHelp != "" {
			t.Fatal("normal CLI must not activate the GUI/daemon experiment")
		}
	}
}
