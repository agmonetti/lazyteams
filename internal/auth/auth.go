package auth

import (
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
