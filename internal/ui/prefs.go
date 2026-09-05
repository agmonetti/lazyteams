package ui

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/agmonetti/lazyteams/internal/helpers"
)

type Preferences struct {
	// Maps UserID -> ChatID to support multiple accounts without cache collisions
	SelfChatIDs           map[string]string   `json:"self_chat_ids"`
	DownloadDir           string              `json:"download_dir,omitempty"`
	HiddenTeams           []string            `json:"hidden_teams,omitempty"`
	HiddenChannels        map[string][]string `json:"hidden_channels"` // teamID → channelID
	DMSectionCollapsed    bool                `json:"dmSectionCollapsed"`
	GroupSectionCollapsed bool                `json:"groupSectionCollapsed"`
	ShowFileInfo          bool                `json:"showFileInfo"`
}

func prefsPath() string {
	return filepath.Join(helpers.ConfigDir(), "prefs.json")
}

func loadPrefs() Preferences {
	var p Preferences
	p.SelfChatIDs = make(map[string]string)

	data, err := os.ReadFile(prefsPath())
	if err == nil {
		json.Unmarshal(data, &p)
	}

	// Ensure initialization in case the JSON was empty or malformed
	if p.SelfChatIDs == nil {
		p.SelfChatIDs = make(map[string]string)
	}
	if p.HiddenChannels == nil {
		p.HiddenChannels = make(map[string][]string)
	}
	if p.DownloadDir == "" {
		home, _ := os.UserHomeDir()
		p.DownloadDir = filepath.Join(home, "Downloads")
	}
	return p
}

func loadPrefsIntoModel(m *Model, prefs Preferences) {
	m.dmSectionCollapsed = prefs.DMSectionCollapsed
	m.groupSectionCollapsed = prefs.GroupSectionCollapsed
	m.showFileInfo = prefs.ShowFileInfo
}

func savePrefs(p Preferences) error {
	path := prefsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	// Permissions 0600: only the owner can read/write this file
	return os.WriteFile(path, data, 0600)
}
