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
	graphToken, webToken, notifToken, eduToken, cookie, eduCookie, spacesToken, fabricToken, err := auth.GetTokens()
	if err != nil {
		fmt.Println("Authentication error:\n", err)
		os.Exit(1)
	}

	graphClient := graph.NewClient(graphToken, webToken, notifToken, eduToken, cookie, eduCookie, spacesToken, fabricToken)

	userName := auth.ParseUserNameFromToken(graphToken)

	p := tea.NewProgram(
		ui.New(graphClient, userName),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Fatal TUI error: %v\n", err)
		os.Exit(1)
	}
}
