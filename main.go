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
	// 1. Get tokens from environment
	graphToken, webToken, notifToken, eduToken, cookie, eduCookie, spacesToken, err := auth.GetTokens()
	if err != nil {
		fmt.Println("Authentication error:\n", err)
		os.Exit(1)
	}

	// 2. Initialize the client
	graphClient := graph.NewClient(graphToken, webToken, notifToken, eduToken, cookie, eduCookie, spacesToken)

	// 3. Extract username from token
	userName := auth.ParseUserNameFromToken(graphToken)

	// 4. Start the TUI
	p := tea.NewProgram(ui.New(graphClient, userName), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Fatal TUI error: %v\n", err)
		os.Exit(1)
	}
}
