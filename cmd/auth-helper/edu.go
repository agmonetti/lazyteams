package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
)

var assignmentsSelectors = []string{
	`[data-tid="app-bar-edu-assignments"]`,
	`[role="navigation"] span:text-is("Assignments")`,
	`[role="navigation"] span:text-is("Tareas")`,
	`[role="navigation"] [aria-label="Assignments"]`,
	`[role="navigation"] [aria-label="Tareas"]`,
	`span:text-is("Assignments") >> nth=0`,
	`span:text-is("Tareas") >> nth=0`,
}

func isAssignmentsURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && parsed.Hostname() == "assignments.edu.cloud.microsoft"
}

func pageHasAssignments(page playwright.Page) bool {
	if isAssignmentsURL(page.URL()) {
		return true
	}
	for _, frame := range page.Frames() {
		if isAssignmentsURL(frame.URL()) {
			return true
		}
	}
	return false
}

// freshEduToken checks cache eligibility, not authenticity. Assignments remains
// the authority for token validity. Leave enough lifetime to save and use it.
func freshEduToken(token string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return false
	}
	var claims struct {
		Expiry    int64 `json:"exp"`
		NotBefore int64 `json:"nbf"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Expiry <= now.Add(30*time.Second).Unix() || claims.NotBefore > now.Unix() {
		return false
	}
	aud := extractAudFromJWT(token)
	return aud == "api://8f348934-64be-4bb2-bc16-c54c96789f43" || aud == "8f348934-64be-4bb2-bc16-c54c96789f43"
}

func validateEduCapture(captured *tokens) error {
	captured.mu.Lock()
	token := captured.eduToken
	captured.mu.Unlock()
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if err := validateEduToken(client, token); err != nil {
		return err
	}
	captured.mu.Lock()
	captured.eduValidated = true
	captured.mu.Unlock()
	fmt.Println("  " + symOK + "  Assignments API accepted EDU token.")
	return nil
}

func validateEduToken(client *http.Client, token string) error {
	req, err := http.NewRequest(http.MethodGet, "https://assignments.edu.cloud.microsoft/api/v1.0/edu/me/work?$top=1", nil)
	if err != nil {
		return fmt.Errorf("create Assignments validation request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://teams.microsoft.com")
	req.Header.Set("Referer", "https://teams.microsoft.com/")
	req.Header.Set("X-Ms-Client-Type", "web")
	req.Header.Set("X-Ms-ClientType", "web")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("validate EDU token with Assignments: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Assignments rejected captured EDU token (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// Return every candidate, not the first cache hit. MSAL retains expired entries
// alongside current entries, and Assignments storage may live in an iframe.
const eduStorageScript = `() => {
	const tokens = [];
	for (const storageName of ['localStorage', 'sessionStorage']) {
		try {
			const storage = window[storageName];
			for (const key of Object.keys(storage)) {
				if (!key.toLowerCase().includes('accesstoken')) continue;
				try {
					const entry = JSON.parse(storage.getItem(key));
					if (typeof entry.secret === 'string') tokens.push(entry.secret);
				} catch (_) {}
			}
		} catch (_) {}
	}
	return tokens;
}`
