//go:build !windows

package version

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func ownedByCurrentUser(path string, fi os.FileInfo) error {
	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("refusing %s: file is not owned by the current user", path)
	}
	return nil
}

var renameInstalledBinary = os.Rename

// stageBinary writes a complete, executable copy beside its destination.
func stageBinary(src string, target updateTarget) (string, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(target.path), ".lazyteams-stage-*")
	if err != nil {
		return "", err
	}
	defer func() {
		out.Close()
		if err != nil {
			os.Remove(out.Name())
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return "", err
	}
	if err = out.Chmod(target.mode); err != nil {
		return "", err
	}
	if err = out.Sync(); err != nil {
		return "", err
	}
	if err = out.Close(); err != nil {
		return "", err
	}
	return out.Name(), nil
}

func installBinaryPair(self updateTarget, tuiSrc string, auth updateTarget, authSrc string) error {
	tuiStage, err := stageBinary(tuiSrc, self)
	if err != nil {
		return fmt.Errorf("staging TUI: %w", err)
	}
	defer os.Remove(tuiStage)
	authStage, err := stageBinary(authSrc, auth)
	if err != nil {
		return fmt.Errorf("staging auth helper: %w", err)
	}
	defer os.Remove(authStage)
	if err := self.unchanged(); err != nil {
		return err
	}
	if err := auth.unchanged(); err != nil {
		return err
	}

	// A hard link retains the original inode, including mode and owner, without
	// copying the running binary. Never touch the real files until both new
	// executables and the rollback link are ready.
	backup, err := os.CreateTemp(filepath.Dir(self.path), ".lazyteams-backup-*")
	if err != nil {
		return err
	}
	backupPath := backup.Name()
	backup.Close()
	if err := os.Remove(backupPath); err != nil {
		return err
	}
	if err := os.Link(self.path, backupPath); err != nil {
		return fmt.Errorf("preparing TUI rollback: %w", err)
	}
	defer os.Remove(backupPath)
	if err := renameInstalledBinary(tuiStage, self.path); err != nil {
		return fmt.Errorf("installing TUI: %w", err)
	}
	if err := renameInstalledBinary(authStage, auth.path); err != nil {
		if rollbackErr := os.Rename(backupPath, self.path); rollbackErr != nil {
			return errors.Join(fmt.Errorf("installing auth helper: %w", err), fmt.Errorf("restoring TUI: %w", rollbackErr))
		}
		return fmt.Errorf("installing auth helper (TUI restored): %w", err)
	}
	return nil
}

// ConsumeUpdateResult is only used by the Windows deferred updater.
func ConsumeUpdateResult(_ []string) (bool, error) {
	return false, nil
}
