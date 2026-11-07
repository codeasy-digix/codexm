//go:build !gui_experiment

package cli

import "testing"

func TestGUIExperimentIsAbsentFromNormalBuild(t *testing.T) {
	if handled, err := runGUIExperiment("gui", []string{"send"}, ""); handled || err != nil || guiExperimentHelp != "" {
		t.Fatal("normal CLI must not activate the GUI experiment")
	}
}
