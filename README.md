<p align="center">
  <img src="assets/banner.svg" alt="lazyteams" width="900">
</p>

<p align="center">
  <strong>A keyboard-driven Microsoft Teams client for the terminal.</strong><br>
  <em>Built for Universities and Enterprises. Zero Electron, real SSO/MFA support without IT Admin approval, and full Education Assignments integration.</em>
</p>

<p align="center">
  <a href="https://lazyteams.agmonetti.workers.dev">Explore</a> ✦
  <a href="#quick-start">Quick Start</a> ✦
  <a href="https://lazyteams.agmonetti.workers.dev/#first-time-setup">Getting Started</a> ✦
  <a href="https://lazyteams.agmonetti.workers.dev">Documentation</a> ✦
  <a href="CONTRIBUTING.md">Contributing</a> ✦
  <a href="SECURITY.md">Security</a> ✦
  <a href="LICENSE">License</a>
</p>

---

TUI client for Microsoft Teams running 100% in the terminal — **no Electron, no web browser overhead (~30MB RAM vs 1.5GB)**. Built with **Clean Architecture + Elm Architecture (Bubble Tea)**.

<p align="center">
  <img src="assets/gif.gif" alt="lazyteams demo" width="900">
</p>

## Why lazyteams?

Most command-line Teams projects rely on simple public OAuth device flows that **fail immediately in corporate and university tenants** with `AADSTS65002` because non-admin users cannot register third-party Azure AD apps.

`lazyteams` was designed from the ground up to solve this: it uses a hybrid architecture that logs in via your institution's genuine web session once, bypassing administrative blocks and granting access to internal endpoints like **Teams Assignments** and **ChatSvc**.

| Feature | Official Teams (Electron) | Other Terminal TUIs | **lazyteams** |
| :--- | :---: | :---: | :---: |
| **Memory Footprint** | ~1.5 GB | ~25 MB | **~30 MB** |
| **University / Enterprise SSO** | Yes | Fails (`AADSTS65002`) | **100% Compatible (SAML / Duo / MFA)** |
| **Requires Azure Admin Approval** | No (Pre-approved) | Yes (Blocks non-admins) | **No IT Admin approval needed** |
| **Teams Assignments (Homework)** | Yes | Not supported | **Full View / Upload / Submit / Undo** |
| **Paste Images from Clipboard** | Yes | Limited or Broken | **Native (`Ctrl+P` via AMS upload)** |
| **Thread Replies & Reactions** | Yes | Partial | **Full thread trees & emoji counts** |
| **Zero Webview in TUI Runtime** | Electron bloat | Native TUI | **Pure terminal (Bubble Tea + Lipgloss)** |

---

## Highlights

- **Education & Assignments (Exclusive):** The only TUI with first-class support for Microsoft Teams Assignments. View deadlines, upcoming/overdue filters, download reference materials, upload deliverables, and submit or undo submission directly from the terminal.
- **Full Chat & Threads:** Send, edit, and delete messages. Full thread replies, @mentions autocomplete, live search (`/`), emoji reactions, and instant image pasting from clipboard (`Ctrl+P`).
- **Cloud Files & SharePoint:** Deep recursive Drive browser for channels and classes, chunked large file uploads, multi-file downloads, inline text preview, and folder creation/deletion.
- **Enterprise & Channel Management:** Create and delete teams/channels, manage members and roles (including private channels via internal APIs), and toggle hidden items.
- **Direct Messages & User Search:** Search external users by exact email, auto-discover personal notes, dynamic sorting by unread status and activity, and real-time presence indicators (`Available`, `Busy`, `DND`, `Away`).
- **Responsive & Mobile Mode:** Adaptive layout for narrow terminals, tmux panes, or mobile SSH clients (`Ctrl+B`, automatically adapts below 120 columns).

---

## The Enterprise & University SSO Solution (lazyteams-auth)

Why are there two binaries (`lazyteams` and `lazyteams-auth`)?

1. **The Problem:** In corporate and educational tenants (universities, hospitals, enterprises), IT administrators restrict third-party OAuth app registrations. Standard CLI clients fail with errors like `AADSTS65002` or require unattainable Global Admin consent.
2. **The Solution:** `lazyteams-auth` launches an authentic, isolated browser session once. You sign in through your organization's official SSO portal (supporting 2FA, Duo, Authenticator, or hardware keys).
3. **Headless TUI:** Once the session tokens are safely saved to `~/.config/lazyteams/tokens.env` (with restricted permissions `0600`), the main TUI (`lazyteams`) runs completely standalone in your terminal. Tokens are automatically refreshed in the background without interrupting your workflow.

> [!NOTE]
> Token renewals for Teams Web, Notifications, and EDU run quietly in headless mode. Renewals for Microsoft Graph and Fabric channels briefly open a visible browser window when required by Microsoft. Full token lifecycle details are covered in the [documentation](https://lazyteams.agmonetti.workers.dev).

---

## Quick Start

### Option 1: Pre-built Binaries (Recommended)

Download the latest release for Linux, macOS, or Windows from [GitHub Releases](https://github.com/agmonetti/lazyteams/releases):

```bash
# Extract the archive
tar -xzf lazyteams-linux-amd64.tar.gz

# 1. Authenticate once with your work/university account
./lazyteams-auth

# 2. Start the TUI
./lazyteams
```

### Option 2: Build from Source

**Prerequisites:** Go 1.22+ and a Microsoft Teams account (Enterprise or Education).

```bash
# Clone the repository
git clone https://github.com/agmonetti/lazyteams.git
cd lazyteams

# Build both binaries
go build -o lazyteams .
go build -o lazyteams-auth ./cmd/auth-helper/

# Capture tokens (first run, interactive login)
./lazyteams-auth

# Launch lazyteams
./lazyteams
```

> [!TIP]
> First-time setup requires granting Microsoft Graph consent once (see [the setup guide](https://lazyteams.agmonetti.workers.dev/#first-time-setup)). After initial capture, expired tokens are renewed automatically in the background.

---

## Workspaces & Keybindings

`lazyteams` organizes your workflow into four dedicated workspaces:

- <kbd>F1</kbd> **Teams & Channels:** Browse joined teams, channel discussions, and cloud files.
- <kbd>F2</kbd> **Direct Messages:** 1:1 and group chats, unread badges, and contact presence.
- <kbd>F3</kbd> **Activity:** Centralized notification feed (mentions, replies, reactions) with instant thread jump.
- <kbd>F4</kbd> **Assignments:** Classes, coursework, upcoming/overdue tasks, file uploads, and submissions.

Press <kbd>?</kbd> anywhere inside the application to open the interactive keyboard shortcuts cheat sheet.

---

## Self-Updating

Keep `lazyteams` up to date with a single command:

```bash
./lazyteams --update
```

This checks GitHub Releases for new versions, verifies the SHA-256 checksums, and safely replaces both the TUI and the auth helper binaries.

---

## Documentation

Comprehensive guides, architecture overviews, and troubleshooting:

- [Official Documentation](https://lazyteams.agmonetti.workers.dev)
- [First-Time Setup Walkthrough](https://lazyteams.agmonetti.workers.dev/#first-time-setup)
- [Auth Process Demo](https://lazyteams.agmonetti.workers.dev/#auth-process-demo)
- [Troubleshooting Guide](https://lazyteams.agmonetti.workers.dev/troubleshooting.html)
- [FAQ](https://lazyteams.agmonetti.workers.dev/faq.html)
- [Security Model (SECURITY.md)](SECURITY.md)

---

## Disclaimer & Responsible Use

- **Educational and Productivity Tool:** Operates on already-authenticated Microsoft Teams user sessions. Does not distribute proprietary Microsoft binaries or bypass user access controls.
- *Not affiliated with, endorsed by, or sponsored by Microsoft Corporation.* Microsoft and Microsoft Teams are trademarks of Microsoft Corporation.
- Use `lazyteams` only with accounts and organizations you are authorized to access, in compliance with applicable organizational policies.

---

## License

This project is licensed under the **GNU General Public License v3.0 (GPLv3)**. See the [LICENSE](LICENSE) file for details.
