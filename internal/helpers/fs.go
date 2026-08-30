package helpers

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// HomeDir returns the current user's home directory, falling back to
// legacy HOME/USERPROFILE environment variables if needed.
func HomeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	if home := os.Getenv("USERPROFILE"); home != "" {
		return home
	}
	return ""
}

// ConfigDir returns the lazyteams config directory (~/.config/lazyteams).
func ConfigDir() string {
	return filepath.Join(HomeDir(), ".config", "lazyteams")
}

func removeFirefoxLocks(dir string) {
	if dir == "" {
		return
	}
	os.Remove(filepath.Join(dir, "parent.lock"))
	os.Remove(filepath.Join(dir, ".parentlock"))
	os.Remove(filepath.Join(dir, "lock"))
}

func cleanPlaywrightFirefoxLocks() {
	dirs := []string{
		filepath.Join(HomeDir(), ".cache", "ms-playwright"),
		filepath.Join(HomeDir(), "AppData", "Local", "ms-playwright"),
	}
	for _, base := range dirs {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), "firefox") {
				removeFirefoxLocks(filepath.Join(base, entry.Name()))
				removeFirefoxLocks(filepath.Join(base, entry.Name(), "firefox"))
			}
		}
	}
}

// KillZombieBrowser terminates any stale Firefox left over from a crashed run
// that is still holding the persistent browser profile. It matches only the
// profile path in the command line, so the user's personal Firefox is never
// touched. Errors are ignored: killing nothing is the normal case.
//
// Accepted risk: the match pattern is derived from $HOME, so a local attacker
// who can control the HOME environment variable could steer `pkill -9 -f`
// toward arbitrary processes whose command line contains that path. This
// requires local code execution already, and the profile path is hard to
// collide with, so the risk is accepted for the crash-recovery benefit.
func KillZombieBrowser() {
	profile := ConfigDir() + string(os.PathSeparator) + "browser-session"
	switch runtime.GOOS {
	case "windows":
		// /T kills the whole process tree spawned by Firefox.
		exec.Command("taskkill", "/F", "/T", "/FI", "WINDOWTITLE eq "+profile).Run()
	default:
		// SIGKILL (-9) so a hung Firefox can't keep the profile locked. Sleep
		// only when something was actually killed, letting the OS release the
		// profile before the next launch.
		cmdline := strings.ReplaceAll(profile, "\\", "")
		if exec.Command("pkill", "-9", "-f", cmdline).Run() == nil {
			time.Sleep(500 * time.Millisecond)
		}
	}
	// Firefox leaves lock files behind; remove them so a fresh launch can
	// never fail with "Firefox is already running, but is not responding" or
	// Playwright host validation errors with broken lock symlinks.
	removeFirefoxLocks(profile)
	cleanPlaywrightFirefoxLocks()
}

// SignalAuthProcess asks a background lazyteams-auth helper to stop gracefully so
// it can close its Playwright browser before exiting. Windows has no signal
// model, so it falls back to a hard kill there.
func SignalAuthProcess(proc *os.Process) {
	if proc == nil {
		return
	}
	if runtime.GOOS == "windows" {
		proc.Kill()
		return
	}
	proc.Signal(syscall.SIGTERM)
}
