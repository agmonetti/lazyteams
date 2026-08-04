package helpers

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

// ConfigDir returns the msTTui config directory (~/.config/teamstui).
func ConfigDir() string {
	return filepath.Join(HomeDir(), ".config", "teamstui")
}

// KillZombieBrowser terminates any stale Firefox left over from a crashed run
// that is still holding the persistent browser profile. It matches only the
// profile path in the command line, so the user's personal Firefox is never
// touched. Errors are ignored: killing nothing is the normal case.
func KillZombieBrowser() {
	profile := ConfigDir() + string(os.PathSeparator) + "browser-session"
	switch runtime.GOOS {
	case "windows":
		exec.Command("taskkill", "/F", "/FI", "WINDOWTITLE eq "+profile).Run()
	default:
		cmdline := strings.ReplaceAll(profile, "\\", "")
		exec.Command("pkill", "-f", cmdline).Run()
	}
}
