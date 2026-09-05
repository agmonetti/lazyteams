package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/agmonetti/lazyteams/internal/auth"
	"github.com/agmonetti/lazyteams/internal/graph"
	"github.com/agmonetti/lazyteams/internal/helpers"
	"github.com/agmonetti/lazyteams/internal/ui"
	"github.com/agmonetti/lazyteams/internal/version"
)

func main() {
	// Kill any stale browser holding our profile from a previous crashed run.
	helpers.KillZombieBrowser()

	// Handle --help, --version, --update and --debug flags
	debugMode := false
	doUpdate := false
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-v" {
			fmt.Println(version.Version)
			os.Exit(0)
		}
		if arg == "--update" {
			doUpdate = true
		}
		if arg == "--help" || arg == "-h" {
			fmt.Print(`lazyteams — Microsoft Teams Terminal UI

USAGE:
  ./lazyteams                         Start the TUI
  ./lazyteams --help                  Show this help
  ./lazyteams --version               Show the build version
  ./lazyteams --debug                 Enable detailed auth-helper logging
  ./lazyteams --update                Check for and install the latest release

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

UPDATE:
  ./lazyteams --update              Download the latest release (TUI and
                                    auth-helper) from GitHub, verify its
                                    SHA-256 checksums and replace the running
                                    binaries. It will ask for confirmation
                                    before replacing anything.

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

	if doUpdate {
		os.Exit(runUpdate())
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

// runUpdate performs the self-update flow and returns a process exit code.
func runUpdate() int {
	info, err := version.LatestReleaseInfo(version.GithubReleasesURL)
	if err != nil {
		fmt.Printf("Update check failed: %v\n", err)
		return 1
	}
	if version.Compare(version.Version, info.TagName) >= 0 {
		fmt.Printf("Already up to date (v%s).\n", strings.TrimPrefix(version.Version, "v"))
		return 0
	}

	fmt.Printf("A new release (%s) is available; you are running %s.\n", info.TagName, version.Version)

	selfExe, err := version.ExecutablePath()
	if err != nil {
		fmt.Printf("Cannot determine current binary path: %v\n", err)
		return 1
	}
	authExe := version.AuthHelperSiblingPath(selfExe)

	confirm := func(prompt string) bool {
		fmt.Printf("%s [y/N] ", prompt)
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))
		return line == "y" || line == "yes"
	}
	progress := func(msg string) {
		fmt.Print(msg)
	}

	updated, err := info.UpdateCmd(version.Version, selfExe, authExe, confirm, progress)
	if err != nil {
		fmt.Printf("Update failed: %v\n", err)
		return 1
	}
	if !updated {
		return 0
	}

	// Restart with the new binary, preserving original arguments minus any
	// --update/--help flags and this process's argv[0].
	args := make([]string, 0, len(os.Args))
	args = append(args, selfExe)
	for _, a := range os.Args[1:] {
		if a == "--update" {
			continue
		}
		args = append(args, a)
	}
	if err := version.Restart(selfExe, args); err != nil {
		fmt.Printf("Updated but could not restart: %v\n", err)
		return 1
	}
	return 0
}
