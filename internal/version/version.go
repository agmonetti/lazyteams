// Package version centralizes the app version and the GitHub update check.
package version

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Version is the current build version. It defaults to "dev" and is meant to
// be overridden at build time via:
//
//	go build -ldflags "-X lazyteams/internal/version.Version=v1.2.3" ./...
var Version = "dev"

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
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	// GitHub's API rejects requests without a User-Agent header.
	req.Header.Set("User-Agent", "lazyteams")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github releases: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
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
