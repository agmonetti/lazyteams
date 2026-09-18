package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mxschmitt/playwright-go"

	"github.com/agmonetti/lazyteams/internal/auth"
	"github.com/agmonetti/lazyteams/internal/helpers"
	"github.com/agmonetti/lazyteams/internal/version"
)

type tokens struct {
	mu               sync.Mutex
	graphToken       string
	webToken         string
	notifToken       string
	eduToken         string
	eduRequestSeen   bool
	cookie           string
	spacesToken      string
	fabricToken      string
	intentionalClose bool
	graphCandidate   *auth.GraphCandidate
	graphPersisted   bool
}

var globalSpin *spinner
var debugMode bool
var eventMode bool
var eventWriter io.Writer
var eventBatchID string
var eventStarted time.Time
var eventGeneration uint64
var eventTerminal bool
var eventWriteErr error
var graphInteractionEmitted bool
var eventStateMu sync.Mutex

var persistBatchTokenValues = auth.PersistTokenValues
var persistGraphCandidate = auth.PersistGraphCandidate
var runGraphLifecycleRunner = runGraphLifecycle
var runBatchGraphCapture = tryExtractGraphTokenViaGraphExplorer
var runBatchGraphOutcome = waitForGraphOutcome
var runBatchVisibleGraphOutcome = waitForVisibleGraphCandidate
var runBatchContextClose = func(ctx playwright.BrowserContext) error { return ctx.Close() }
var runBatchContextNewPage = func(ctx playwright.BrowserContext) (playwright.Page, error) { return ctx.NewPage() }
var runBatchRegisterInterceptor = registerFullInterceptor

var (
	errGraphInteractionRequired = errors.New("graph interaction required")
	errGraphTimeout             = errors.New("graph acquisition timeout")
	errGraphNetwork             = errors.New("graph network unavailable")
	errGraphCancelled           = errors.New("graph acquisition cancelled")
)

func persistBatchCredential(configDir string, values auth.TokenValues) error {
	set, err := persistBatchTokenValues(configDir, values)
	if err != nil {
		return err
	}
	eventStateMu.Lock()
	eventGeneration = set.Generation
	eventStateMu.Unlock()
	return nil
}

func currentEventWriteErr() error {
	eventStateMu.Lock()
	defer eventStateMu.Unlock()
	return eventWriteErr
}

func eventIsTerminal() bool {
	eventStateMu.Lock()
	defer eventStateMu.Unlock()
	return eventTerminal
}

func emitEvent(event, provider, state, errorCode string) error {
	eventStateMu.Lock()
	defer eventStateMu.Unlock()
	if !eventMode || eventWriter == nil || eventTerminal {
		return nil
	}
	err := auth.WriteAuthEvent(eventWriter, auth.AuthEvent{
		Version: auth.EventVersion, Event: event, BatchID: eventBatchID,
		Provider: provider, Generation: eventGeneration, State: state,
		DurationMS: time.Since(eventStarted).Milliseconds(), ErrorCode: errorCode,
	})
	if err != nil {
		eventWriteErr = err
	}
	if event == "completed" || event == "cancelled" {
		eventTerminal = true
	}
	return err
}

type contextRegistry struct {
	mu       sync.Mutex
	contexts map[playwright.BrowserContext]struct{}
	closing  bool
}

func (r *contextRegistry) add(ctx playwright.BrowserContext) {
	if ctx == nil {
		return
	}
	r.mu.Lock()
	if r.contexts == nil {
		r.contexts = make(map[playwright.BrowserContext]struct{})
	}
	r.contexts[ctx] = struct{}{}
	r.mu.Unlock()
	ctx.OnClose(func(playwright.BrowserContext) { r.remove(ctx) })
}

func (r *contextRegistry) remove(ctx playwright.BrowserContext) {
	r.mu.Lock()
	delete(r.contexts, ctx)
	r.mu.Unlock()
}

func (r *contextRegistry) closeAll() {
	r.mu.Lock()
	r.closing = true
	contexts := make([]playwright.BrowserContext, 0, len(r.contexts))
	for ctx := range r.contexts {
		contexts = append(contexts, ctx)
	}
	r.mu.Unlock()
	for _, ctx := range contexts {
		_ = ctx.Close()
	}
}

func (r *contextRegistry) isClosing() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closing
}

var activeContexts contextRegistry

// legacyWindowsConsole reports whether output goes to a legacy Windows console
// (cmd.exe/PowerShell conhost), where Unicode glyphs may render as '?'.
func legacyWindowsConsole() bool {
	return runtime.GOOS == "windows" && os.Getenv("WT_SESSION") == ""
}

// symOK is the check mark; ASCII "OK" on legacy Windows consoles.
var symOK = "✓"

func init() {
	if legacyWindowsConsole() {
		symOK = "OK"
	}
}

func debugPrintf(format string, args ...interface{}) {
	if debugMode {
		fmt.Printf(format, args...)
	}
}

// captureLoopResult describes what the token capture loop ended with.
const (
	captureOK     = "ok"     // all Teams tokens captured (or loop finished)
	captureStale  = "stale"  // nothing captured headless — session likely expired
	captureClosed = "closed" // browser was closed by the user
)

func printBox(lines []string) {
	width := 47
	fmt.Println("  ┌" + strings.Repeat("─", width) + "┐")
	for _, line := range lines {
		// Tabs render wider than a single rune, so expand them before
		// measuring to keep the box borders aligned.
		line = strings.ReplaceAll(line, "\t", "    ")
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
		fmt.Printf("\r\033[K  %s  %-45s\n", symOK, name)
		globalSpin.SetLabel("Waiting for next token...")
	} else {
		fmt.Printf("  %s  %-45s\n", symOK, name)
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

// markIntentionalClose flags the context close as deliberate (e.g. reopening
// visible for Graph Explorer / fabric capture) so the close handler does not
// clear the session.
func (t *tokens) markIntentionalClose() {
	t.mu.Lock()
	t.intentionalClose = true
	t.mu.Unlock()
}

// registerFullInterceptor captures cookies and all Bearer tokens from any
// context (headless or visible) so token capture keeps working after a
// context is reopened.
func registerFullInterceptor(ctx playwright.BrowserContext, captured *tokens) {
	ctx.On("request", func(req playwright.Request) {
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
		if strings.Contains(url, "assignments.edu.cloud.microsoft") {
			captured.mu.Lock()
			firstEduRequest := !captured.eduRequestSeen
			captured.eduRequestSeen = true
			captured.mu.Unlock()
			if firstEduRequest {
				debugPrintf("→ EDU request observed\n")
			}
		}

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
			// Graph candidates are accepted only from the dedicated Graph Explorer
			// listener, after provenance and permissions validation.
			_ = token

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
			fmt.Printf("  %s  %s\n", symOK, tk.name)
			captured++
		} else {
			fmt.Printf("  ·  %s\n", tk.name)
		}
	}
	fmt.Printf("\n  %d/7 captured\n", captured)
}

func main() {
	os.Exit(run())
}

var launchPersistentContext = func(pw *playwright.Playwright, sessionDir string, headless bool) (playwright.BrowserContext, error) {
	return pw.Firefox.LaunchPersistentContext(sessionDir, playwright.BrowserTypeLaunchPersistentContextOptions{
		Headless: playwright.Bool(headless),
		SlowMo:   playwright.Float(0),
		Args:     []string{"--no-first-run"},
	})
}

func openPersistentContext(pw *playwright.Playwright, sessionDir string, headless bool) (playwright.BrowserContext, error) {
	return launchPersistentContext(pw, sessionDir, headless)
}

// runSetupHook is a narrow seam for observing run's public setup path.
// It must not short-circuit run: production lifecycle and cleanup stay covered.
var runSetupHook = func() {}

// runOpenPersistentContext keeps the public run path injectable in deterministic
// tests without duplicating setup or invoking Playwright manually.
var runOpenPersistentContext = openPersistentContext
var runBatchJobsStateRunner = runBatchJobsState
var runPlaywrightInstall = playwright.Install
var runPlaywrightRun = playwright.Run
var runPatchWebSocketAsserts = patchWebSocketAsserts
var runStopPlaywright = func(pw *playwright.Playwright) {
	if pw != nil {
		pw.Stop()
	}
}
var runContextPages = func(ctx playwright.BrowserContext) []playwright.Page { return ctx.Pages() }
var runContextNewPage = func(ctx playwright.BrowserContext) (playwright.Page, error) { return ctx.NewPage() }
var runPageSetViewport = func(page playwright.Page, width, height int) error {
	return page.SetViewportSize(width, height)
}
var runContextOnClose = func(ctx playwright.BrowserContext, fn func()) {
	ctx.On("close", func(playwright.BrowserContext) { fn() })
}
var runRegisterInterceptor = registerFullInterceptor
var runCloseContext = func(ctx playwright.BrowserContext) error {
	if ctx == nil {
		return nil
	}
	return ctx.Close()
}

func openRunPersistentContext(pw *playwright.Playwright, sessionDir string, headless bool) (playwright.BrowserContext, error) {
	return runOpenPersistentContext(pw, sessionDir, headless)
}

func run() int {
	args := os.Args[1:]
	runSetupHook()
	for _, arg := range args {
		if arg == "--events" {
			eventMode = true
		}
	}
	var interrupted atomic.Bool
	var sigCh chan os.Signal
	if eventMode {
		if eventWriter == nil {
			eventWriter = os.Stdout
		}
		os.Stdout = os.Stderr
		eventBatchID = fmt.Sprintf("%x-%x", time.Now().UnixNano(), os.Getpid())
		eventStarted = time.Now()
		eventStateMu.Lock()
		eventTerminal = false
		eventWriteErr = nil
		graphInteractionEmitted = false
		eventStateMu.Unlock()
		// Load the baseline before started: its generation is part of the
		// stream contract and must not be guessed as zero.
		if current, err := auth.LoadCredentialSet(); err == nil {
			eventStateMu.Lock()
			eventGeneration = current.Generation
			eventStateMu.Unlock()
		}
	}
	// Install the handler before emitting started. A cancellation arriving
	// immediately after startup must still produce the protocol terminal event.
	sigCh = make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		<-sigCh
		interrupted.Store(true)
		if eventMode && !eventIsTerminal() {
			_ = emitEvent("cancelled", "batch", "cancelled", "cancelled")
		}
		activeContexts.closeAll()
	}()
	if eventMode {
		emitEvent("started", "batch", "running", "")
	}
	batchJobsArg := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--renew-batch" && i+1 < len(args) {
			batchJobsArg = args[i+1]
		}
	}
	if eventMode {
		// Every event-mode batch has exactly one terminal event, including
		// failures while setting up the browser or profile lock.
		defer func() {
			if batchJobsArg != "" && !eventIsTerminal() {
				emitEvent("completed", "batch", "partial_failure", "acquisition_failed")
			}
		}()

		if batchJobsArg == "" {
			emitEvent("completed", "batch", "partial_failure", "protocol_error")
			return 1
		}
		if _, err := normalizeBatchJobs(batchJobsArg); err != nil {
			emitEvent("completed", "batch", "partial_failure", "protocol_error")
			return 1
		}
	}
	for _, arg := range args {
		if arg == "--version" || arg == "-v" {
			fmt.Println(version.Version)
			return 0
		}
		if arg == "--help" || arg == "-h" {
			fmt.Print(`lazyteams-auth — Microsoft Teams authentication helper

USAGE:
  ./lazyteams-auth                         Full token capture
  ./lazyteams-auth --renew <token>         Renew one token
  ./lazyteams-auth --renew edu --show      Renew EDU with visible browser
  ./lazyteams-auth --debug                 Enable detailed diagnostic output
  ./lazyteams-auth --version               Show the build version
  ./lazyteams-auth --help                  Show this help

OPTIONS:
  --show             Force browser visible
  --headless         Force headless mode
  --clear-session    Delete browser session
  --clear-tokens     Delete saved tokens
`)
			return 0
		}
	}
	for _, arg := range args {
		if arg == "--debug" {
			debugMode = true
		}
	}

	initConsole()
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════╗")
	fmt.Println("║          lazyteams — Auth Helper           ║")
	fmt.Println("╚════════════════════════════════════════════╝")
	fmt.Println()

	configDir := helpers.ConfigDir()
	sessionDir := filepath.Join(configDir, "browser-session")
	if interrupted.Load() {
		return 1
	}
	if err := os.MkdirAll(sessionDir, 0700); err != nil {
		fmt.Printf("Error creating session directory: %v\n", err)
		return 1
	}
	profileLock, err := helpers.AcquireProfileLock(configDir)
	if err != nil {
		if errors.Is(err, helpers.ErrProfileInUse) {
			fmt.Println("profile_in_use: another authentication helper owns the browser session")
		} else {
			fmt.Printf("Error acquiring profile ownership: %v\n", err)
		}
		return 1
	}
	defer profileLock.Release()
	if interrupted.Load() {
		return 1
	}
	// Parse --renew flag
	renewOnly := ""
	showBrowser := false
	forceHeadless := false
	clearSession := false
	clearTokens := false
	batchJobs := batchJobsArg
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
		// --renew-batch was parsed before acquiring the profile lock so invalid
		// protocol requests fail without opening Playwright.
	}

	if clearSession {
		fmt.Println("→ Clearing browser session...")
		if err := os.RemoveAll(sessionDir); err != nil {
			fmt.Printf("  ⚠ Error clearing session: %v\n", err)
		} else {
			fmt.Println("  " + symOK + "  Browser session cleared.")
			fmt.Println("  Next run will require login.")
		}
		if !clearTokens {
			return 0
		}
	}

	if clearTokens {
		fmt.Println("→ Clearing saved tokens...")
		tokensPath := filepath.Join(configDir, "tokens.env")
		if err := os.Remove(tokensPath); err != nil && !os.IsNotExist(err) {
			fmt.Printf("  ⚠ Error clearing tokens: %v\n", err)
		} else {
			fmt.Println("  " + symOK + "  Tokens cleared.")
		}
		return 0
	}

	firstRun := !sessionExists(sessionDir)
	if forceHeadless {
		showBrowser = false
	}
	graphBatch := false
	if batchJobs != "" {
		if jobs, err := normalizeBatchJobs(batchJobs); err == nil {
			for _, job := range jobs {
				graphBatch = graphBatch || job == "graph"
			}
		}
	}
	if firstRun && !graphBatch {
		showBrowser = true
		fmt.Println("  ● First run — browser will open for login")
	} else if showBrowser {
		fmt.Println("  ● Session found — browser will open")
	} else {
		fmt.Println("  ● Session found — running headless")
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

	if interrupted.Load() {
		return 1
	}
	if !browserExists {
		fmt.Println("→ Downloading browser (one-time only, ~80MB)...")
		if err := runPlaywrightInstall(&playwright.RunOptions{
			Browsers: []string{"firefox"},
		}); err != nil {
			fmt.Printf("Error installing browser: %v\n", err)
			return 1
		}
	} else {
		// Install silently without output
		runPlaywrightInstall(&playwright.RunOptions{
			Browsers: []string{"firefox"},
			Verbose:  false,
		})
	}

	runPatchWebSocketAsserts()
	if interrupted.Load() {
		return 1
	}

	pw, err := runPlaywrightRun()
	if err != nil {
		fmt.Printf("Error starting Playwright: %v\n", err)
		return 1
	}
	defer runStopPlaywright(pw)
	if interrupted.Load() {
		return 1
	}

	context, err := openRunPersistentContext(pw, sessionDir, !showBrowser)
	if err != nil {
		fmt.Printf("Error launching browser: %v\n", err)
		return 1
	}
	if context != nil {
		activeContexts.add(context)
	}
	defer func() { _ = runCloseContext(context) }()
	if interrupted.Load() {
		return 1
	}

	captured := &tokens{}

	// Signal that the browser was closed unexpectedly
	browserClosed := make(chan struct{}, 1)

	// Detect unexpected browser close and clear session if capture incomplete
	// An intentional close (e.g. reopening visible for Graph Explorer) must not
	// be treated as an unexpected browser close.
	runContextOnClose(context, func() {
		captured.mu.Lock()
		intentional := captured.intentionalClose
		incomplete := captured.graphToken == "" || captured.webToken == "" || captured.cookie == ""
		captured.mu.Unlock()
		if renewOnly == "" && batchJobs == "" && !intentional && incomplete && !activeContexts.isClosing() {
			fmt.Println("\n\n[!] Browser closed before login completed. Clearing session...")
			os.RemoveAll(sessionDir)
			fmt.Println("  " + symOK + "  Session cleared. Next run will require login.")
		}
		select {
		case browserClosed <- struct{}{}:
		default:
		}
	})

	// The early signal controller above owns ordered browser shutdown.
	// Returning through main preserves context, Playwright and lock defers.
	runRegisterInterceptor(context, captured)

	// Use the page that the persistent context already opened
	pages := runContextPages(context)
	var page playwright.Page
	if len(pages) > 0 {
		page = pages[0]
	} else {
		page, err = runContextNewPage(context)
		if err != nil {
			fmt.Printf("Error creating page: %v\n", err)
			return 1
		}
	}

	if interrupted.Load() {
		return 1
	}
	if err := runPageSetViewport(page, 1280, 800); err != nil {
		fmt.Printf("Error setting viewport: %v\n", err)
	}

	var renewalErr error
	var batchErr error
	switch {
	case batchJobs != "":
		page, context, batchErr = runBatchJobsStateRunner(pw, sessionDir, batchJobs, page, context, captured, configDir)
	case renewOnly == "web" || renewOnly == "notif":
		fmt.Println("→ Renewing WEB + NOTIF tokens...")
		renewWebNotif(page, context, captured)

	case renewOnly == "edu":
		fmt.Println("→ Renewing EDU token...")
		renewalErr = renewEdu(page, context, captured, showBrowser)

	case renewOnly == "fabric":
		fmt.Println("→ Renewing FABRIC token...")
		captured.markIntentionalClose()
		context.Close()
		manualFabricTokenCapture(pw, sessionDir, captured)

	case renewOnly == "graph":
		fmt.Println("→ Renewing GRAPH token...")
		page, context, batchErr = dispatchRenewal("graph", func() (playwright.Page, playwright.BrowserContext, error) {
			err := graphRenewalRunner(func() error {
				var err error
				page, context, err = runBatchJobsStateRunner(pw, sessionDir, "graph", page, context, captured, configDir)
				return err
			})
			return page, context, err
		}, func() (playwright.Page, playwright.BrowserContext, error) { return page, context, nil })
		if batchErr != nil {
			fmt.Printf("  ⚠ Graph renewal failed: %v\n", batchErr)
		}

	default:
		fmt.Println("→ Full token capture...")
		if firstRun {
			fmt.Println("→ Sign in. After logging in, Teams will load")
			fmt.Println("  and tokens will be captured automatically.")
		} else {
			fmt.Println("→ Teams will load automatically.")
		}
		page, context, batchErr = dispatchRenewal(renewOnly, func() (playwright.Page, playwright.BrowserContext, error) {
			return page, context, nil
		}, func() (playwright.Page, playwright.BrowserContext, error) {
			return fullRenewalRunner(pw, page, context, captured, sessionDir, firstRun, browserClosed, configDir)
		})
	}

	if eventMode && batchJobs != "" {
		return finishBatch(batchErr, interrupted.Load())
	}

	if interrupted.Load() {
		fmt.Println("\n  · Capture cancelled; preserving existing credentials.")
		return 1
	}
	if batchErr != nil {
		fmt.Printf("Renewal failed: %v\n", batchErr)
		return 1
	}

	fmt.Println()
	if captured.allCaptured() {
		fmt.Println(symOK + " All tokens captured.")
	} else {
		fmt.Println("⚠ Partial tokens — saving what's available.")
	}
	printTokenStatus(captured)

	if err := saveTokens(captured, configDir); err != nil {
		fmt.Printf("Error saving tokens: %v\n", err)
		return 1
	}
	if renewalErr != nil {
		fmt.Printf("Renewal failed: %v\n", renewalErr)
		if showBrowser {
			waitBeforeClosingBrowser()
		}
		_ = runCloseContext(context)
		runStopPlaywright(pw)
		return 1
	}

	fmt.Println()
	fmt.Println("→ Session saved. Next time no login is required.")
	fmt.Println("→ You can now run: ./lazyteams")
	return 0
}

func sessionExists(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

func normalizeBatchJobs(raw string) ([]string, error) {
	allowed := map[string]bool{"graph": true, "web_notif": true, "edu": true}
	seen := map[string]bool{}
	var jobs []string
	for _, rawJob := range strings.Split(raw, ",") {
		job := strings.TrimSpace(rawJob)
		if job == "web" || job == "notif" {
			job = "web_notif"
		}
		if !allowed[job] {
			return nil, fmt.Errorf("unsupported batch job")
		}
		if !seen[job] {
			seen[job] = true
			jobs = append(jobs, job)
		}
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("empty batch")
	}
	return jobs, nil
}

func batchProviders(job string) []string {
	switch job {
	case "web_notif":
		return []string{"web", "notif"}
	default:
		return []string{job}
	}
}

func batchJobCaptured(job string, before, after auth.TokenValues) bool {
	return len(batchJobMissingProviders(job, before, after)) == 0
}

func batchJobMissingProviders(job string, before, after auth.TokenValues) []string {
	changed := func(old, current string) bool { return current != "" && current != old }
	var missing []string
	check := func(provider, old, current string) {
		if !changed(old, current) {
			missing = append(missing, provider)
		}
	}
	switch job {
	case "graph":
		check("graph", before.Graph, after.Graph)
	case "web_notif":
		check("web", before.Web, after.Web)
		check("notif", before.Notif, after.Notif)
	case "edu":
		check("edu", before.EDU, after.EDU)
	default:
		return []string{"batch"}
	}
	return missing
}

func persistBatchCheckpoint(configDir, job string, values auth.TokenValues, last map[string]string) (bool, error) {
	return persistBatchCheckpointWithCandidate(configDir, job, values, last, nil)
}

func persistBatchCheckpointWithCandidate(configDir, job string, values auth.TokenValues, last map[string]string, candidate *auth.GraphCandidate) (bool, error) {
	all := map[string]string{"graph": values.Graph, "web": values.Web, "notif": values.Notif, "edu": values.EDU, "cookie": values.Cookie, "spaces": values.Spaces}
	allowed := map[string]bool{}
	switch job {
	case "graph":
		allowed["graph"] = true
	case "web_notif":
		for _, provider := range []string{"web", "notif", "cookie", "spaces"} {
			allowed[provider] = true
		}
	case "edu":
		allowed["edu"] = true
	default:
		return false, fmt.Errorf("unsupported batch job %q", job)
	}
	changed := make(map[string]string)
	for provider, token := range all {
		if allowed[provider] && token != "" && token != last[provider] {
			changed[provider] = token
		}
	}
	if len(changed) == 0 {
		return false, nil
	}
	if job == "graph" && candidate != nil && changed["graph"] != "" {
		if _, err := auth.PersistGraphCandidate(configDir, *candidate); err != nil {
			return false, err
		}
	} else {
		delta := auth.TokenValues{Graph: changed["graph"], Web: changed["web"], Notif: changed["notif"], EDU: changed["edu"], Cookie: changed["cookie"], Spaces: changed["spaces"]}
		if err := persistBatchCredential(configDir, delta); err != nil {
			return false, err
		}
	}
	for provider, token := range changed {
		last[provider] = token
		if err := emitEvent("credential_ready", provider, "ready", ""); err != nil {
			return false, err
		}
	}
	return true, nil
}

// transitionGraphContext closes the owned context before opening its visible
// replacement; the profile must never have two contexts at once.
func transitionGraphContext(closeContext func() error, openVisible func() error) error {
	if err := closeContext(); err != nil {
		return err
	}
	return openVisible()
}

// runGraphLifecycle owns the headless→visible transition without depending on Playwright.
// The callbacks keep the production browser code small and make the contract testable.
func runGraphLifecycle(timeout time.Duration, headless func() (string, error), visible func() (string, error), persist func() error, interaction func() error, closing func() bool) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if closing() {
			return errGraphCancelled
		}
		state, err := headless()
		if err != nil {
			return err
		}
		if state == "interaction_required" {
			if err := interaction(); err != nil {
				return err
			}
			if closing() {
				return errGraphCancelled
			}
			state, err = visible()
			if err != nil {
				return err
			}
		}
		if state == "cancelled" {
			return errGraphCancelled
		}
		if state == "interaction_required" {
			// The visible flow already had its chance to complete. Do not
			// reopen the same profile or retry the same candidate forever.
			return errGraphInteractionRequired
		}
		if state == "ready" {
			if err := persist(); err != nil {
				// An invalid candidate is not an interaction request: discard it
				// and keep listening for a later valid Graph request.
				lastErr = err
				continue
			}
			return nil
		}
		if state == "timeout" {
			break
		}
		lastErr = errGraphNetwork
	}
	if lastErr != nil {
		return lastErr
	}
	return errGraphTimeout
}

func runBatchJobsState(pw *playwright.Playwright, sessionDir, raw string, page playwright.Page, ctx playwright.BrowserContext, captured *tokens, configDir string) (playwright.Page, playwright.BrowserContext, error) {
	captureValues := func() auth.TokenValues {
		captured.mu.Lock()
		defer captured.mu.Unlock()
		return auth.TokenValues{Graph: captured.graphToken, Web: captured.webToken, Notif: captured.notifToken, EDU: captured.eduToken, Cookie: captured.cookie, Spaces: captured.spacesToken, Fabric: captured.fabricToken}
	}
	err := runBatchJobSequence(raw, captureValues, func(job string) error {
		switch job {
		case "graph":
			err := runGraphLifecycleRunner(2*time.Minute,
				func() (string, error) {
					if err := runBatchGraphCapture(page, captured, true); err != nil {
						return "", err
					}
					return runBatchGraphOutcome(page, captured, 2*time.Minute), nil
				},
				func() (string, error) {
					captured.markIntentionalClose()
					var visible playwright.BrowserContext
					var err error
					err = transitionGraphContext(func() error { return runBatchContextClose(ctx) }, func() error {
						visible, err = openRunPersistentContext(pw, sessionDir, false)
						if err != nil {
							return errGraphInteractionRequired
						}
						ctx = visible
						activeContexts.add(ctx)
						page, err = runBatchContextNewPage(ctx)
						if err != nil {
							return errGraphInteractionRequired
						}
						runBatchRegisterInterceptor(ctx, captured)
						if err := runBatchGraphCapture(page, captured, false); err != nil {
							return err
						}
						return nil
					})
					if err != nil {
						return "", err
					}
					return runBatchVisibleGraphOutcome(page, captured, 2*time.Minute), nil
				},
				func() error {
					captured.mu.Lock()
					candidate := captured.graphCandidate
					captured.graphCandidate = nil
					captured.mu.Unlock()
					if candidate == nil {
						return errGraphTimeout
					}
					set, err := persistGraphCandidate(configDir, *candidate)
					if err != nil {
						if errors.Is(err, auth.ErrIncompatibleCredential) {
							captured.mu.Lock()
							captured.graphCandidate = nil
							captured.mu.Unlock()
						}
						return err
					}
					captured.mu.Lock()
					captured.graphToken = candidate.Token
					captured.graphPersisted = true
					captured.mu.Unlock()
					eventStateMu.Lock()
					eventGeneration = set.Generation
					eventStateMu.Unlock()
					return nil
				},
				func() error {
					eventStateMu.Lock()
					graphInteractionEmitted = true
					eventStateMu.Unlock()
					return emitEvent("interaction_required", "graph", "interaction_required", "interaction_required")
				},
				activeContexts.isClosing,
			)
			if err != nil {
				return err
			}
		case "web_notif":
			renewWebNotif(page, ctx, captured)
		case "edu":
			return renewEdu(page, ctx, captured, false)
		}
		return nil
	}, func(job string, values auth.TokenValues, last map[string]string) (bool, error) {
		if job == "graph" {
			captured.mu.Lock()
			persisted := captured.graphPersisted
			captured.mu.Unlock()
			if persisted {
				last["graph"] = values.Graph
				if err := emitEvent("credential_ready", "graph", "ready", ""); err != nil {
					return false, err
				}
				return true, nil
			}
		}
		return persistBatchCheckpointWithCandidate(configDir, job, values, last, nil)
	}, func() string { return page.URL() })
	return page, ctx, err
}

// runBatchJobSequence is the real batch control loop with browser work and
// checkpoint persistence injected. Tests use the same loop with deterministic
// runners, so terminal/error ordering is covered without Playwright.
func runBatchJobSequence(raw string, capture func() auth.TokenValues, runJob func(string) error, checkpoint func(string, auth.TokenValues, map[string]string) (bool, error), pageURL func() string) error {
	jobs, err := normalizeBatchJobs(raw)
	if err != nil {
		return err
	}
	requested := make(map[string]bool, len(jobs))
	for _, job := range jobs {
		requested[job] = true
	}
	last := map[string]string{}
	var firstErr error
	for _, job := range []string{"graph", "web_notif", "edu"} {
		if !requested[job] {
			continue
		}
		previous := capture()
		if err := runJob(job); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			for _, provider := range batchProviders(job) {
				if job == "graph" && errors.Is(err, errGraphInteractionRequired) {
					eventStateMu.Lock()
					emitted := graphInteractionEmitted
					eventStateMu.Unlock()
					if !emitted {
						_ = emitEvent("interaction_required", provider, "interaction_required", "interaction_required")
					}
				} else {
					_ = emitEvent("provider_failed", provider, "unknown", batchErrorCode(err))
				}
			}
			continue
		}
		changed, err := checkpoint(job, capture(), last)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			errCode := "persistence_failed"
			if strings.Contains(err.Error(), "stale credential generation") {
				errCode = "stale_generation"
			} else if strings.Contains(err.Error(), "incompatible") || strings.Contains(err.Error(), "bootstrap permissions") || strings.Contains(err.Error(), "official HTTPS") || strings.Contains(err.Error(), "Graph Explorer") {
				errCode = "incompatible_credential"
			}
			for _, provider := range batchProviders(job) {
				emitEvent("provider_failed", provider, "unknown", errCode)
			}
			continue
		}
		missing := batchJobMissingProviders(job, previous, capture())
		if !changed || len(missing) > 0 {
			if firstErr == nil {
				firstErr = fmt.Errorf("batch job %s did not capture all requested credentials", job)
			}
			for _, provider := range missing {
				if job == "graph" && isLoginURL(pageURL()) {
					emitEvent("interaction_required", provider, "interaction_required", "interaction_required")
				} else {
					emitEvent("provider_failed", provider, "unknown", "acquisition_failed")
				}
			}
		}
	}
	return firstErr
}

func batchErrorCode(err error) string {
	switch {
	case errors.Is(err, errGraphTimeout):
		return "timeout"
	case errors.Is(err, errGraphNetwork):
		return "network_unavailable"
	case errors.Is(err, errGraphCancelled):
		return "cancelled"
	case errors.Is(err, auth.ErrStaleGeneration):
		return "stale_generation"
	case errors.Is(err, auth.ErrIncompatibleCredential):
		return "incompatible_credential"
	default:
		return "acquisition_failed"
	}
}

func finishBatch(batchErr error, interrupted bool) int {
	if eventIsTerminal() {
		return 1
	}
	if interrupted {
		if err := emitEvent("cancelled", "batch", "cancelled", "cancelled"); err != nil {
			return 1
		}
		return 1
	}
	if batchErr != nil {
		emitEvent("completed", "batch", "partial_failure", batchErrorCode(batchErr))
		return 1
	}
	if currentEventWriteErr() != nil {
		emitEvent("completed", "batch", "partial_failure", "protocol_error")
		return 1
	}
	if err := emitEvent("completed", "batch", "completed", ""); err != nil || currentEventWriteErr() != nil {
		return 1
	}
	return 0
}

func saveTokens(t *tokens, configDir string) error {
	t.mu.Lock()
	values := auth.TokenValues{
		Graph: t.graphToken, Web: t.webToken, Notif: t.notifToken, EDU: t.eduToken,
		Cookie: t.cookie, Spaces: t.spacesToken, Fabric: t.fabricToken,
	}
	// A Graph Explorer candidate is already persisted by the shared Graph path;
	// do not rewrite it through the legacy browser metadata path.
	if t.graphPersisted {
		values.Graph = ""
	}
	t.mu.Unlock()

	if _, err := auth.PersistTokenValues(configDir, values); err != nil {
		return err
	}
	fmt.Printf("→ Tokens saved to: %s\n", filepath.Join(configDir, "tokens.env"))
	return nil
}

// renewWebNotif navigates Teams to trigger WEB + NOTIF token requests.
func renewWebNotif(page playwright.Page, ctx playwright.BrowserContext, captured *tokens) {
	page.Goto("https://teams.microsoft.com", playwright.PageGotoOptions{
		Timeout:   playwright.Float(60000),
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	})

	// Navigate to Teams section to trigger SPACES_TOKEN
	page.Goto("https://teams.microsoft.com/v2/#/teams/", playwright.PageGotoOptions{
		Timeout: playwright.Float(15000),
	})

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
func renewEdu(page playwright.Page, ctx playwright.BrowserContext, captured *tokens, showBrowser bool) error {
	_, err := page.Goto("https://teams.microsoft.com/v2/",
		playwright.PageGotoOptions{Timeout: playwright.Float(60000)},
	)
	if err != nil {
		return fmt.Errorf("navigate to Teams: %w", err)
	}

	if isLoginURL(page.URL()) {
		if !showBrowser {
			return fmt.Errorf("interactive login required; run ./lazyteams-auth --renew edu --show")
		}
		fmt.Println("→ Waiting for interactive login and 2FA...")
		page, err = waitForTeamsPage(ctx, 5*time.Minute)
		if err != nil {
			return err
		}
	}
	waitForTeamsShell(page, 30*time.Second)
	logPages("EDU pages before click", ctx.Pages())
	logTeamsDOM(page)

	selectors := []string{
		`[role="navigation"] span:text-is("Assignments")`,
		`[data-tid="app-bar-edu-assignments"]`,
		`span:text-is("Assignments") >> nth=0`,
	}

	clicked, clickErrors := clickAssignments(page, selectors, 30*time.Second)

	assignmentsPageFound := false
	for _, candidate := range ctx.Pages() {
		if strings.Contains(candidate.URL(), "assignments.edu.cloud.microsoft") {
			assignmentsPageFound = true
			break
		}
	}
	logPages("EDU pages after click", ctx.Pages())

	captured.mu.Lock()
	hasEdu := captured.eduToken != ""
	captured.mu.Unlock()
	if hasEdu {
		return nil
	}
	if !clicked && !assignmentsPageFound {
		logTeamsDOM(page)
		return fmt.Errorf("could not click Assignments: %s", strings.Join(clickErrors, "; "))
	}

	// Try JS extraction on every page because Teams may open Assignments in a new tab.
	deadline := time.Now().Add(10 * time.Second)
	jsErrorLogged := false
	for time.Now().Before(deadline) {
		for _, candidate := range ctx.Pages() {
			if err := tryExtractEduTokenFromJS(candidate, captured); err != nil && !jsErrorLogged {
				debugPrintf("  ⚠ EDU JS extraction failed on %s: %v\n", candidate.URL(), err)
				jsErrorLogged = true
			}
		}
		captured.mu.Lock()
		hasEdu = captured.eduToken != ""
		captured.mu.Unlock()
		if hasEdu {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	captured.mu.Lock()
	requestSeen := captured.eduRequestSeen
	captured.mu.Unlock()
	return fmt.Errorf("EDU token was not captured after Assignments click (request observed: %t, assignments URL found: %t)", requestSeen, assignmentsPageFound)
}

func isLoginURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && parsed.Hostname() == "login.microsoftonline.com"
}

func isTeamsURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && parsed.Hostname() == "teams.microsoft.com"
}

func waitForTeamsPage(ctx playwright.BrowserContext, timeout time.Duration) (playwright.Page, error) {
	deadline := time.Now().Add(timeout)
	lastURL := ""
	for time.Now().Before(deadline) {
		for _, candidate := range ctx.Pages() {
			candidateURL := candidate.URL()
			if candidateURL != lastURL {
				debugPrintf("  login page URL: %s\n", candidateURL)
				lastURL = candidateURL
			}
			if isTeamsURL(candidateURL) {
				debugPrintf("  %s  Login completed: %s\n", symOK, candidateURL)
				return candidate, nil
			}
		}
		time.Sleep(1 * time.Second)
	}
	return nil, fmt.Errorf("interactive login did not reach Teams within %s", timeout)
}

func logPages(label string, pages []playwright.Page) {
	if !debugMode {
		return
	}
	debugPrintf("→ %s (%d pages)\n", label, len(pages))
	for i, page := range pages {
		debugPrintf("  page[%d]: %s\n", i, page.URL())
	}
}

func waitForTeamsShell(page playwright.Page, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		navigationCount, _ := page.Locator(`[role="navigation"]`).Count()
		if navigationCount > 0 {
			return
		}
		time.Sleep(1 * time.Second)
	}
	debugPrintf("  ⚠ Teams shell did not expose navigation within %s\n", timeout)
}

func clickAssignments(page playwright.Page, selectors []string, timeout time.Duration) (bool, []string) {
	deadline := time.Now().Add(timeout)
	var clickErrors []string
	for time.Now().Before(deadline) {
		for _, sel := range selectors {
			err := page.Locator(sel).First().Click(playwright.LocatorClickOptions{
				Timeout: playwright.Float(1500),
			})
			if err == nil {
				debugPrintf("→ Clicked Assignments sidebar (%s)\n", sel)
				return true, clickErrors
			}
			if len(clickErrors) < 12 {
				clickErrors = append(clickErrors, fmt.Sprintf("%s: %v", sel, err))
			}
		}
		time.Sleep(1 * time.Second)
	}
	return false, clickErrors
}

func logTeamsDOM(page playwright.Page) {
	snapshot, err := page.Locator("body").AriaSnapshot()
	if err != nil {
		debugPrintf("  ⚠ Could not capture Teams accessibility tree: %v\n", err)
		return
	}
	const maxSnapshotLength = 12000
	if len(snapshot) > maxSnapshotLength {
		snapshot = snapshot[:maxSnapshotLength] + "..."
	}
	debugPrintf("→ Teams accessibility tree:\n%s\n", snapshot)
}

func waitBeforeClosingBrowser() {
	fmt.Println("Press Enter to close the browser...")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func graphCaptureNeeded(captured *tokens) bool {
	captured.mu.Lock()
	defer captured.mu.Unlock()
	return captured.graphToken == ""
}

// fullRenewal runs the full capture flow (all 5 tokens).
func runGraphRenewal(batch func() error) error {
	return batch()
}

// These seams keep the CLI dispatch testable without starting Playwright.
var (
	graphRenewalRunner = runGraphRenewal
	fullRenewalRunner  = fullRenewal
)

func dispatchRenewal(mode string, graph, full func() (playwright.Page, playwright.BrowserContext, error)) (playwright.Page, playwright.BrowserContext, error) {
	if mode == "graph" {
		return graph()
	}
	return full()
}

func runFullRenewalFlow(graph, postGraph func() error) error {
	if err := graph(); err != nil {
		return err
	}
	return postGraph()
}

func renewGraphIfNeeded(captured *tokens, acquire func() error) error {
	if !graphCaptureNeeded(captured) {
		return nil
	}
	if err := acquire(); err != nil {
		return err
	}
	captured.mu.Lock()
	persisted := captured.graphPersisted
	captured.mu.Unlock()
	if !persisted {
		return errGraphTimeout
	}
	return nil
}

var fullRenewalGoto = func(page playwright.Page) error {
	_, err := page.Goto("https://teams.microsoft.com", playwright.PageGotoOptions{Timeout: playwright.Float(90000)})
	return err
}

var fullRenewalCapture = captureLoop
var fullRenewalSleep = time.Sleep

var fullRenewalGraph = func(pw *playwright.Playwright, page playwright.Page, ctx playwright.BrowserContext, captured *tokens, sessionDir, configDir string) (playwright.Page, playwright.BrowserContext, error) {
	err := renewGraphIfNeeded(captured, func() error {
		var err error
		page, ctx, err = runBatchJobsStateRunner(pw, sessionDir, "graph", page, ctx, captured, configDir)
		return err
	})
	return page, ctx, err
}

var fullRenewalPostGraph = func(pw *playwright.Playwright, page playwright.Page, ctx playwright.BrowserContext, captured *tokens, sessionDir string) error {
	fullRenewalSleep(2 * time.Second)
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
		captured.markIntentionalClose()
		if err := ctx.Close(); err != nil {
			return err
		}
		manualFabricTokenCapture(pw, sessionDir, captured)
	}
	return nil
}

func fullRenewal(pw *playwright.Playwright, page playwright.Page, ctx playwright.BrowserContext, captured *tokens, sessionDir string, firstRun bool, browserClosed <-chan struct{}, configDir string) (playwright.Page, playwright.BrowserContext, error) {
	globalSpin = newSpinner("Connecting to Teams...")
	globalSpin.Start()
	defer func() {
		if globalSpin != nil {
			globalSpin.Stop("")
			globalSpin = nil
		}
	}()

	// Open the base URL — no networkidle wait, let the user interact with login
	err := fullRenewalGoto(page)
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
			// Check if browser was closed
			select {
			case <-browserClosed:
				fmt.Println("\n  · Browser closed — stopping capture.")
				return page, ctx, nil
			default:
			}

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
		fullRenewalSleep(5 * time.Second)
	}

	// Now navigate to /v2/#/teams/ to trigger tokens
	if globalSpin != nil {
		globalSpin.SetLabel("Navigating to Teams to capture tokens...")
	}

	captureResult := fullRenewalCapture(page, ctx, pw, sessionDir, captured, browserClosed)
	if captureResult == captureClosed {
		return page, ctx, nil
	}

	// TEAMS_NOTIF_TOKEN = TEAMS_WEB_TOKEN
	captured.mu.Lock()
	if captured.notifToken == "" && captured.webToken != "" {
		captured.notifToken = captured.webToken
		notifyTokenCaptured("TEAMS_NOTIF_TOKEN")
	}
	captured.mu.Unlock()

	// Graph renewal owns its own headless→visible fallback; keep the original
	// context alive so the shared operation always receives a live page/context.

	// Plan B: Graph Explorer for MS_GRAPH_TOKEN
	page, ctx, err = fullRenewalGraph(pw, page, ctx, captured, sessionDir, configDir)
	if err == nil {
		err = fullRenewalPostGraph(pw, page, ctx, captured, sessionDir)
	}
	if err != nil {
		return page, ctx, err
	}
	return page, ctx, nil
}

// captureLoop navigates Teams and runs the 90-second token capture loop.
// It returns:
//   - captureOK when Teams loaded and tokens were captured (or the loop
//     finished without detecting a stale session)
//   - captureStale when nothing was captured within 45s (Teams never loaded —
//     the saved session's Microsoft login expired)
//   - captureClosed when the browser was closed by the user
func captureLoop(page playwright.Page, ctx playwright.BrowserContext, pw *playwright.Playwright, sessionDir string, captured *tokens, browserClosed <-chan struct{}) string {
	page.Goto("https://teams.microsoft.com/v2/#/teams/", playwright.PageGotoOptions{
		Timeout: playwright.Float(15000),
	})
	time.Sleep(5 * time.Second)

	startTime := time.Now()
	deadline := startTime.Add(90 * time.Second)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		select {
		case <-browserClosed:
			fmt.Println("\n  · Browser closed — stopping capture.")
			return captureClosed
		case <-ticker.C:
			// continue with normal logic
		}
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

		// Nothing captured headless after 45s → the Microsoft session expired,
		// Teams never loaded, and the user must sign in again.
		// (The cookie can still be captured on a stale session, so web/spaces —
		// which only appear once Teams fully loads — are the reliable signal.)
		if elapsed > 45*time.Second && !hasWeb && !hasSpaces {
			return captureStale
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
			time.Sleep(3 * time.Second)

			eduSelectors := []string{
				`[role="navigation"] span:text-is("Assignments")`,
				`[data-tid="app-bar-edu-assignments"]`,
				`span:text-is("Assignments") >> nth=0`,
			}
			clickAssignments(page, eduSelectors, 15*time.Second)
			time.Sleep(3 * time.Second)

			// Extract from every page because Teams may open Assignments in a new tab.
			for _, candidate := range ctx.Pages() {
				if err := tryExtractEduTokenFromJS(candidate, captured); err != nil {
					debugPrintf("  ⚠ EDU token JS extraction failed on %s: %v\n", candidate.URL(), err)
				}
			}
		}

		// Step 4 (55s): try to extract EDU_TOKEN from JS
		// Step 5 (70s): Navigate to private channel to get FabricToken
		if elapsed > 70*time.Second && !hasFabric && hasGraph {
			globalSpin.SetLabel("Automating TEAMS_FABRIC_TOKEN capture...")
			manualFabricTokenCapture(pw, sessionDir, captured)
		}

		if elapsed > 55*time.Second && !hasEdu {
			globalSpin.SetLabel("Attempting to extract EDU_TOKEN from JS memory...")
			if err := tryExtractEduTokenFromJS(page, captured); err != nil {
				fmt.Printf("  ⚠ EDU token JS extraction failed: %v\n", err)
			}
		}
	}

	return captureOK
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

func waitForVisibleGraphCandidate(_ playwright.Page, captured *tokens, timeout time.Duration) string {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		if activeContexts.isClosing() {
			return "cancelled"
		}
		captured.mu.Lock()
		ready := captured.graphCandidate != nil
		captured.mu.Unlock()
		if ready {
			return "ready"
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			return "timeout"
		}
	}
}

func waitForGraphOutcome(page playwright.Page, captured *tokens, timeout time.Duration) string {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		if activeContexts.isClosing() {
			return "cancelled"
		}
		captured.mu.Lock()
		ready := captured.graphCandidate != nil
		captured.mu.Unlock()
		if ready {
			return "ready"
		}
		if state := graphPageInteractionState(page); state != "" {
			return state
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			return "timeout"
		}
	}
}

func graphPageInteractionState(page playwright.Page) string {
	if isLoginURL(page.URL()) {
		return "interaction_required"
	}
	value, err := page.Evaluate(`() => (document.body && document.body.innerText || "").toLowerCase()`)
	if err != nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	for _, marker := range []string{"consent required", "multi-factor authentication", "verify your identity", "pick an account", "select an account"} {
		if strings.Contains(text, marker) {
			return "interaction_required"
		}
	}
	return ""
}

// tryExtractGraphTokenViaGraphExplorer navigates to Graph Explorer and extracts
// the token with the correct scopes (Chat.ReadBasic, Files.Read, etc.).
func graphRequestCandidate(token, requestURL, pageURL, authHeader string, mainFrame bool) (auth.GraphCandidate, bool) {
	if !mainFrame || !strings.HasPrefix(authHeader, "Bearer ") {
		return auth.GraphCandidate{}, false
	}
	u, err := url.Parse(requestURL)
	if err != nil || u.Scheme != "https" || u.Host != "graph.microsoft.com" {
		return auth.GraphCandidate{}, false
	}
	return auth.GraphCandidate{Token: token, RequestURL: requestURL, PageURL: pageURL, ClientID: auth.GraphExplorerClientID}, true
}

func captureGraphCandidate(captured *tokens, token, requestURL, pageURL string) {
	candidate := auth.GraphCandidate{Token: token, RequestURL: requestURL, PageURL: pageURL, ClientID: auth.GraphExplorerClientID}
	captured.mu.Lock()
	defer captured.mu.Unlock()
	if captured.graphCandidate == nil {
		// Keep the untrusted token quarantined until PersistGraphCandidate validates it.
		captured.graphCandidate = &candidate
	}
}

func tryExtractGraphTokenViaGraphExplorer(page playwright.Page, captured *tokens, autoClick bool) error {
	page.On("request", func(req playwright.Request) {
		authHeader := ""
		for key, value := range req.Headers() {
			if strings.EqualFold(key, "authorization") {
				authHeader = value
				break
			}
		}
		requestURL := req.URL()
		candidate, ok := graphRequestCandidate(strings.TrimPrefix(authHeader, "Bearer "), requestURL, page.URL(), authHeader, req.Frame() == page.MainFrame())
		if !ok {
			return
		}
		captureGraphCandidate(captured, candidate.Token, candidate.RequestURL, candidate.PageURL)
	})

	_, err := page.Goto("https://developer.microsoft.com/en-us/graph/graph-explorer",
		playwright.PageGotoOptions{Timeout: playwright.Float(20000)},
	)
	if err != nil {
		return errGraphNetwork
	}

	if !eventMode {
		time.Sleep(5 * time.Second)
	}

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
				if !eventMode {
					time.Sleep(3 * time.Second)
				}
				break
			}
		}
	} else {
		fmt.Println("→ First run detected. Waiting for you to sign in to Graph Explorer.")
		fmt.Println("  Once signed in, click 'Run query' to capture the token.")
		fmt.Println("  The helper will detect it automatically.")
	}

	// Graph tokens are accepted only from the request listener above; browser storage is not an acquisition boundary.
	// Only redirect if a validated/persistable candidate was observed.
	captured.mu.Lock()
	hasToken := captured.graphCandidate != nil
	captured.mu.Unlock()
	if hasToken {
		_, _ = page.Goto("https://teams.microsoft.com/v2/",
			playwright.PageGotoOptions{Timeout: playwright.Float(15000)},
		)
	}
	return nil
}

// tryExtractEduTokenFromJS attempts to extract the EDU_TOKEN from
// Teams' localStorage/sessionStorage (MSAL cache for Assignments).
func tryExtractEduTokenFromJS(page playwright.Page, captured *tokens) error {
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

	var lastErr error
	failedEvaluations := 0
	for _, script := range scripts {
		result, err := page.Evaluate(script)
		if err != nil {
			lastErr = err
			failedEvaluations++
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
				return nil
			}
		}
	}
	if failedEvaluations == len(scripts) && lastErr != nil {
		return fmt.Errorf("all browser storage evaluations failed: %w", lastErr)
	}
	return nil
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
		"   (It will fail if you are the only owner",
		"but the token will be captured instantly!)",
		"Press [Enter] in this terminal to skip.",
	})
	fmt.Println()

	visibleCtx, err := openPersistentContext(pw, sessionDir, false)
	if err != nil {
		fmt.Printf("  ⚠ Could not open browser for manual capture: %v\n", err)
		return
	}
	activeContexts.add(visibleCtx)
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
				fmt.Println("\n  " + symOK + "  TEAMS_FABRIC_TOKEN manually captured!")
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
			if globalSpin != nil {
				globalSpin.Stop("")
				globalSpin = nil
			}
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
			fmt.Println("  " + symOK + "  Token captured! Browser closes in 10s...")
			time.Sleep(10 * time.Second) // wait before closing browser
			return
		}
	}

	fmt.Println("\n  · TEAMS_FABRIC_TOKEN — timed out waiting for manual action")
	if globalSpin != nil {
		globalSpin.Stop("")
		globalSpin = nil
	}
}

type spinner struct {
	frames []string
	stop   chan struct{}
	mu     sync.Mutex
	label  string
}

// spinnerFrames returns braille dots on Unix/Windows Terminal and an ASCII
// fallback for legacy Windows consoles (cmd.exe/PowerShell conhost), which
// cannot render U+2800 glyphs.
func spinnerFrames() []string {
	if legacyWindowsConsole() {
		return []string{"|", "/", "-", "\\"}
	}
	return []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
}

func newSpinner(label string) *spinner {
	return &spinner{
		frames: spinnerFrames(),
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
	case "darwin":
		base = filepath.Join(helpers.HomeDir(), "Library", "Caches", "ms-playwright-go")
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
