package helpers

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
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

func TestProfileLockExcludesConcurrentHelper(t *testing.T) {
	dir := t.TempDir()
	readyPath := filepath.Join(dir, "ready")
	cmd := exec.Command(os.Args[0], "-test.run=TestProfileLockChildProcess")
	cmd.Env = append(os.Environ(),
		"LAZYTEAMS_PROFILE_LOCK_CHILD=1",
		"LAZYTEAMS_PROFILE_LOCK_DIR="+dir,
		"LAZYTEAMS_PROFILE_LOCK_READY="+readyPath,
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(readyPath); err != nil {
		t.Fatalf("child did not acquire profile lock: %v", err)
	}

	second, err := AcquireProfileLock(dir)
	if !errors.Is(err, ErrProfileInUse) {
		if err == nil {
			_ = second.Release()
		}
		t.Fatalf("concurrent AcquireProfileLock = %v, want ErrProfileInUse", err)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	third, err := AcquireProfileLock(dir)
	if err != nil {
		t.Fatalf("AcquireProfileLock after child termination: %v", err)
	}
	if err := third.Release(); err != nil {
		t.Fatalf("third Release: %v", err)
	}
}

func TestProfileLockChildProcess(t *testing.T) {
	if os.Getenv("LAZYTEAMS_PROFILE_LOCK_CHILD") != "1" {
		return
	}
	lock, err := AcquireProfileLock(os.Getenv("LAZYTEAMS_PROFILE_LOCK_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if err := os.WriteFile(os.Getenv("LAZYTEAMS_PROFILE_LOCK_READY"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
}
