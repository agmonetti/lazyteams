package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
)

// GetTokens lee ambos tokens JWT desde el entorno.
func GetTokens() (string, string, error) {
	graphToken := os.Getenv("MS_GRAPH_TOKEN")
	webToken := os.Getenv("TEAMS_WEB_TOKEN")
	cookie := os.Getenv("TEAMS_COOKIE")

	if graphToken == "" || webToken == "" {
		return "", "", errors.New("Faltan tokens de entorno.\nAsegurate de exportar MS_GRAPH_TOKEN y TEAMS_WEB_TOKEN.")
	}
	if cookie == "" {
		return "", "", errors.New("Falta TEAMS_COOKIE en el entorno.\nAsegurate de exportar la cookie capturada desde el navegador.")
	}

	graphToken = strings.TrimPrefix(graphToken, "Bearer ")
	graphToken = strings.TrimSpace(graphToken)

	webToken = strings.TrimPrefix(webToken, "Bearer ")
	webToken = strings.TrimSpace(webToken)

	return graphToken, webToken, nil
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
	if name, ok := claims["name"].(string); ok {
		return name
	}
	if name, ok := claims["unique_name"].(string); ok {
		return name
	}
	return ""
}
