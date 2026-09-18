package helpers

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
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
