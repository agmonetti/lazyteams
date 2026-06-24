package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"teamsTUI/internal/auth"
	"teamsTUI/internal/graph"
	"teamsTUI/internal/ui"
)

func main() {
	// 1. Obtener los tokens del entorno
	graphToken, webToken, notifToken, eduToken, cookie, eduCookie, err := auth.GetTokens()
	if err != nil {
		fmt.Println("Error de Autenticación:\n", err)
		os.Exit(1)
	}

	// 2. Inicializar el cliente
	graphClient := graph.NewClient(graphToken, webToken, notifToken, eduToken, cookie, eduCookie)

	// 3. Extraer nombre del usuario del token
	userName := auth.ParseUserNameFromToken(graphToken)

	// 4. Levantar la TUI
	p := tea.NewProgram(ui.New(graphClient, userName), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error fatal en la TUI: %v\n", err)
		os.Exit(1)
	}
}
