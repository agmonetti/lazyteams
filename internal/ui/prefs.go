package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Preferences struct {
	// Mapea UserID -> ChatID para soportar múltiples cuentas sin que el caché colisione
	SelfChatIDs map[string]string `json:"self_chat_ids"`
	DownloadDir string            `json:"download_dir,omitempty"`
}

func prefsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "teamstui", "prefs.json")
}

func loadPrefs() Preferences {
	var p Preferences
	p.SelfChatIDs = make(map[string]string)

	data, err := os.ReadFile(prefsPath())
	if err == nil {
		json.Unmarshal(data, &p)
	}

	// Asegurar inicialización por si el JSON estaba vacío o malformado
	if p.SelfChatIDs == nil {
		p.SelfChatIDs = make(map[string]string)
	}
	if p.DownloadDir == "" {
		home, _ := os.UserHomeDir()
		p.DownloadDir = filepath.Join(home, "Downloads")
	}
	return p
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
	// Permisos 0600: solo el dueño puede leer/escribir este archivo
	return os.WriteFile(path, data, 0600)
}