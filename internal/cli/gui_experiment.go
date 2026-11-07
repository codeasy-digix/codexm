//go:build gui_experiment

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

const guiExperimentHelp = "\nExperimental build: codexm gui [help|list|open|new|send|status]\n"

func runGUIExperiment(command string, args []string, configPath string) (bool, error) {
	if command != "gui" {
		return false, nil
	}
	if configPath != "" {
		return true, errors.New("the GUI experiment uses the native desktop home, not --config or a selected CLI account")
	}
	self, err := os.Executable()
	if err != nil {
		return true, err
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return true, err
	}
	binary := filepath.Join(filepath.Dir(self), "codexm-gui")
	info, err := os.Stat(binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return true, errors.New("matching codexm-gui is missing; build with scripts/build-gui-experiment.sh (installed codexm is unchanged)")
	}
	return true, syscall.Exec(binary, append([]string{binary}, args...), os.Environ())
}
