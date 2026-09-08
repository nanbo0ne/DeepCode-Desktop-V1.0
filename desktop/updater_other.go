//go:build !windows

package main

import (
	"errors"
	"io"
	"os/exec"
)

func lockUpdatePackage(*pendingDesktopUpdate) (io.Closer, error) {
	return nil, errors.New("in-place installation is only available on Windows")
}

func isInstalledDesktop() bool { return false }

// installerCommand exists only so updater.go compiles off Windows; applyWindows is
// never dispatched there (see updater_app.go).
func installerCommand(name, _ string) *exec.Cmd {
	return exec.Command(name)
}
