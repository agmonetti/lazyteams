//go:build windows

package version

import (
	"os"
	"os/exec"
	"path/filepath"
)

// ExecutablePath returns the current running binary path.
func ExecutablePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved, nil
	}
	return p, nil
}

// Restart launches the specified executable with the given args on Windows.
func Restart(binary string, args []string) error {
	cmd := exec.Command(binary, args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
