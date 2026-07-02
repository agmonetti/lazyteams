# ms-teams-TUI

<p align="center">
  <img src="assets/banner.svg" alt="ms-teams-TUI" width="900">
</p>

<p align="center">
  A fast, keyboard-driven Microsoft Teams client for the terminal.
</p>

TUI client for Microsoft Teams. Runs in the terminal — no Electron, no browser.

**~3,500 lines of Go.** Clean Architecture + Elm Architecture (Bubble Tea). Single static binary.

```
╔══════════════════════╦════════════════════════════════════════════════════════╗
║ Chats                ║            TEAMS-TUI                                   ║
║                      ║                                                        ║
║ ► Personal notes     ║  Microsoft Teams Terminal UI                           ║
║   PRIVATE CHAT 1     ║  v1.0.0-beta                                           ║
║   PRIVATE CHAT 2     ║                                                        ║
║   PRIVATE CHAT 3     ║  [↑/↓] Navigate teams · [Enter]                       ║
║   ...                ║                                                        ║
║                      ║                                                        ║
╠══════════════════════╩════════════════════════════════════════════════════════╣
║ [1] Teams  [2] DMs  [3] Activity  [4] Assignments  [p] Status  [q] Quit     ║
╚═══════════════════════════════════════════════════════════════════════════════╝
```

## Features

- **4 Workspaces**: Teams & channels, DMs, Activity/Notifications, Education Assignments
- **Messages**: Read and send messages (RichText/Html), auto-refresh every 15s, clickable links (OSC 8)
- **Files**: Recursive Drive browser, multi-selection, download with customizable directory
- **Presence**: Read and change your status (Available, Busy, DoNotDisturb, etc.)
- **Notifications**: Filter by type, navigate to source channel
- **Auto-discovery**: Personal chat ("Personal notes"), smart group names

## Requirements

- Go 1.24+
- Microsoft Teams account (university or enterprise)
- Linux (tested on Arch). Playwright needs Firefox (for the auth helper).

## Quick Start

```bash
# Build
go build -o msTTui .
go build -o msTTui-auth ./cmd/auth-helper/

# Capture tokens (first time, ~90s)
./msTTui-auth

# Run
./msTTui
```

## Tokens

msTTui needs 5 tokens to connect to the Teams APIs. The `msTTui-auth` helper captures them automatically via Playwright (Firefox) in ~90 seconds:

| Token | Expiration | Usage |
|-------|-----------|-----|
| `MS_GRAPH_TOKEN` | ~1h | Chats, teams, channels, presence, files |
| `TEAMS_WEB_TOKEN` | ~24h | Read/write messages (ChatSvc) |
| `TEAMS_NOTIF_TOKEN` | ~24h | Push notifications |
| `EDU_TOKEN` | ~1h | Education Assignments |
| `TEAMS_COOKIE` | ~24h | Session authentication |

Tokens are saved to `~/.config/teamstui/tokens.env`. There is no automatic renewal — run `./msTTui-auth` again when they expire.

## Architecture

```
msTTui/
├── main.go                          # Entry point
├── cmd/
│   ├── auth-helper/                 # Playwright auth helper (captures 5 tokens)
│   ├── debug-assignments/           # Debug tool for Education API
│   └── debug-endpoints/             # Debug tool for endpoints
└── internal/
    ├── auth/                        # Tokens (reading + JWT parsing)
    ├── graph/                       # HTTP client for Microsoft Graph API
    │   ├── client.go                # Client, doReq(), cleanHTML()
    │   ├── teams_api.go             # Teams and channels
    │   ├── chats_api.go             # Chats, self-chat auto-discovery
    │   ├── messages_api.go          # Read/write messages (ChatSvc)
    │   ├── files_api.go             # Drive browser, remote items
    │   ├── presence_api.go          # Get/Set/Clear presence
    │   ├── assignments_api.go       # Education Assignments (blocked by WAF)
    │   ├── activity_api.go          # Notifications via ChatSvc
    │   └── download_api.go          # File downloads
    ├── teams/teams.go               # Attachment aggregation, file icons
    └── ui/                          # TUI (Elm Architecture with Bubble Tea)
```

**Clean Architecture**: `ui` never makes HTTP calls. All networking goes through `graph` and `teams` via `tea.Cmd` (async).

## Documentation

**[Full documentation](https://ms-teams-tui.agmonetti.workers.dev/)** — Keyboard shortcuts, configuration, limitations, development guide, and more.

## Security

- Tokens stored in plaintext (`~/.config/teamstui/tokens.env`)
- ChatSvc API is undocumented/private — Microsoft could break it at any time
- Playwright browser session stores cookies on disk

## Disclaimer

Educational tool. Operates on already-authenticated Microsoft Teams sessions. Does not distribute proprietary Microsoft binaries. *Not affiliated with Microsoft Corporation.*
