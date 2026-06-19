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
	// 1. Obtener los tokens del entorno (BYOT Dual)
	graphToken, webToken, err := auth.GetTokens()
	if err != nil {
		fmt.Println("Error de Autenticación:\n", err)
		os.Exit(1)
	}

	// 2. Inicializar el cliente
	graphClient := graph.NewClient(graphToken, webToken)

	// 3. (Sprint 2) Levantar la TUI y dejar que ella cargue los equipos de fondo
	p := tea.NewProgram(ui.New(graphClient), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error fatal en la TUI: %v\n", err)
		os.Exit(1)
	}
}
