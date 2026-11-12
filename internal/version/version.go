// Package version centralizes the app version and the GitHub update check.
package version

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Version is the current build version. It defaults to "dev" and is meant to
// be overridden at build time via:
//
//	go build -ldflags "-X github.com/agmonetti/lazyteams/internal/version.Version=v1.2.3" ./...
var Version = "dev"

// httpClient is the HTTP client used for the GitHub update check. The timeout
// prevents a stalled network connection from hanging the check goroutine.
var httpClient = &http.Client{Timeout: 10 * time.Second}

// GithubReleasesURL is the GitHub Releases API endpoint for the latest
// release. It is shared by the in-TUI update check and the `--update`
// self-update command. Exported so callers can point tests at a stub URL.
const GithubReleasesURL = "https://api.github.com/repos/agmonetti/lazyteams/releases/latest"

// ErrRateLimited reports that the anonymous GitHub request was rate-limited
// and the optional authenticated gh fallback could not provide the metadata.
// Callers use it to show a quiet notice without failing the whole UI.
var ErrRateLimited = errors.New("github rate limit")

// ghLatestReleaseJSON fetches the release through the authenticated gh CLI.
// It is a variable so tests can supply a fixture without invoking gh.
var ghLatestReleaseJSON = runGHLatestRelease

// ghRun executes gh without a shell and returns stdout only. Stderr is
// deliberately discarded so tokens or diagnostics never reach the UI.
var ghRun = func(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

// ghAPIPath converts the public releases endpoint into the path expected by
// "gh api". Only the public GitHub API is accepted.
func ghAPIPath(apiURL string) (string, error) {
	u, err := url.Parse(apiURL)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" || u.Hostname() != "api.github.com" || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("gh fallback requires the public GitHub API")
	}
	path := strings.TrimPrefix(u.Path, "/")
	if !strings.HasPrefix(path, "repos/") || !strings.HasSuffix(path, "/releases/latest") {
		return "", errors.New("gh fallback cannot derive a releases endpoint")
	}
	return path, nil
}

// runGHLatestRelease asks the authenticated gh CLI for the full release JSON.
// Only the repository path is passed, and no credential is read or logged.
func runGHLatestRelease(apiURL string) ([]byte, error) {
	path, err := ghAPIPath(apiURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	body, err := ghRun(ctx, "api", path)
	if err != nil {
		return nil, fmt.Errorf("gh api: %w", err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, errors.New("gh api: empty response")
	}
	return body, nil
}

// fetchLatestReleaseJSON queries the public API first. A 403 or 429 response
// is treated as a rate limit and the authenticated gh CLI is tried as an
// optional fallback. Any other failure is returned unchanged.
func fetchLatestReleaseJSON(apiURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	// GitHub's API rejects requests without a User-Agent header.
	req.Header.Set("User-Agent", "lazyteams")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return io.ReadAll(resp.Body)
	}
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests {
		return nil, fmt.Errorf("github releases: status %d", resp.StatusCode)
	}

	body, ghErr := ghLatestReleaseJSON(apiURL)
	if ghErr == nil {
		return body, nil
	}
	return nil, fmt.Errorf("%w: status %d; gh fallback failed: %v", ErrRateLimited, resp.StatusCode, ghErr)
}

// Compare compares two semantic version strings (e.g. "v1.2.3") and returns
// -1, 0 or 1. Segments are compared numerically, so "v1.10.0" is newer than
// "v1.9.0". A "dev" build sorts before every release, so users compiling from
// source always see any release as an update.
func Compare(a, b string) int {
	av, bv := parse(a), parse(b)
	for i := 0; i < len(av); i++ {
		if av[i] < bv[i] {
			return -1
		}
		if av[i] > bv[i] {
			return 1
		}
	}
	return 0
}

func parse(s string) [3]int {
	s = strings.TrimPrefix(s, "v")
	if s == "" || s == "dev" {
		return [3]int{}
	}
	var v [3]int
	for i, part := range strings.SplitN(s, ".", 3) {
		// Suffixes like "-beta" fail to parse and are ignored.
		n, _ := strconv.Atoi(part)
		v[i] = n
	}
	return v
}

// LatestRelease fetches the latest release tag from a GitHub releases API URL.
// The caller passes the full URL so tests can point at an httptest server.
func LatestRelease(apiURL string) (string, error) {
	body, err := fetchLatestReleaseJSON(apiURL)
	if err != nil {
		return "", err
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if payload.TagName == "" {
		return "", fmt.Errorf("github releases: empty tag_name")
	}
	return payload.TagName, nil
}
