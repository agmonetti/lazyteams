<p align="center">
  <img src="assets/banner.svg" alt="ms-teams-TUI" width="900">
</p>

<p align="center">
  A fast, keyboard-driven Microsoft Teams client for the terminal.
</p>

TUI client for Microsoft Teams. Runs entirely in the terminal — no Electron, no browser.

**Clean Architecture + Elm Architecture (Bubble Tea)**. Single static binary.

```
╔══════════════════════╦════════════════════════════════════════════════════════╗
║ Chats                ║            TEAMS-TUI                                   ║
║                      ║                                                        ║
║ ▼ Direct Messages    ║  Microsoft Teams Terminal UI                           ║
║ ▶ Personal notes     ║  v1.0.0-beta                                           ║
║   PRIVATE CHAT 1     ║                                                        ║
║   PRIVATE CHAT 2     ║  [↑/↓] Navigate chats · [Enter]                        ║
║                      ║                                                        ║
║ ▼ Group Chats        ║                                                        ║
║   GROUP CHAT 1       ║                                                        ║
║                      ║                                                        ║
╠══════════════════════╩════════════════════════════════════════════════════════╣
║ [1-4] Workspace  [↑/↓] Navigate  [Enter] Open  [n] New DM  [p] Status  [q] Quit 
╚═══════════════════════════════════════════════════════════════════════════════╝
```

## Features

- **Global UI**: Top status bar with global unread summary, customizable pane layout, a full-screen interactive Cheat Sheet (`?`), and a **Mobile Mode** (responsive single-panel display for narrow terminals — `Ctrl+B` or auto below 120 cols, adapts to window size).
- **4 Workspaces**: Teams and Channels (1), DMs and Group Chats (2), Activity/Notifications (3), Education Assignments (4).
- **Activity filters**: All / Unread / @Mentions / Tags (tagged mentions).
- **Chat & Messaging**: 
  - Send, read, **edit**, and **delete** messages.
  - **Inline image paste**: `Ctrl+P` pastes clipboard images via AMS (Microsoft's Attachment Media Service).
  - **Thread view** with inline replies (`r`), visual tree connectors (`┌─ └→`), and **DM reply with quote** (`Enter` in cursor mode).
  - Native **Markdown rendering** (Glamour) translating Teams HTML (tables, bold, lists).
  - Infinite scroll pagination.
  - **@Mentions** with real-time autocomplete popup.
  - **Reactions** picker (emoji support) with auto-reload, and real-time read receipts (Consumption Horizons).
  - **Attachments**: View/download AMS images and file attachments with `v` in cursor mode (opens locally via OS default app).
  - Visual indicators: `[Attached Image]` / `[Attached File]` for messages with media.
  - Live chat search replacing the input bar (`/`).
- **Files Management**: 
  - Recursive Drive browser with chunked uploads and multi-file downloads.
  - Create folders (`F`), delete files/folders (`Del`), and an interactive local directory picker.
  - Inline TUI preview for text files.
  - Intelligent fallback to **Office Online Viewer** for PDFs and Docs.
- **Team & Channel Management**: 
  - Create and delete Teams (`N`, `D`) and Channels (`C`, `X`).
  - **Member Management**: Add or remove members from Teams and **Private Channels** (bypassing public API limitations using internal Fabric APIs).
  - Hide/unhide specific teams and channels (`H`, `A`) to declutter your workspace.
- **Direct Messages**: Create new 1:1 chats via user search (including **external/federated users** by exact email), auto-discovery of "Personal notes" chat (always prioritized at the top), collapsible sections (DMs vs Groups), and **dynamic sorting** by unread status and last activity.
- **Education Assignments**: View instructions, download professor reference materials, see your submitted work, track statuses, upload files (`u`), submit (`s`), undo submit (`S`), and remove resources (`Del`).
- **Presence**: Read and change your availability status (Available, Busy, DoNotDisturb, etc.).

## Requirements

- Go 1.24+
- Microsoft Teams account (university or enterprise)
- Linux, macOS, or Windows. Playwright requires Firefox for the `auth-helper`.
- **Windows**: Windows Terminal strongly recommended (see [Platform Support](/platform)).

## Quick Start

```bash
# Build the TUI and the Auth Helper
go build -o msTTui .
go build -o msTTui-auth ./cmd/auth-helper/

# Capture tokens (first time, interactive)
./msTTui-auth

# Run
./msTTui

# Show CLI help (usage, auth commands, config paths)
./msTTui --help
```

See [Command-line options](https://ms-teams-tui.agmonetti.workers.dev/#command-line-options) for the full list of `msTTui-auth` flags (`--renew`, `--clear-session`, `--clear-tokens`, `--show`, `--headless`).

## First-time setup

### 1. Grant Microsoft Graph permissions (one-time)

Before running the auth helper for the first time, you need to grant permissions manually via Graph Explorer:

1. Go to [Graph Explorer](https://developer.microsoft.com/en-us/graph/graph-explorer)
2. Click **Sign in** and log in with your Microsoft/organizational account
3. Go to the **Modify permissions** tab
4. Search and enable each of the following permissions:

| Permission | Purpose |
|------------|---------|
| `User.Read` | Read your profile |
| `User.Read.All` | Search users for DMs, @mentions, member management |
| `Team.ReadBasic.All` | List your teams |
| `Channel.ReadBasic.All` | List channels |
| `Chat.ReadBasic` | Read chat names and members |
| `Presence.Read.All` | Read presence status |
| `Presence.ReadWrite` | Set your presence |
| `Files.ReadWrite.All` | Access and upload files |
| `Sites.Read.All` | Access SharePoint sites |
| `GroupMember.Read.All` | *(Optional)* Read team members (May require Admin Consent) |
| `Team.Create` | *(Optional)* Create teams (`N`) |

### 2. Run the auth helper

```bash
./msTTui-auth
```

When prompted, a browser will open Graph Explorer. Click **Sign in**, select your account, and wait for the tokens to be captured automatically.

### 3. Run msTTui

```bash
./msTTui
```

## Tokens

The client requires 8 distinct tokens/cookies to talk to the many internal APIs that power Microsoft Teams. The `msTTui-auth` helper captures them automatically via a headless (and partially visible) Playwright Firefox session:

| Token | Expiration | Usage |
|-------|-----------|-----|
| `MS_GRAPH_TOKEN` | ~1h | Chats, teams, channels, presence, files, user search |
| `TEAMS_WEB_TOKEN` | ~24h | Read/write messages, reactions, read receipts (ChatSvc) |
| `TEAMS_NOTIF_TOKEN` | ~24h | Push notifications activity |
| `EDU_TOKEN` | ~1h | Education Assignments API |
| `EDU_COOKIE` | ~24h | Education Assignments WAF bypass |
| `TEAMS_SPACES_TOKEN` | ~24h | Team and standard channel creation/deletion |
| `TEAMS_FABRIC_TOKEN` | ~24h | Private channel member management (JWE write-scope). Captured by changing your own role (Owner→Member) in any channel — fails if you are the only owner, but token is captured before the error. |
| `TEAMS_COOKIE` | ~24h | General session authentication |

Tokens are saved to `~/.config/teamstui/tokens.env`. The TUI automatically renews most expired tokens in the background via the auth helper.

## Architecture

```
lazyteams/
├── main.go                          # Entry point
├── cmd/
│   ├── auth-helper/                 # Playwright auth helper
│   └── debug-*/                     # Assorted debug tools
└── internal/
    ├── auth/                        # Token loader + JWT parsing
    ├── graph/                       # Graph, ChatSvc, Spaces, Fabric APIs
    ├── teams/                       # Business logic (file icons, attachment logic)
    └── ui/                          # Bubble Tea TUI
        ├── model.go                 # State definition
        ├── types.go                 # Shared type definitions
        ├── view.go                  # Rendering logic (Layouts, Panels)
        ├── update.go                # Main state machine
        ├── update_keys.go           # Keybinding routing
        ├── update_popups.go         # Floating windows logic
        ├── update_data.go           # Network/API response handling
        ├── update_insert.go         # Textarea input logic
        ├── helpers.go               # Pure utility functions
        ├── commands.go              # tea.Cmd constructors (API calls)
        ├── format.go                # Message formatting
        ├── help.go                  # Cheat sheet renderer
        ├── styles.go                # Lipgloss color/style definitions
        ├── prefs.go                 # Persistent preferences
        └── components/
            └── directorypicker/     # Reusable filesystem browser component
```

## Disclaimer

Educational tool. Operates on already-authenticated Microsoft Teams sessions. Does not distribute proprietary Microsoft binaries. *Not affiliated with Microsoft Corporation.*

## License

This project is licensed under the **GNU General Public License v3.0 (GPLv3)**. See the [LICENSE](LICENSE) file for more details.
