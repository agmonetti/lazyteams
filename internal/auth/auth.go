package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lazyteams/internal/helpers"
)

// loadTokensFile reads the tokens file into a map.
// Environment variables still take precedence when set explicitly.
func loadTokensFile() map[string]string {
	result := make(map[string]string)
	path := filepath.Join(helpers.ConfigDir(), "tokens.env")
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}

	// Warn when the token file is readable by group/others (e.g. restored from
	// a backup with default permissions). Do not block: the file still works,
	// and Windows has no POSIX mode bits to enforce anyway.
	if info, statErr := os.Stat(path); statErr == nil && info.Mode().Perm()&0o077 != 0 {
		fmt.Fprintf(os.Stderr, "warning: %s is readable by other users (mode %#o); run chmod 600 on it\n", path, info.Mode().Perm())
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
		result[key] = val
	}
	return result
}

func GetTokens() (string, string, string, string, string, string, string, string, error) {
	env := loadTokensFile()

	get := func(key string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return env[key]
	}

	graphToken := get("MS_GRAPH_TOKEN")
	webToken := get("TEAMS_WEB_TOKEN")
	notifToken := get("TEAMS_NOTIF_TOKEN")
	eduToken := get("EDU_TOKEN")
	cookie := get("TEAMS_COOKIE")
	eduCookie := get("EDU_COOKIE")
	spacesToken := get("TEAMS_SPACES_TOKEN")
	fabricToken := get("TEAMS_FABRIC_TOKEN")

	if graphToken == "" || webToken == "" {
		return "", "", "", "", "", "", "", "", errors.New(
			"Tokens not found.\n\n" +
				"Run ./lazyteams-auth first\n" +
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
