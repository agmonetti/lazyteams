package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mxschmitt/playwright-go"

	"teamsTUI/internal/helpers"
)

type tokens struct {
	mu          sync.Mutex
	graphToken  string
	webToken    string
	notifToken  string
	eduToken    string
	cookie      string
	spacesToken string
	fabricToken string
}

var globalSpin *spinner

func printBox(lines []string) {
	width := 47
	fmt.Println("  ┌" + strings.Repeat("─", width) + "┐")
	for _, line := range lines {
		runes := []rune(line)
		padding := width - len(runes) - 1
		if padding < 0 {
			padding = 0
		}
		fmt.Printf("  │ %s%s│\n", line, strings.Repeat(" ", padding))
	}
	fmt.Println("  └" + strings.Repeat("─", width) + "┘")
}

func notifyTokenCaptured(name string) {
	if globalSpin != nil {
		fmt.Printf("\r\033[K  ✓  %-25s\n", name)
		globalSpin.SetLabel("Waiting for next token...")
	} else {
		fmt.Printf("  ✓  %-25s\n", name)
	}
}

func (t *tokens) allCaptured() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.graphToken != "" &&
		t.webToken != "" &&
		t.notifToken != "" &&
		t.eduToken != "" &&
		t.cookie != ""
}

func printTokenStatus(t *tokens) {
	t.mu.Lock()
	defer t.mu.Unlock()

	type tokenInfo struct {
		name string
		val  string
	}
	tokens := []tokenInfo{
		{"MS_GRAPH_TOKEN     ", t.graphToken},
		{"TEAMS_WEB_TOKEN    ", t.webToken},
		{"TEAMS_NOTIF_TOKEN  ", t.notifToken},
		{"EDU_TOKEN          ", t.eduToken},
		{"TEAMS_COOKIE       ", t.cookie},
		{"TEAMS_SPACES_TOKEN ", t.spacesToken},
		{"TEAMS_FABRIC_TOKEN ", t.fabricToken},
	}

	captured := 0
	fmt.Println("  Token status:")
	fmt.Println()
	for _, tk := range tokens {
		if tk.val != "" {
			fmt.Printf("  ✓  %s\n", tk.name)
			captured++
		} else {
			fmt.Printf("  ·  %s\n", tk.name)
		}
	}
	fmt.Printf("\n  %d/7 captured\n", captured)
}

func main() {
	initConsole()

	fmt.Println("╔════════════════════════════════════════════╗")
	fmt.Println("║            msTTui — Auth Helper -          ║")
	fmt.Println("╚════════════════════════════════════════════╝")
	fmt.Println()

	configDir := helpers.ConfigDir()
	sessionDir := filepath.Join(configDir, "browser-session")

	os.MkdirAll(sessionDir, 0700)

	// Parse --renew flag
	renewOnly := ""
	showBrowser := false
	forceHeadless := false
	clearSession := false
	clearTokens := false
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		if args[i] == "--renew" && i+1 < len(args) {
			renewOnly = args[i+1]
		}
		if args[i] == "--show" {
			showBrowser = true
		}
		if args[i] == "--headless" {
			forceHeadless = true
		}
		if args[i] == "--clear-session" {
			clearSession = true
		}
		if args[i] == "--clear-tokens" {
			clearTokens = true
		}
	}

	if clearSession {
		fmt.Println("→ Clearing browser session...")
		if err := os.RemoveAll(sessionDir); err != nil {
			fmt.Printf("  ⚠ Error clearing session: %v\n", err)
		} else {
			fmt.Println("  ✓ Browser session cleared.")
			fmt.Println("  Next run will require login.")
		}
		if !clearTokens {
			os.Exit(0)
		}
	}

	if clearTokens {
		fmt.Println("→ Clearing saved tokens...")
		tokensPath := filepath.Join(configDir, "tokens.env")
		if err := os.Remove(tokensPath); err != nil && !os.IsNotExist(err) {
			fmt.Printf("  ⚠ Error clearing tokens: %v\n", err)
		} else {
			fmt.Println("  ✓ Tokens cleared.")
		}
		os.Exit(0)
	}

	firstRun := !sessionExists(sessionDir)
	if firstRun {
		showBrowser = true
		fmt.Println("  ● First run — browser will open for login")
	} else {
		fmt.Println("  ● Session found — running headless")
	}

	if forceHeadless {
		showBrowser = false
	}
	fmt.Println()

	if !showBrowser {
		fmt.Println("→ Running in background (no browser window).")
		fmt.Println("  Run with --show to open browser manually.")
	} else {
		fmt.Println("→ Browser window will open.")
		if firstRun {
			fmt.Println("  Please sign in to your Microsoft account.")
		}
	}
	fmt.Println()

	fmt.Println("→ Verifying browser...")

	// Check if browser is already installed to avoid re-download
	browserExists := false
	// Linux: ~/.cache/ms-playwright, Windows: %USERPROFILE%\AppData\Local\ms-playwright
	for _, dir := range []string{
		filepath.Join(helpers.HomeDir(), ".cache", "ms-playwright"),
		filepath.Join(helpers.HomeDir(), "AppData", "Local", "ms-playwright"),
	} {
		if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
			browserExists = true
			break
		}
	}

	if !browserExists {
		fmt.Println("→ Downloading browser (one-time only, ~80MB)...")
		if err := playwright.Install(&playwright.RunOptions{
			Browsers: []string{"firefox"},
		}); err != nil {
			fmt.Printf("Error installing browser: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Install silently without output
		playwright.Install(&playwright.RunOptions{
			Browsers: []string{"firefox"},
			Verbose:  false,
		})
	}

	patchWebSocketAsserts()

	pw, err := playwright.Run()
	if err != nil {
		fmt.Printf("Error starting Playwright: %v\n", err)
		os.Exit(1)
	}
	defer pw.Stop()

	context, err := pw.Firefox.LaunchPersistentContext(
		sessionDir,
		playwright.BrowserTypeLaunchPersistentContextOptions{
			Headless: playwright.Bool(!showBrowser),
			SlowMo:   playwright.Float(0),
			Args: []string{
				"--no-first-run",
			},
		},
	)
	if err != nil {
		fmt.Printf("Error launching browser: %v\n", err)
		os.Exit(1)
	}
	defer context.Close()

	captured := &tokens{}

	// Detect unexpected browser close and clear session if capture incomplete
	context.On("close", func() {
		captured.mu.Lock()
		incomplete := captured.graphToken == "" || captured.webToken == "" || captured.cookie == ""
		captured.mu.Unlock()
		if incomplete {
			os.RemoveAll(sessionDir)
		}
	})

	// Catch Ctrl+C to save tokens before exiting
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n\n[!] Interrupted by user. Saving captured tokens...")

		// If capture was incomplete, clear the browser session
		// so the next run forces re-login
		captured.mu.Lock()
		incomplete := captured.graphToken == "" || captured.webToken == "" || captured.cookie == ""
		captured.mu.Unlock()

		if incomplete {
			fmt.Println("  ⚠ Incomplete session — clearing browser session to force re-login next run.")
			os.RemoveAll(sessionDir)
		}

		if err := saveTokens(captured, configDir); err != nil {
			fmt.Printf("Error saving tokens: %v\n", err)
		} else {
			fmt.Println("Tokens saved.")
		}
		os.Exit(1)
	}()

	context.On("request", func(req playwright.Request) {
		url := req.URL()
		headers := req.Headers()

		// Capture cookies from the Cookie header of each Teams request
		captured.mu.Lock()
		if captured.cookie == "" {
			for k, v := range headers {
				if strings.ToLower(k) == "cookie" && v != "" {
					if strings.Contains(url, "teams.microsoft.com") ||
						strings.Contains(url, "ic3.teams.office.com") ||
						strings.Contains(url, "graph.microsoft.com") ||
						strings.Contains(url, "substrate.office.com") ||
						strings.Contains(url, "sharepoint.com") {
						captured.cookie = v
						notifyTokenCaptured("TEAMS_COOKIE")
					}
					break
				}
			}
		}
		captured.mu.Unlock()

		// Capture Bearer tokens
		authHeader := ""
		for k, v := range headers {
			if strings.ToLower(k) == "authorization" {
				authHeader = v
				break
			}
		}

		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		aud := extractAudFromJWT(token)

		captured.mu.Lock()
		defer captured.mu.Unlock()

		switch {
		// MS_GRAPH_TOKEN: ONLY from graph.microsoft.com direct requests
		// SharePoint and substrate tokens don't have the required scopes
		case (strings.Contains(aud, "graph.microsoft.com") || aud == "00000003-0000-0000-c000-000000000000") &&
			strings.Contains(url, "graph.microsoft.com") &&
			captured.graphToken == "":
			captured.graphToken = token
			notifyTokenCaptured("MS_GRAPH_TOKEN")

		// TEAMS_NOTIF_TOKEN and TEAMS_WEB_TOKEN
		case strings.Contains(aud, "ic3.teams.office.com"):
			if (strings.Contains(url, "48%3Anotifications") ||
				strings.Contains(url, "48:notifications")) &&
				captured.notifToken == "" {
				captured.notifToken = token
				notifyTokenCaptured("TEAMS_NOTIF_TOKEN")
			} else if strings.Contains(url, "chatsvc") && captured.webToken == "" {
				captured.webToken = token
				notifyTokenCaptured("TEAMS_WEB_TOKEN")
			}

		// EDU_TOKEN
		case strings.Contains(aud, "8f348934") && captured.eduToken == "":
			captured.eduToken = token
			notifyTokenCaptured("EDU_TOKEN")

		case strings.Contains(url, "assignments.edu.cloud.microsoft") && captured.eduToken == "":
			captured.eduToken = token
			notifyTokenCaptured("EDU_TOKEN")

		case strings.Contains(aud, "api.spaces.skype.com") && captured.spacesToken == "":
			captured.spacesToken = token
			notifyTokenCaptured("TEAMS_SPACES_TOKEN")

		case strings.Contains(url, "fabric/"):
			if captured.fabricToken == "" {
				captured.fabricToken = token
				notifyTokenCaptured("TEAMS_FABRIC_TOKEN")
			}

		}
	})

	// Use the page that the persistent context already opened
	pages := context.Pages()
	var page playwright.Page
	if len(pages) > 0 {
		page = pages[0]
	} else {
		page, err = context.NewPage()
		if err != nil {
			fmt.Printf("Error creating page: %v\n", err)
			os.Exit(1)
		}
	}

	if err := page.SetViewportSize(1280, 800); err != nil {
		fmt.Printf("Error setting viewport: %v\n", err)
	}

	switch renewOnly {
	case "web", "notif":
		fmt.Println("→ Renewing WEB + NOTIF tokens...")
		renewWebNotif(page, context, captured)

	case "edu":
		fmt.Println("→ Renewing EDU token...")
		renewEdu(page, captured)

	case "fabric":
		fmt.Println("→ Renewing FABRIC token...")
		context.Close()
		manualFabricTokenCapture(pw, sessionDir, captured)

	case "graph":
		fmt.Println("→ Renewing GRAPH token...")
		context.Close()
		visibleCtx, err := pw.Firefox.LaunchPersistentContext(
			sessionDir,
			playwright.BrowserTypeLaunchPersistentContextOptions{
				Headless: playwright.Bool(false),
				Args:     []string{"--no-first-run"},
			},
		)
		if err != nil {
			fmt.Printf("  ⚠ Could not open browser: %v\n", err)
			break
		}
		defer visibleCtx.Close()

		visibleCtx.On("request", func(req playwright.Request) {
			authHeader := ""
			for k, v := range req.Headers() {
				if strings.ToLower(k) == "authorization" {
					authHeader = v
					break
				}
			}
			if !strings.HasPrefix(authHeader, "Bearer ") {
				return
			}
			token := strings.TrimPrefix(authHeader, "Bearer ")
			aud := extractAudFromJWT(token)
			if strings.Contains(aud, "graph.microsoft.com") || aud == "00000003-0000-0000-c000-000000000000" {
				captured.mu.Lock()
				if captured.graphToken == "" {
					captured.graphToken = token
					notifyTokenCaptured("MS_GRAPH_TOKEN")
				}
				captured.mu.Unlock()
			}
		})

		pages := visibleCtx.Pages()
		var visiblePage playwright.Page
		if len(pages) > 0 {
			visiblePage = pages[0]
		} else {
			visiblePage, _ = visibleCtx.NewPage()
		}
		visiblePage.SetViewportSize(1280, 800)

		fmt.Println()
		fmt.Println("→ Browser opening Graph Explorer.")
		fmt.Println("  Sign in with your account and the token")
		fmt.Println("  will be captured automatically.")
		fmt.Println()

		tryExtractGraphTokenViaGraphExplorer(visiblePage, captured, !firstRun)

		deadline := time.Now().Add(120 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(500 * time.Millisecond)
			captured.mu.Lock()
			has := captured.graphToken != ""
			captured.mu.Unlock()
			if has {
				fmt.Println("  ✓ Token captured! Browser closes in 3s...")
				time.Sleep(3 * time.Second)
				break
			}
		}

	default:
		fmt.Println("→ Full token capture...")
		if firstRun {
			fmt.Println("→ Sign in. After logging in, Teams will load")
			fmt.Println("  and tokens will be captured automatically.")
		} else {
			fmt.Println("→ Teams will load automatically.")
		}
		fullRenewal(pw, page, context, captured, sessionDir, firstRun)
	}

	fmt.Println()
	if captured.allCaptured() {
		fmt.Println("✓ All tokens captured.")
	} else {
		fmt.Println("⚠ Partial tokens — saving what's available.")
	}
	printTokenStatus(captured)

	if err := saveTokens(captured, configDir); err != nil {
		fmt.Printf("Error saving tokens: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("→ Session saved. Next time no login is required.")
	fmt.Println("→ You can now run: ./msTTui")
}

func sessionExists(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

func saveTokens(t *tokens, configDir string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	tokensPath := filepath.Join(configDir, "tokens.env")

	// Merge: load existing tokens, overwrite only non-empty values
	existing := make(map[string]string)
	if data, err := os.ReadFile(tokensPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "export ") {
				line = strings.TrimPrefix(line, "export ")
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				val := strings.Trim(strings.TrimSpace(parts[1]), `"`)
				existing[key] = val
			}
		}
	}

	// Apply new values (non-empty wins)
	if t.graphToken != "" {
		existing["MS_GRAPH_TOKEN"] = t.graphToken
	}
	if t.webToken != "" {
		existing["TEAMS_WEB_TOKEN"] = t.webToken
	}
	if t.notifToken != "" {
		existing["TEAMS_NOTIF_TOKEN"] = t.notifToken
	}
	if t.eduToken != "" {
		existing["EDU_TOKEN"] = t.eduToken
	}
	if t.cookie != "" {
		existing["TEAMS_COOKIE"] = t.cookie
	}
	if t.spacesToken != "" {
		existing["TEAMS_SPACES_TOKEN"] = t.spacesToken
	}
	if t.fabricToken != "" {
		existing["TEAMS_FABRIC_TOKEN"] = t.fabricToken
	}

	content := fmt.Sprintf(`# msTTui tokens — auto-generated on %s
# Do not edit manually. Run ./msTTui-auth to renew.

export MS_GRAPH_TOKEN="%s"
export TEAMS_WEB_TOKEN="%s"
export TEAMS_NOTIF_TOKEN="%s"
export EDU_TOKEN="%s"
export TEAMS_COOKIE="%s"
export TEAMS_SPACES_TOKEN="%s"
export TEAMS_FABRIC_TOKEN="%s"
`,
		time.Now().Format(time.RFC3339),
		existing["MS_GRAPH_TOKEN"],
		existing["TEAMS_WEB_TOKEN"],
		existing["TEAMS_NOTIF_TOKEN"],
		existing["EDU_TOKEN"],
		existing["TEAMS_COOKIE"],
		existing["TEAMS_SPACES_TOKEN"],
		existing["TEAMS_FABRIC_TOKEN"],
	)

	if err := os.WriteFile(tokensPath, []byte(content), 0600); err != nil {
		return fmt.Errorf("error writing tokens: %w", err)
	}

	fmt.Printf("→ Tokens saved to: %s\n", tokensPath)
	return nil
}

// renewWebNotif navigates Teams to trigger WEB + NOTIF token requests.
func renewWebNotif(page playwright.Page, ctx playwright.BrowserContext, captured *tokens) {
	page.Goto("https://teams.microsoft.com", playwright.PageGotoOptions{
		Timeout:   playwright.Float(60000),
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	})

	time.Sleep(5 * time.Second)

	// Navigate to Teams section to trigger SPACES_TOKEN
	page.Goto("https://teams.microsoft.com/v2/#/teams/", playwright.PageGotoOptions{
		Timeout: playwright.Float(15000),
	})
	time.Sleep(5 * time.Second)

	deadline := time.Now().Add(30 * time.Second)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		<-ticker.C
		captured.mu.Lock()
		hasWeb := captured.webToken != ""
		hasNotif := captured.notifToken != ""
		captured.mu.Unlock()

		if hasWeb && hasNotif {
			notifyTokenCaptured("WEB + NOTIF")
			break
		}

		// If we have web but not notif, trigger manual fetch
		captured.mu.Lock()
		webTok := captured.webToken
		captured.mu.Unlock()
		if webTok != "" && !hasNotif {
			page.Evaluate(`(token) => {
				fetch("https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/48:notifications/messages?startTime=0&pageSize=1&view=msnp24Equivalent&targetType=Passport", {
					headers: {
						"Authorization": "Bearer " + token,
						"X-Ms-Client-Type": "web"
					}
				})
			}`, webTok)
		}
	}

	// NOTIF fallback
	captured.mu.Lock()
	if captured.notifToken == "" && captured.webToken != "" {
		captured.notifToken = captured.webToken
		notifyTokenCaptured("TEAMS_NOTIF_TOKEN")
	}
	captured.mu.Unlock()

	// Cookies
	cookies, err := ctx.Cookies("https://teams.microsoft.com")
	if err == nil {
		var parts []string
		for _, c := range cookies {
			if c.Name != "" && c.Value != "" {
				parts = append(parts, fmt.Sprintf("%s=%s", c.Name, c.Value))
			}
		}
		if len(parts) > 0 {
			captured.mu.Lock()
			captured.cookie = strings.Join(parts, "; ")
			captured.mu.Unlock()
			notifyTokenCaptured("TEAMS_COOKIE")
		}
	}
}

// renewEdu navigates Teams to trigger EDU token request.
func renewEdu(page playwright.Page, captured *tokens) {
	_, err := page.Goto("https://teams.microsoft.com/v2/",
		playwright.PageGotoOptions{Timeout: playwright.Float(60000)},
	)
	if err != nil {
		fmt.Println("  ⚠ Teams slow to load, continuing...")
	}

	time.Sleep(3 * time.Second)

	selectors := []string{
		`[role="navigation"] span:text-is("Assignments")`,
		`[data-tid="app-bar-edu-assignments"]`,
		`span:text-is("Assignments") >> nth=0`,
	}

	clicked := false
	for _, sel := range selectors {
		err := page.Locator(sel).First().Click(playwright.LocatorClickOptions{
			Timeout: playwright.Float(3000),
		})
		if err == nil {
			fmt.Printf("→ Clicked Assignments sidebar (%s)\n", sel)
			clicked = true

			deadline := time.Now().Add(12 * time.Second)
			for time.Now().Before(deadline) {
				time.Sleep(500 * time.Millisecond)
				if strings.Contains(page.URL(), "assignments.edu.cloud.microsoft") {
					time.Sleep(2 * time.Second)
					break
				}
			}
			break
		}
	}

	if !clicked {
		time.Sleep(15 * time.Second)
	}

	// Try JS extraction
	tryExtractEduTokenFromJS(page, captured)
}

// fullRenewal runs the full capture flow (all 5 tokens).
func fullRenewal(pw *playwright.Playwright, page playwright.Page, ctx playwright.BrowserContext, captured *tokens, sessionDir string, firstRun bool) {
	globalSpin = newSpinner("Connecting to Teams...")
	globalSpin.Start()
	defer func() {
		if globalSpin != nil {
			globalSpin.Stop("")
			globalSpin = nil
		}
	}()

	// Open the base URL — no networkidle wait, let the user interact with login
	_, err := page.Goto("https://teams.microsoft.com",
		playwright.PageGotoOptions{
			Timeout: playwright.Float(90000),
		},
	)
	if err != nil {
		fmt.Println("  ⚠ Teams slow to load or auth required, continuing...")
	}

	if globalSpin != nil {
		globalSpin.SetLabel("Waiting for login...")
	}

	// Wait for login FIRST — up to 5 minutes on first run
	loginTimeout := 3 * time.Minute
	if firstRun {
		loginTimeout = 5 * time.Minute
	}
	// On first run, wait for the interceptor to capture the first token
	// as a signal that Teams loaded and login completed.
	// On subsequent runs, just wait briefly.
	if firstRun {
		if globalSpin != nil {
			globalSpin.SetLabel("Waiting for you to sign in...")
		}
		loginDeadline := time.Now().Add(loginTimeout)
		for time.Now().Before(loginDeadline) {
			time.Sleep(1 * time.Second)
			// WebToken or SpacesToken only appear when Teams has fully loaded
			captured.mu.Lock()
			hasWeb := captured.webToken != ""
			hasSpaces := captured.spacesToken != ""
			captured.mu.Unlock()
			if hasWeb || hasSpaces {
				if globalSpin != nil {
					globalSpin.SetLabel("Teams loaded!")
				}
				time.Sleep(2 * time.Second)
				break
			}
		}
	} else {
		time.Sleep(5 * time.Second)
	}

	// Now navigate to /v2/#/teams/ to trigger tokens
	if globalSpin != nil {
		globalSpin.SetLabel("Navigating to Teams to capture tokens...")
	}
	page.Goto("https://teams.microsoft.com/v2/#/teams/", playwright.PageGotoOptions{
		Timeout: playwright.Float(15000),
	})
	time.Sleep(5 * time.Second)

	// Step: try fabric token from storage

	startTime := time.Now()
	deadline := startTime.Add(90 * time.Second)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		<-ticker.C
		elapsed := time.Since(startTime)

		captured.mu.Lock()
		hasGraph := captured.graphToken != ""
		hasWeb := captured.webToken != ""
		hasNotif := captured.notifToken != ""
		hasEdu := captured.eduToken != ""
		hasCookie := captured.cookie != ""
		hasFabric := captured.fabricToken != ""
		hasSpaces := captured.spacesToken != ""
		captured.mu.Unlock()

		if hasGraph && hasWeb && hasNotif && hasEdu && hasCookie && hasSpaces && hasFabric {
			notifyTokenCaptured("All tokens")
			break
		}

		if hasWeb && hasNotif && hasEdu && hasCookie && hasSpaces && !hasGraph {
			break // Exit loop early, go straight to Graph Explorer
		}

		// Step 2 (20s): trigger TEAMS_NOTIF_TOKEN
		if elapsed > 20*time.Second && !hasNotif {
			globalSpin.SetLabel("Navigating to Activity to trigger notifications token...")
			page.Goto("https://teams.microsoft.com/v2/",
				playwright.PageGotoOptions{Timeout: playwright.Float(15000)},
			)
			time.Sleep(2 * time.Second)
			captured.mu.Lock()
			webTok := captured.webToken
			captured.mu.Unlock()
			if webTok != "" {
				page.Evaluate(`(token) => {
					fetch("https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/48:notifications/messages?startTime=0&pageSize=1&view=msnp24Equivalent&targetType=Passport", {
						headers: {
							"Authorization": "Bearer " + token,
							"X-Ms-Client-Type": "web"
						}
					})
				}`, webTok)
				time.Sleep(2 * time.Second)
			}
		}

		// Step 3 (40s): trigger EDU_TOKEN
		if elapsed > 40*time.Second && !hasEdu {
			globalSpin.SetLabel("Navigating to Assignments...")
			page.Goto("https://teams.microsoft.com/v2/",
				playwright.PageGotoOptions{
					Timeout:   playwright.Float(15000),
					WaitUntil: playwright.WaitUntilStateNetworkidle,
				},
			)
			time.Sleep(5 * time.Second)

			page.Locator(`[role="navigation"] span:text-is("Assignments")`).First().Click(
				playwright.LocatorClickOptions{Timeout: playwright.Float(5000)},
			)
			time.Sleep(5 * time.Second)
		}

		// Step 4 (55s): try to extract EDU_TOKEN from JS
		// Step 5 (70s): Navigate to private channel to get FabricToken
		if elapsed > 70*time.Second && !hasFabric && hasGraph {
			globalSpin.SetLabel("Automating TEAMS_FABRIC_TOKEN capture...")
			manualFabricTokenCapture(pw, sessionDir, captured)
		}

		if elapsed > 55*time.Second && !hasEdu {
			globalSpin.SetLabel("Attempting to extract EDU_TOKEN from JS memory...")
			tryExtractEduTokenFromJS(page, captured)
		}
	}

	// TEAMS_NOTIF_TOKEN = TEAMS_WEB_TOKEN
	captured.mu.Lock()
	if captured.notifToken == "" && captured.webToken != "" {
		captured.notifToken = captured.webToken
		notifyTokenCaptured("TEAMS_NOTIF_TOKEN")
	}
	captured.mu.Unlock()

	// Plan B: Graph Explorer for MS_GRAPH_TOKEN
	captured.mu.Lock()
	needsGraphToken := captured.graphToken == ""
	captured.mu.Unlock()

	if needsGraphToken {
		if globalSpin != nil {
			globalSpin.Stop("")
			globalSpin = nil
		}

		fmt.Println()
		printBox([]string{
			"Action required: MS_GRAPH_TOKEN",
			"Opening browser — sign in to Graph Explorer",
		})
		fmt.Println()

		// Close headless context and reopen visible
		ctx.Close()

		visibleCtx, err := pw.Firefox.LaunchPersistentContext(
			sessionDir,
			playwright.BrowserTypeLaunchPersistentContextOptions{
				Headless: playwright.Bool(false),
				SlowMo:   playwright.Float(0),
				Args:     []string{"--no-first-run"},
			},
		)
		if err != nil {
			fmt.Printf("  ⚠ Could not open browser: %v\n", err)
		} else {
			// Re-register interceptor on new context
			visibleCtx.On("request", func(req playwright.Request) {
				authHeader := ""
				for k, v := range req.Headers() {
					if strings.ToLower(k) == "authorization" {
						authHeader = v
						break
					}
				}
				if !strings.HasPrefix(authHeader, "Bearer ") {
					return
				}
				token := strings.TrimPrefix(authHeader, "Bearer ")
				
				// 1. Check for Graph Token
				aud := extractAudFromJWT(token)
				if strings.Contains(aud, "graph.microsoft.com") || aud == "00000003-0000-0000-c000-000000000000" {
					captured.mu.Lock()
					if captured.graphToken == "" {
						captured.graphToken = token
						notifyTokenCaptured("MS_GRAPH_TOKEN")
					}
					captured.mu.Unlock()
				}

				// 2. Check for Fabric Token
				if strings.Contains(req.URL(), "fabric/") {
					captured.mu.Lock()
					if captured.fabricToken == "" {
						captured.fabricToken = token
						notifyTokenCaptured("TEAMS_FABRIC_TOKEN")
					}
					captured.mu.Unlock()
				}
			})

			pages := visibleCtx.Pages()
			var visiblePage playwright.Page
			if len(pages) > 0 {
				visiblePage = pages[0]
			} else {
				visiblePage, _ = visibleCtx.NewPage()
			}
			visiblePage.SetViewportSize(1280, 800)

			// Navigate to Graph Explorer and optionally auto-click Run query
			tryExtractGraphTokenViaGraphExplorer(visiblePage, captured, !firstRun)

			// Wait for token if not yet captured
			graphDeadline := time.Now().Add(300 * time.Second)
			for time.Now().Before(graphDeadline) {
				time.Sleep(500 * time.Millisecond)
				captured.mu.Lock()
				hasGraph := captured.graphToken != ""
				captured.mu.Unlock()
				if hasGraph {
					break
				}
			}

			// Do not close visibleCtx — reuse it for fabric token
			if captured.fabricToken == "" {
				fmt.Println()
				printBox([]string{
					"Action required: TEAMS_FABRIC_TOKEN",
					"1. Go to any Private or Shared Channel.",
					"2. Go to the 'Members' tab.",
					"3. Try to change your own role", 
					"	(e.g., Owner -> Member)",
					"   (It will fail if you are the only owner, but",
					"   the token will be captured instantly!)",
					"Press [Enter] in this terminal to skip.",
				})

				globalSpin = newSpinner("Capturing fabric token...")
				globalSpin.Start()

				// Navigate to Teams in the same browser
				visiblePage.Goto("https://teams.microsoft.com/v2/",
					playwright.PageGotoOptions{Timeout: playwright.Float(20000)},
				)

				// Channel for skip
				skipCh := make(chan struct{}, 1)
				go func() {
					buf := make([]byte, 1)
					if _, err := os.Stdin.Read(buf); err == nil {
						close(skipCh)
					}
				}()

				// Wait up to 2 minutes
				deadline := time.Now().Add(120 * time.Second)
				for time.Now().Before(deadline) {
					select {
					case <-skipCh:
						fmt.Println("\n  · TEAMS_FABRIC_TOKEN — skipped by user")
						// Force Firefox to flush cookies to disk via page navigation
						visiblePage.Goto("about:blank", playwright.PageGotoOptions{
							Timeout: playwright.Float(3000),
						})
						time.Sleep(5 * time.Second)
						visibleCtx.Close()
						return
					default:
					}

					time.Sleep(1 * time.Second)
					captured.mu.Lock()
					hasFabric := captured.fabricToken != ""
					captured.mu.Unlock()
					if hasFabric {
						if globalSpin != nil {
							globalSpin.Stop("")
							globalSpin = nil
						}
						fmt.Println("  ✓ Token captured! Browser closes in 10s...")
						time.Sleep(10 * time.Second)
						break
					}
				}
			}

			visibleCtx.Close()
			return
		}
	}

	// For the normal flow where needsGraphToken is false
	time.Sleep(2 * time.Second)
	cookies, err := ctx.Cookies("https://teams.microsoft.com", "https://ic3.teams.office.com")
	if err == nil && len(cookies) > 0 {
		var parts []string
		for _, c := range cookies {
			if c.Name != "" && c.Value != "" {
				parts = append(parts, fmt.Sprintf("%s=%s", c.Name, c.Value))
			}
		}
		captured.mu.Lock()
		if captured.cookie == "" {
			captured.cookie = strings.Join(parts, "; ")
			notifyTokenCaptured("TEAMS_COOKIE")
		}
		captured.mu.Unlock()
	}

	if captured.fabricToken == "" {
		ctx.Close()
		manualFabricTokenCapture(pw, sessionDir, captured)
	}
}

func extractAudFromJWT(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	payload = strings.NewReplacer("-", "+", "_", "/").Replace(payload)
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return ""
	}
	switch v := claims["aud"].(type) {
	case string:
		return v
	case []interface{}:
		if len(v) > 0 {
			if s, ok := v[0].(string); ok {
				return s
			}
		}
	}
	return ""
}

// tryExtractGraphTokenViaGraphExplorer navigates to Graph Explorer and extracts
// the token with the correct scopes (Chat.ReadBasic, Files.Read, etc.).
func tryExtractGraphTokenViaGraphExplorer(page playwright.Page, captured *tokens, autoClick bool) {

	_, err := page.Goto("https://developer.microsoft.com/en-us/graph/graph-explorer",
		playwright.PageGotoOptions{Timeout: playwright.Float(20000)},
	)
	if err != nil {
		fmt.Println("  ⚠ Timeout loading Graph Explorer, continuing...")
	}

	time.Sleep(5 * time.Second)

	if autoClick {
		// Click "Run query" to trigger a request to graph.microsoft.com
		// The request interceptor captures it automatically
		selectors := []string{
			`button[aria-label="Run query"]`,
			`button:text("Run query")`,
			`[data-testid="run-query-button"]`,
		}

		for _, sel := range selectors {
			err := page.Click(sel, playwright.PageClickOptions{
				Timeout: playwright.Float(3000),
			})
			if err == nil {
				time.Sleep(3 * time.Second)
				break
			}
		}
	} else {
		fmt.Println("→ First run detected. Waiting for you to sign in to Graph Explorer.")
		fmt.Println("  Once signed in, click 'Run query' to capture the token.")
		fmt.Println("  The helper will detect it automatically.")
	}

	// Fallback: extract from Graph Explorer's localStorage
	scripts := []string{
		`(function() {
			for (let k of Object.keys(localStorage)) {
				if (k.includes('accesstoken') && k.includes('graph.microsoft.com')) {
					try {
						let v = JSON.parse(localStorage[k]);
						if (v.secret && v.secret.length > 100) return v.secret;
					} catch(e) {}
				}
			}
			return '';
		})()`,
	}

	for _, script := range scripts {
		result, err := page.Evaluate(script)
		if err != nil {
			continue
		}
		if token, ok := result.(string); ok && token != "" && len(token) > 100 {
			aud := extractAudFromJWT(token)
			if strings.Contains(aud, "graph.microsoft.com") {
				captured.mu.Lock()
				if captured.graphToken == "" {
					captured.graphToken = token
					notifyTokenCaptured("MS_GRAPH_TOKEN")
				}
				captured.mu.Unlock()

				// Return to Teams afterwards
				page.Goto("https://teams.microsoft.com/v2/",
					playwright.PageGotoOptions{Timeout: playwright.Float(15000)},
				)
				return
			}
		}
	}

	// Only redirect if token was already captured
	captured.mu.Lock()
	hasToken := captured.graphToken != ""
	captured.mu.Unlock()
	if hasToken {
		page.Goto("https://teams.microsoft.com/v2/",
			playwright.PageGotoOptions{Timeout: playwright.Float(15000)},
		)
	}
}

// tryExtractEduTokenFromJS attempts to extract the EDU_TOKEN from
// Teams' localStorage/sessionStorage (MSAL cache for Assignments).
func tryExtractEduTokenFromJS(page playwright.Page, captured *tokens) {
	scripts := []string{
		`(function() {
			for (let k of Object.keys(localStorage)) {
				if (k.includes('accesstoken') && k.includes('8f348934')) {
					try {
						let v = JSON.parse(localStorage[k]);
						if (v.secret) return v.secret;
					} catch(e) {}
				}
			}
			return '';
		})()`,
		`(function() {
			for (let k of Object.keys(sessionStorage)) {
				if (k.includes('accesstoken') && k.includes('8f348934')) {
					try {
						let v = JSON.parse(sessionStorage[k]);
						if (v.secret) return v.secret;
					} catch(e) {}
				}
			}
			return '';
		})()`,
	}

	for _, script := range scripts {
		result, err := page.Evaluate(script)
		if err != nil {
			continue
		}
		if token, ok := result.(string); ok && len(token) > 100 {
			aud := extractAudFromJWT(token)
			if strings.Contains(aud, "8f348934") {
				captured.mu.Lock()
				if captured.eduToken == "" {
					captured.eduToken = token
					notifyTokenCaptured("EDU_TOKEN")
				}
				captured.mu.Unlock()
				return
			}
		}
	}
}

// Step: try to extract FabricToken from browser storage

func manualFabricTokenCapture(pw *playwright.Playwright, sessionDir string, captured *tokens) {
	if globalSpin != nil {
		globalSpin.Stop("")
		globalSpin = nil
	}

	fmt.Println()
	printBox([]string{
		"Action required: TEAMS_FABRIC_TOKEN",
		"1. Go to any Private or Shared Channel.",
		"2. Go to the 'Members' tab.",
		"3. Try to change your own role", 
		"	(e.g., Owner -> Member)",
		"   (It will fail if you are the only owner, but",
		"   the token will be captured instantly!)",
		"Press [Enter] in this terminal to skip.",
	})
	fmt.Println()

	visibleCtx, err := pw.Firefox.LaunchPersistentContext(
		sessionDir,
		playwright.BrowserTypeLaunchPersistentContextOptions{
			Headless: playwright.Bool(false),
			SlowMo:   playwright.Float(0),
			Args:     []string{"--no-first-run"},
		},
	)
	if err != nil {
		fmt.Printf("  ⚠ Could not open browser for manual capture: %v\n", err)
		return
	}
	defer visibleCtx.Close()

	visibleCtx.On("request", func(req playwright.Request) {
		authHeader := ""
		for k, v := range req.Headers() {
			if strings.ToLower(k) == "authorization" {
				authHeader = v
				break
			}
		}
		if !strings.HasPrefix(authHeader, "Bearer ") {
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")

		if strings.Contains(req.URL(), "fabric/") {
			captured.mu.Lock()
			if captured.fabricToken == "" {
				captured.fabricToken = token
				notifyTokenCaptured("TEAMS_FABRIC_TOKEN")
				fmt.Println("\n  ✓ TEAMS_FABRIC_TOKEN manually captured!")
			}
			captured.mu.Unlock()
		}
	})

	pages := visibleCtx.Pages()
	var visiblePage playwright.Page
	if len(pages) > 0 {
		visiblePage = pages[0]
	} else {
		visiblePage, _ = visibleCtx.NewPage()
	}
	visiblePage.SetViewportSize(1280, 800)
	visiblePage.Goto("https://teams.microsoft.com/v2/", playwright.PageGotoOptions{Timeout: playwright.Float(20000)})

	// Channel for skip
	skipCh := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 1)
		if _, err := os.Stdin.Read(buf); err == nil {
			close(skipCh)
		}
	}()

	// Wait up to 2 minutes for the user to do the action
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-skipCh:
			fmt.Println("\n  · TEAMS_FABRIC_TOKEN — skipped by user")
			return
		default:
		}

		time.Sleep(1 * time.Second)
		captured.mu.Lock()
		hasFabric := captured.fabricToken != ""
		captured.mu.Unlock()

		if hasFabric {
			if globalSpin != nil {
				globalSpin.Stop("")
				globalSpin = nil
			}
			fmt.Println("  ✓ Token captured! Browser closes in 10s...")
			time.Sleep(10 * time.Second) // wait before closing browser
			return
		}
	}

	fmt.Println("\n  · TEAMS_FABRIC_TOKEN — timed out waiting for manual action")
}

func extractNameFromToken(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	payload = strings.NewReplacer("-", "+", "_", "/").Replace(payload)
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return ""
	}
	if name, ok := claims["name"].(string); ok && name != "" {
		parts := strings.Fields(name)
		if len(parts) > 0 {
			return strings.ToLower(parts[0])
		}
	}
	if given, ok := claims["given_name"].(string); ok && given != "" {
		return strings.ToLower(given)
	}
	return ""
}

// extractTenantIDFromJWT extracts the tenant ID ("tid") claim from a JWT.
func extractTenantIDFromJWT(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	payload = strings.NewReplacer("-", "+", "_", "/").Replace(payload)
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return ""
	}
	if tid, ok := claims["tid"].(string); ok {
		return tid
	}
	return ""
}

// extractOIDFromJWT extracts the object ID ("oid") claim from a JWT.
func extractOIDFromJWT(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	payload = strings.NewReplacer("-", "+", "_", "/").Replace(payload)
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return ""
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return ""
	}
	if oid, ok := claims["oid"].(string); ok {
		return oid
	}
	return ""
}

type spinner struct {
	frames []string
	stop   chan struct{}
	mu     sync.Mutex
	label  string
}

func newSpinner(label string) *spinner {
	return &spinner{
		frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		stop:   make(chan struct{}),
		label:  label,
	}
}

func (s *spinner) Start() {
	go func() {
		i := 0
		for {
			select {
			case <-s.stop:
				fmt.Printf("\r  %-50s\r", "") // clear line
				return
			default:
				s.mu.Lock()
				label := s.label
				s.mu.Unlock()
				fmt.Printf("\r  %s %s", s.frames[i%len(s.frames)], label)
				i++
				time.Sleep(80 * time.Millisecond)
			}
		}
	}()
}

func (s *spinner) SetLabel(label string) {
	s.mu.Lock()
	s.label = label
	s.mu.Unlock()
}

func (s *spinner) Stop(doneMsg string) {
	close(s.stop)
	time.Sleep(100 * time.Millisecond)
	if doneMsg != "" {
		fmt.Println(doneMsg)
	}
}

// patchWebSocketAsserts neutralizes the fatal asserts inside the Playwright
// Firefox websocket handlers. When a page navigates away while a websocket is
// still in flight, playwright's coreBundle.js throws an uncaught "Assertion
// error" that kills the Node driver process and makes the auth-helper crash on
// the next browser action. We replace those asserts with early returns.
func patchWebSocketAsserts() {
	// Driver lives at <cache>/ms-playwright-go/<version>/package/lib/coreBundle.js
	var base string
	switch runtime.GOOS {
	case "windows":
		base = filepath.Join(helpers.HomeDir(), "AppData", "Local", "ms-playwright-go")
	default:
		base = filepath.Join(helpers.HomeDir(), ".cache", "ms-playwright-go")
	}

	matches, err := filepath.Glob(filepath.Join(base, "*", "package", "lib", "coreBundle.js"))
	if err != nil || len(matches) == 0 {
		return
	}
	bundlePath := matches[len(matches)-1]

	data, err := os.ReadFile(bundlePath)
	if err != nil {
		return
	}
	orig := string(data)
	patched := strings.Replace(orig,
		"const request2 = this._webSocketRequests.get(event.requestId);\n        assert(request2);\n        const response2 = this._webSocketResponses.get(event.requestId);\n        assert(response2);",
		"const request2 = this._webSocketRequests.get(event.requestId);\n        const response2 = this._webSocketResponses.get(event.requestId);\n        if (!request2 || !response2)\n          return;",
		1,
	)
	if patched != orig {
		if err := os.WriteFile(bundlePath, []byte(patched), 0o644); err == nil {
			fmt.Println("  · Patched playwright websocket asserts.")
		}
	}
}
