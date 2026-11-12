//go:build !windows

package helpers

import (
	"errors"
	"os"
	"syscall"
)

func lockDescriptor(file *os.File) error {
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return ErrProfileInUse
		}
		return err
	}
	return nil
}

func unlockDescriptor(file *os.File) error { return syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }
