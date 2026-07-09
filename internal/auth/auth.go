package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// loadTokensFile loads the tokens file into the environment if it exists.
// Existing environment variables take precedence over the file.
func loadTokensFile() {
	path := filepath.Join(os.Getenv("HOME"), ".config", "teamstui", "tokens.env")
	data, err := os.ReadFile(path)
	if err != nil {
		return // file doesn't exist, continue with env vars
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

func GetTokens() (string, string, string, string, string, string, string, string, error) {
	loadTokensFile()

	graphToken := os.Getenv("MS_GRAPH_TOKEN")
	webToken := os.Getenv("TEAMS_WEB_TOKEN")
	notifToken := os.Getenv("TEAMS_NOTIF_TOKEN")
	eduToken := os.Getenv("EDU_TOKEN")
	cookie := os.Getenv("TEAMS_COOKIE")
	eduCookie := os.Getenv("EDU_COOKIE")
	spacesToken := os.Getenv("TEAMS_SPACES_TOKEN")
	fabricToken := os.Getenv("TEAMS_FABRIC_TOKEN")

	if graphToken == "" || webToken == "" {
		return "", "", "", "", "", "", "", "", errors.New(
			"Tokens not found.\n\n" +
			"Run ./msTTui-auth first\n" +
			"to capture tokens automatically.")
	}
	if cookie == "" {
		return "", "", "", "", "", "", "", "", errors.New("Missing TEAMS_COOKIE in environment.")
	}

	graphToken = strings.TrimSpace(strings.TrimPrefix(graphToken, "Bearer "))
	webToken = strings.TrimSpace(strings.TrimPrefix(webToken, "Bearer "))
	notifToken = strings.TrimSpace(strings.TrimPrefix(notifToken, "Bearer "))
	eduToken = strings.TrimSpace(strings.TrimPrefix(eduToken, "Bearer "))
	eduCookie = strings.TrimSpace(eduCookie)
	spacesToken = strings.TrimSpace(strings.TrimPrefix(spacesToken, "Bearer "))
	fabricToken = strings.TrimSpace(strings.TrimPrefix(fabricToken, "Bearer "))

	return graphToken, webToken, notifToken, eduToken, cookie, eduCookie, spacesToken, fabricToken, nil
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
