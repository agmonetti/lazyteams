package helpers

import (
	"os"
	"path/filepath"
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
