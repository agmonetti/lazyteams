//go:build !windows

package version

import (
	"os"
	"path/filepath"
	"syscall"
)

// ExecutablePath returns the absolute path of the current running binary,
// resolving symlinks so restart points at the real file.
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

// Restart re-executes binary with the given argv (argv[0] is binary).
// syscall.Exec replaces the current process image, so on success it never
// returns; on failure it returns an error the caller reports.
func Restart(binary string, argv []string) error {
	if err := syscall.Exec(binary, argv, os.Environ()); err != nil {
		return err
	}
	return nil
}
