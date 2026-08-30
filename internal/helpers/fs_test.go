package helpers

import (
	"os"
	"path/filepath"
	"testing"
)

// setHome points both home variables os.UserHomeDir resolves on Unix ($HOME)
// and Windows (%USERPROFILE%) at the given values, so HomeDir/ConfigDir behave
// the same on every platform.
func setHome(t *testing.T, home, userprofile string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", userprofile)
}

func TestHomeDir(t *testing.T) {
	dir := t.TempDir()

	t.Run("resolves the home variable", func(t *testing.T) {
		setHome(t, dir, dir)
		if got := HomeDir(); got != dir {
			t.Errorf("HomeDir() = %q, want %q", got, dir)
		}
	})

	t.Run("falls back to USERPROFILE when home is empty", func(t *testing.T) {
		setHome(t, "", dir)
		if got := HomeDir(); got != dir {
			t.Errorf("HomeDir() = %q, want %q", got, dir)
		}
	})

	t.Run("returns empty when nothing is set", func(t *testing.T) {
		// Caveat: on Windows os.UserHomeDir also falls back to
		// %HOMEDRIVE%%HOMEPATH%, which is usually set, so this case is only
		// asserted on Unix (CI runs on ubuntu).
		setHome(t, "", "")
		if got := HomeDir(); got != "" {
			t.Errorf("HomeDir() = %q, want empty", got)
		}
	})
}

func TestConfigDir(t *testing.T) {
	dir := t.TempDir()
	setHome(t, dir, dir)

	want := filepath.Join(dir, ".config", "lazyteams")
	if got := ConfigDir(); got != want {
		t.Errorf("ConfigDir() = %q, want %q", got, want)
	}
}

func TestRemoveFirefoxLocks(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "lock")
	parentLockPath := filepath.Join(dir, ".parentlock")
	parentDotLockPath := filepath.Join(dir, "parent.lock")

	// Create test lock files
	_ = os.WriteFile(parentLockPath, []byte(""), 0600)
	_ = os.WriteFile(parentDotLockPath, []byte(""), 0600)
	_ = os.Symlink("nonexistent-target", lockPath)

	removeFirefoxLocks(dir)

	if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock file still exists after removeFirefoxLocks")
	}
	if _, err := os.Lstat(parentLockPath); !os.IsNotExist(err) {
		t.Errorf(".parentlock file still exists after removeFirefoxLocks")
	}
	if _, err := os.Lstat(parentDotLockPath); !os.IsNotExist(err) {
		t.Errorf("parent.lock file still exists after removeFirefoxLocks")
	}
}
