# Security Policy

## Supported Versions

lazyteams is pre-release software. Security fixes are applied to the latest commit on the default branch (`main`). Older commits and tagged releases are not supported separately.

| Version        | Supported |
| -------------- | --------- |
| Latest `main`  | Yes       |
| Older commits  | No        |

## Reporting a Vulnerability

If you discover a security issue, **please do not open a public GitHub issue** with exploit details, tokens, cookies, or session data.

Instead, report it privately:

1. Open a [GitHub Security Advisory](https://github.com/agmonetti/lazyteams/security/advisories/new) (recommended once the repository is public), **or**
2. Contact the maintainer through a private channel (email or direct message).

Include, when possible:

- A clear description of the issue and its impact
- Steps to reproduce
- Affected files or components
- Any suggested fix or mitigation

You should receive an acknowledgment within a reasonable timeframe. We will investigate, coordinate a fix, and publish a disclosure once a patch is available.

Please redact tokens, cookies, JWTs, and personal data from any report.

## Scope

The following are **in scope** for security reports:

- Credential handling, storage, or accidental exposure in the repository
- Local privilege escalation or path traversal (e.g. file download/write outside the intended directory)
- Command injection or unsafe subprocess execution
- Terminal escape-sequence or hyperlink (OSC 8) abuse via untrusted Teams content
- Insecure handling of URLs opened in the system browser
- Authentication helper (`lazyteams-auth`) session or token capture flaws

The following are generally **out of scope**:

- Issues that require full access to the victim's local machine and `~/.config/lazyteams/` (that directory is treated as a trusted secret store, similar to `~/.ssh/`)
- Microsoft Teams, Graph API, or Azure AD vulnerabilities (report those to [Microsoft Security Response Center](https://msrc.microsoft.com/))
- Violations of Microsoft's Terms of Service from using undocumented internal APIs (legal/compliance concern, not an lazyteams code vulnerability)
- Social engineering or phishing against lazyteams users

## Sensitive Data Handled by lazyteams

This client stores and uses credentials locally to operate. Anyone with access to these paths can act as the signed-in Teams user.

| Location | Contents | Permissions |
| -------- | -------- | ----------- |
| `~/.config/lazyteams/tokens.env` | JWT tokens and session cookies | `0600` (intended) |
| `~/.config/lazyteams/browser-session/` | Persistent Playwright/Firefox session | `0700` (intended) |
| `~/.config/lazyteams/prefs.json` | UI preferences (download directory, hidden teams/channels) | `0600` (intended) |

**Never commit** `tokens.env`, `browser-session/`, or `prefs.json` to version control. Do not share these files or paste their contents in issues, pull requests, or chat logs.

Environment variables (`MS_GRAPH_TOKEN`, `TEAMS_WEB_TOKEN`, etc.) override values from `tokens.env` when set.

## Security Recommendations for Users

- Run `./lazyteams-auth` only on machines you trust.
- Keep `~/.config/lazyteams/` readable only by your user account.
- Do not run lazyteams on shared or multi-user systems without understanding the credential exposure.
- Revoke or rotate Microsoft sessions if you suspect your token files were copied.
- Build lazyteams from source when possible; do not run untrusted pre-built binaries.

## Update Integrity

`lazyteams --update` downloads the self-hosted release binaries over HTTPS from
`github.com` and verifies each against the `SHA256SUMS` file shipped with the
same GitHub release before replacing the running binaries. The updater refuses
to proceed if a release does not publish `SHA256SUMS`, and aborts on any
checksum mismatch. This protects against corrupted or tampered binaries during
transport, but it is not a full code-signing scheme: the checksum is published
alongside the binaries it protects, so a GitHub compromise (or a compromised
maintainer account) could alter both together. Treat `--update` output as
trusted only if you already trust the release author.

## Security Recommendations for Contributors

- Do not add hardcoded tokens, cookies, or API keys to the codebase.
- Do not commit compiled binaries, debug dumps, or local config files.
- Use `json.Marshal` (or equivalent) for API payloads; avoid string-interpolated JSON.
- Escape user-controlled content before embedding it in HTML sent to Teams APIs.
- Sanitize file names from remote APIs before writing to disk (`filepath.Base`, validate resolved path stays within the destination directory).
- Validate URLs before passing them to `xdg-open`, `open`, or terminal hyperlink (OSC 8) sequences; restrict to expected schemes (typically `https://`).
- Remove temporary debug logging before merging.

## Disclosure Policy

We follow coordinated disclosure:

1. Reporter submits a private report.
2. Maintainers confirm and develop a fix.
3. A patch is released on `main`.
4. A public advisory or release note is published with credit to the reporter (unless they prefer to remain anonymous).

## Disclaimer

lazyteams is an independent, educational terminal client. It is **not affiliated with, endorsed by, or supported by Microsoft Corporation**. Use at your own risk and in compliance with your organization's policies and Microsoft's terms of service.
