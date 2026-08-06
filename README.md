<p align="center">
  <img src="assets/banner.svg" alt="ms-teams-TUI" width="900">
</p>

<p align="center">
  A fast, keyboard-driven Microsoft Teams client for the terminal.
</p>

TUI client for Microsoft Teams. Runs entirely in the terminal — no Electron, no browser. Built with **Clean Architecture + Elm Architecture (Bubble Tea)**. Single static binary.

```
╔════════════════════════════╦════════════════════════════════════════════════════╗
║ Chats                      ║                    LAZYTEAMS                       ║
║                            ║                                                    ║
║ ▼ Direct Messages          ║            Microsoft Teams Terminal UI             ║
║   Personal notes           ║                   v1.0.0-beta                      ║
║   PRIVATE CHAT 1           ║                                                    ║
║   PRIVATE CHAT 2           ║          [↑/↓] Navigate chats · [Enter]            ║
║                            ║                                                    ║
║ ▼ Group Chats              ║                                                    ║
║   GROUP CHAT 1             ║                                                    ║
║                            ║                                                    ║
╠════════════════════════════╩════════════════════════════════════════════════════╣
║ [1-4] Workspace  [↑/↓] Navigate  [Enter] Open  [n] New DM  [p] Status  [q] Quit ║
╚═════════════════════════════════════════════════════════════════════════════════╝
```

## Highlights

- **4 workspaces**: Teams & Channels, DMs & Group Chats, Activity, Education Assignments.
- **Chat**: send/edit/delete messages, threads with inline replies, @mentions autocomplete, reactions, read receipts, clipboard image paste (`Ctrl+P`), native Markdown rendering, live search (`/`), infinite scroll.
- **Files**: recursive Drive browser, chunked uploads, multi-file downloads, inline text preview, Office Online fallback, create/delete folders.
- **Team management**: create/delete teams and channels, member management (including private channels via internal APIs), hide/unhide items.
- **Direct messages**: user search (external users by exact email), personal notes auto-discovery, dynamic sorting by unread + activity, presence indicators.
- **Assignments**: view instructions, download reference materials, upload/submit/undo-submit your work.
- **Presence**: read contacts and set your own status.
- **Mobile Mode**: single-panel responsive layout for narrow terminals (`Ctrl+B`, auto below 120 cols).

## Requirements

- Go 1.24+
- Microsoft Teams account (university or enterprise)
- Linux, macOS, or Windows. Playwright requires Firefox for the `auth-helper`.

## Quick Start

```bash
# Build the TUI and the Auth Helper
go build -o lazyteams .
go build -o lazyteams-auth ./cmd/auth-helper/

# Capture tokens (first time, interactive)
./lazyteams-auth

# Run
./lazyteams

# Show CLI help (usage, auth commands, config paths)
./lazyteams --help
```

First-time setup requires granting a set of Microsoft Graph permissions once (see [the docs](https://ms-teams-tui.agmonetti.workers.dev/#first-time-setup)). The TUI renews expired tokens automatically in the background.

## Documentation

Full documentation — first-time setup, token system, configuration files, keybindings, platform support, and architecture — lives on the docs site:

[Documentation](https://ms-teams-tui.agmonetti.workers.dev/)

Security-sensitive data handling is described in [`SECURITY.md`](SECURITY.md).

## Disclaimer

Educational tool. Operates on already-authenticated Microsoft Teams sessions. Does not distribute proprietary Microsoft binaries. *Not affiliated with Microsoft Corporation.*

## License

This project is licensed under the **GNU General Public License v3.0 (GPLv3)**. See the [LICENSE](LICENSE) file for more details.
