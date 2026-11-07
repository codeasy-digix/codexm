//go:build !gui_experiment

package cli

const guiExperimentHelp = ""

func runGUIExperiment(_ string, _ []string, _ string) (bool, error) { return false, nil }
