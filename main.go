package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"lazyteams/internal/auth"
	"lazyteams/internal/graph"
	"lazyteams/internal/helpers"
	"lazyteams/internal/ui"
	"lazyteams/internal/version"
)

func main() {
	// Kill any stale browser holding our profile from a previous crashed run.
	helpers.KillZombieBrowser()

	// Handle --help and --version flags
	debugMode := false
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-v" {
			fmt.Println(version.Version)
			os.Exit(0)
		}
		if arg == "--help" || arg == "-h" {
			fmt.Print(`lazyteams — Microsoft Teams Terminal UI

USAGE:
  ./lazyteams                         Start the TUI
  ./lazyteams --help                  Show this help
  ./lazyteams --version               Show the build version
  ./lazyteams --debug                 Enable detailed auth-helper logging

AUTH (run ./lazyteams-auth):
  ./lazyteams-auth                    Full token capture (first run)
  ./lazyteams-auth --renew graph      Renew MS Graph token (opens browser)
  ./lazyteams-auth --renew fabric     Renew Fabric token (opens browser)
  ./lazyteams-auth --renew web        Renew Teams Web token (headless)
  ./lazyteams-auth --renew edu        Renew EDU/Assignments token (headless)
  ./lazyteams-auth --renew notif      Renew Notifications token (headless)
  ./lazyteams-auth --show             Force browser visible
  ./lazyteams-auth --headless         Force headless mode
  ./lazyteams-auth --debug            Enable detailed diagnostic output
  ./lazyteams-auth --clear-session    Delete browser session (forces re-login)
  ./lazyteams-auth --clear-tokens     Delete saved tokens

CONFIG FILES:
  Tokens:   ~/.config/lazyteams/tokens.env        (Linux/macOS)
            %APPDATA%\lazyteams\tokens.env         (Windows)
  Prefs:    ~/.config/lazyteams/prefs.json
  Session:  ~/.config/lazyteams/browser-session/

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
