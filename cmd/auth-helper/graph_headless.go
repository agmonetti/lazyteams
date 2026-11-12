package main

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/agmonetti/lazyteams/internal/auth"
	"github.com/agmonetti/lazyteams/internal/helpers"
	playwright "github.com/mxschmitt/playwright-go"
)

// The account is recorded automatically upon first validated Graph sign-in
// or designated manually in a private file (0600) in the config directory,
// never inferred from a selector. Without it Graph sign-in stays manual.
func graphAccount() string {
	path := filepath.Join(helpers.ConfigDir(), "graph-account")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 256 {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// rememberGraphAccount records the authenticated account identity into graph-account
// (permissions 0600) so future headless Graph Explorer renewals can automatically
// select this account. It only saves the minimal identifier (UPN/email), never tokens.
func rememberGraphAccount(configDir, token string) {
	account := auth.ParseAccountFromToken(token)
	if account == "" {
		return
	}
	if configDir == "" {
		configDir = helpers.ConfigDir()
	}
	path := filepath.Join(configDir, "graph-account")
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm() == 0600 {
		if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) == account {
			return
		}
	}
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(configDir, ".graph-account-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return
	}
	if _, err := tmp.WriteString(account + "\n"); err != nil {
		_ = tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	_ = os.Rename(tmpName, path)
}

func graphUniqueVisible(l playwright.Locator) bool {
	n, err := l.Count()
	if err != nil || n != 1 {
		return false
	}
	visible, err := l.IsVisible()
	return err == nil && visible
}

func graphChallenge(p playwright.Page) bool {
	for _, l := range []playwright.Locator{
		p.Locator(`input[type="password"], input[autocomplete="one-time-code"], input[name="otc"]`),
		p.GetByText(regexp.MustCompile(`(?i)more information required|verify your identity|enter code|sign in another way|permissions requested|stay signed in|consent|approve`)),
	} {
		n, err := l.Count()
		if err != nil {
			return true
		}
		for i := 0; i < n; i++ {
			visible, err := l.Nth(i).IsVisible()
			if err != nil || visible {
				return true
			}
		}
	}
	return false
}

var graphMicrosoftOrigin = "https://login.microsoftonline.com"

var graphSignInSettleTimeout = 10 * time.Second

func graphMicrosoftPage(raw, origin string) bool {
	u, err := url.Parse(raw)
	o, e := url.Parse(origin)
	return err == nil && e == nil && u.Scheme == o.Scheme && u.Host == o.Host && strings.HasPrefix(u.Path, o.Path)
}

// Fail closed on unknown windows, challenges and ambiguous accounts. No credentials or consent are entered.
func graphSelectAccount(page playwright.Page, account, origin string) bool {
	var selector playwright.Page
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		matches := 0
		for _, p := range page.Context().Pages() {
			if graphMicrosoftPage(p.URL(), origin) {
				selector = p
				matches++
			}
		}
		if matches > 1 {
			return false
		}
		if matches == 1 {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if selector == nil {
		return false
	}
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if graphChallenge(selector) {
			return false
		}
		label := selector.GetByText(account, playwright.PageGetByTextOptions{Exact: playwright.Bool(true)})
		n, err := label.Count()
		if err != nil || n > 1 {
			return false
		}
		if graphUniqueVisible(label) {
			control := label.Locator("xpath=ancestor::*[self::button or @role='button'][1]")
			if !graphUniqueVisible(control) {
				return false
			}
			text, err := control.InnerText()
			if err != nil || !regexp.MustCompile(`(?i)\bsigned in\b`).MatchString(text) || graphChallenge(selector) || !graphMicrosoftPage(selector.URL(), origin) {
				return false
			}
			err = control.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(3000)})
			return err == nil || selector.IsClosed() // popup may close before the click is acknowledged
		}
		time.Sleep(150 * time.Millisecond)
	}
	return false
}

func hasGraphCandidate(captured *tokens) bool {
	captured.mu.Lock()
	defer captured.mu.Unlock()
	return captured.graphCandidate != nil
}

// graphSignInCompleted reports whether sign-in finished without the account
// card: with a live Microsoft session the popup signs in by itself and closes.
// Completion means a Graph candidate was observed, or no Microsoft window is
// left and Graph Explorer no longer offers "Sign in". A window that stays open
// (challenge, MFA, consent, unknown UI) still requires interaction.
func graphSignInCompleted(page playwright.Page, signIn playwright.Locator, captured *tokens) bool {
	deadline := time.Now().Add(graphSignInSettleTimeout)
	for {
		if hasGraphCandidate(captured) {
			return true // the candidate is still validated before it is persisted
		}
		if page.IsClosed() {
			return false
		}
		pending := false
		for _, p := range page.Context().Pages() {
			if !p.IsClosed() && graphMicrosoftPage(p.URL(), graphMicrosoftOrigin) {
				if graphChallenge(p) {
					return false
				}
				pending = true
			}
		}
		if !pending {
			if n, err := signIn.Count(); err == nil && n == 0 {
				return true
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func tryGraphHeadlessSignIn(page playwright.Page, captured *tokens) string {
	button := page.Locator(`button[aria-label="Sign in"], button[aria-label="Sign in to Graph Explorer"]`)
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		n, err := button.Count()
		if err != nil || n > 1 {
			return "interaction_required"
		}
		if n == 1 {
			account := graphAccount()
			if !graphUniqueVisible(button) || account == "" || graphChallenge(page) {
				return "interaction_required"
			}
			if err := button.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(3000)}); err != nil {
				return "interaction_required"
			}
			if !graphSelectAccount(page, account, graphMicrosoftOrigin) && !graphSignInCompleted(page, button, captured) {
				return "interaction_required"
			}
			return ""
		}
		if hasGraphCandidate(captured) {
			return ""
		}
		time.Sleep(150 * time.Millisecond)
	}
	return "" // already signed in: retain the existing Run query path
}
