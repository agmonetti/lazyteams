package helpers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var ErrProfileInUse = errors.New("profile_in_use")

type profileLockMetadata struct {
	PID         int       `json:"pid"`
	OwnershipID string    `json:"ownership_id"`
	AcquiredAt  time.Time `json:"acquired_at"`
}

// ProfileLock is an advisory kernel lock held by an open descriptor. The
// metadata is diagnostic only; lock ownership is never inferred from it.
type ProfileLock struct {
	file *os.File
}

func AcquireProfileLock(configDir string) (*ProfileLock, error) {
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(configDir, "browser-session.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockDescriptor(file); err != nil {
		_ = file.Close()
		if errors.Is(err, ErrProfileInUse) {
			return nil, ErrProfileInUse
		}
		return nil, err
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		_ = unlockDescriptor(file)
		_ = file.Close()
		return nil, err
	}
	metadata, err := json.Marshal(profileLockMetadata{PID: os.Getpid(), OwnershipID: hex.EncodeToString(idBytes), AcquiredAt: time.Now().UTC()})
	if err != nil {
		_ = unlockDescriptor(file)
		_ = file.Close()
		return nil, err
	}
	if err := file.Truncate(0); err != nil {
		_ = unlockDescriptor(file)
		_ = file.Close()
		return nil, err
	}
	if _, err := file.WriteAt(metadata, 0); err != nil {
		_ = unlockDescriptor(file)
		_ = file.Close()
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = unlockDescriptor(file)
		_ = file.Close()
		return nil, err
	}
	return &ProfileLock{file: file}, nil
}

func (l *ProfileLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := unlockDescriptor(l.file)
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return fmt.Errorf("unlock profile: %w", unlockErr)
	}
	return closeErr
}
