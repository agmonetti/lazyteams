package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// loadTokensFile carga el archivo de tokens en el entorno si existe.
// Las variables de entorno existentes tienen prioridad sobre el archivo.
func loadTokensFile() {
	path := filepath.Join(os.Getenv("HOME"), ".config", "teamstui", "tokens.env")
	data, err := os.ReadFile(path)
	if err != nil {
		return // archivo no existe, continuar con env vars
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), `"`)
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

func GetTokens() (string, string, string, string, string, string, error) {
	loadTokensFile()

	graphToken := os.Getenv("MS_GRAPH_TOKEN")
	webToken := os.Getenv("TEAMS_WEB_TOKEN")
	notifToken := os.Getenv("TEAMS_NOTIF_TOKEN")
	eduToken := os.Getenv("EDU_TOKEN")
	cookie := os.Getenv("TEAMS_COOKIE")
	eduCookie := os.Getenv("EDU_COOKIE")

	if graphToken == "" || webToken == "" {
		return "", "", "", "", "", "", errors.New(
			"Tokens no encontrados.\n\n" +
			"Corré primero: ./msTTui-auth\n" +
			"para capturar los tokens automáticamente.")
	}
	if cookie == "" {
		return "", "", "", "", "", "", errors.New("Falta TEAMS_COOKIE en el entorno.")
	}

	graphToken = strings.TrimSpace(strings.TrimPrefix(graphToken, "Bearer "))
	webToken = strings.TrimSpace(strings.TrimPrefix(webToken, "Bearer "))
	notifToken = strings.TrimSpace(strings.TrimPrefix(notifToken, "Bearer "))
	eduToken = strings.TrimSpace(strings.TrimPrefix(eduToken, "Bearer "))
	eduCookie = strings.TrimSpace(eduCookie)

	return graphToken, webToken, notifToken, eduToken, cookie, eduCookie, nil
}

func ParseUserNameFromToken(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload := strings.NewReplacer("-", "+", "_", "/").Replace(parts[1])
	if l := len(payload) % 4; l > 0 {
		payload += strings.Repeat("=", 4-l)
	}
	b, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(b, &claims); err != nil {
		return ""
	}
	if name, ok := claims["name"].(string); ok && name != "" {
		return name
	}
	if name, ok := claims["unique_name"].(string); ok && name != "" {
		return name
	}
	if name, ok := claims["preferred_username"].(string); ok && name != "" {
		return name
	}
	if name, ok := claims["upn"].(string); ok && name != "" {
		return name
	}
	if name, ok := claims["given_name"].(string); ok && name != "" {
		return name
	}
	return ""
}
