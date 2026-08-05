package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"teamsTUI/internal/auth"
	"teamsTUI/internal/graph"
	"teamsTUI/internal/helpers"
	"teamsTUI/internal/ui"
)

func main() {
	// Kill any stale browser holding our profile from a previous crashed run.
	helpers.KillZombieBrowser()

	// Handle --help flag
	debugMode := false
	for _, arg := range os.Args[1:] {
		if arg == "--help" || arg == "-h" {
			fmt.Print(`msTTui — Microsoft Teams Terminal UI

USAGE:
  ./msTTui                         Start the TUI
  ./msTTui --help                  Show this help
  ./msTTui --debug                 Enable detailed auth-helper logging

AUTH (run ./msTTui-auth):
  ./msTTui-auth                    Full token capture (first run)
  ./msTTui-auth --renew graph      Renew MS Graph token (opens browser)
  ./msTTui-auth --renew fabric     Renew Fabric token (opens browser)
  ./msTTui-auth --renew web        Renew Teams Web token (headless)
  ./msTTui-auth --renew edu        Renew EDU/Assignments token (headless)
  ./msTTui-auth --renew notif      Renew Notifications token (headless)
  ./msTTui-auth --show             Force browser visible
  ./msTTui-auth --headless         Force headless mode
  ./msTTui-auth --debug            Enable detailed diagnostic output
  ./msTTui-auth --clear-session    Delete browser session (forces re-login)
  ./msTTui-auth --clear-tokens     Delete saved tokens

CONFIG FILES:
  Tokens:   ~/.config/teamstui/tokens.env        (Linux/macOS)
            %APPDATA%\teamstui\tokens.env         (Windows)
  Prefs:    ~/.config/teamstui/prefs.json
  Session:  ~/.config/teamstui/browser-session/

NOTES:
  - On first run, a browser window opens for Microsoft login.
  - Tokens expire periodically and are renewed automatically.
  - TEAMS_FABRIC_TOKEN requires manual capture via a Private Channel.
  - On Windows, use Windows Terminal for full emoji and Unicode support.
  - New accounts may need to consent to Chat.ReadBasic in Graph Explorer.
    See: https://developer.microsoft.com/en-us/graph/graph-explorer

`)
			os.Exit(0)
		}
		if arg == "--debug" {
			debugMode = true
		}
	}

	graphToken, webToken, notifToken, eduToken, cookie, eduCookie, spacesToken, fabricToken, err := auth.GetTokens()
	if err != nil {
		fmt.Println("Authentication error:\n", err)
		os.Exit(1)
	}

	graphClient := graph.NewClient(graphToken, webToken, notifToken, eduToken, cookie, eduCookie, spacesToken, fabricToken)

	userName := auth.ParseUserNameFromToken(graphToken)

	p := tea.NewProgram(
		ui.New(graphClient, userName, debugMode),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Fatal TUI error: %v\n", err)
		os.Exit(1)
	}
}
