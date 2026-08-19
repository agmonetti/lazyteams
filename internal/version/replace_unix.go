//go:build !windows

package version

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// ReplaceBinary atomically replaces the file at dest with the content at src.
// On Unix we can rename over a running executable because the inode swap
// leaves the running process mapped to the old file. To survive src and dest
// being on different filesystems (e.g. /tmp vs /usr/local/bin), the content is
// first copied into a staging file next to dest, then renamed into place.
func ReplaceBinary(src, dest string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	mode := info.Mode()

	staged, err := os.CreateTemp(filepath.Dir(dest), ".lazyteams-replace-*")
	if err != nil {
		return err
	}
	stagedName := staged.Name()
	defer os.Remove(stagedName)

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	if _, err := io.Copy(staged, in); err != nil {
		in.Close()
		staged.Close()
		return err
	}
	in.Close()
	if err := staged.Chmod(mode); err != nil {
		staged.Close()
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}

	if err := os.Rename(stagedName, dest); err != nil {
		return fmt.Errorf("replacing %s: %w", dest, err)
	}
	return nil
}

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
