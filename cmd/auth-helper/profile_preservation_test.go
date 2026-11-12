package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/agmonetti/lazyteams/internal/auth"
	"github.com/mxschmitt/playwright-go"
)

func TestUserCancelledLoginIsTheOnlyProfileClearingCondition(t *testing.T) {
	if !userCancelledLogin(true, true, true, false, false) {
		t.Fatal("closing the first visible login window must clear the profile")
	}
	for name, got := range map[string]bool{
		"targeted renewal":       userCancelledLogin(false, true, true, false, false),
		"existing or headless":   userCancelledLogin(true, false, true, false, false),
		"helper-initiated close": userCancelledLogin(true, true, false, false, false),
		"already signed in":      userCancelledLogin(true, true, true, true, false),
		"signal or driver loss":  userCancelledLogin(true, true, true, false, true),
	} {
		if got {
			t.Fatalf("%s must keep the profile", name)
		}
	}
}

// Drives the public run path with an injected browser: the persistent profile
// must survive every failure that is not the user abandoning the first login.
func TestRunKeepsProfileUnlessUserCancelsFirstLogin(t *testing.T) {
	oldArgs := os.Args
	oldInstall, oldPlaywrightRun := runPlaywrightInstall, runPlaywrightRun
	oldOpen, oldPages, oldNewPage := runOpenPersistentContext, runContextPages, runContextNewPage
	oldViewport, oldOnClose, oldRegister, oldClose := runPageSetViewport, runContextOnClose, runRegisterInterceptor, runCloseContext
	oldFull := fullRenewalRunner
	t.Cleanup(func() {
		os.Args = oldArgs
		runPlaywrightInstall, runPlaywrightRun = oldInstall, oldPlaywrightRun
		runOpenPersistentContext, runContextPages, runContextNewPage = oldOpen, oldPages, oldNewPage
		runPageSetViewport, runContextOnClose, runRegisterInterceptor, runCloseContext = oldViewport, oldOnClose, oldRegister, oldClose
		fullRenewalRunner = oldFull
	})
	runPlaywrightInstall = func(...*playwright.RunOptions) error { return nil }
	runPlaywrightRun = func(...*playwright.RunOptions) (*playwright.Playwright, error) { return nil, nil }
	runOpenPersistentContext = func(*playwright.Playwright, string, bool) (playwright.BrowserContext, error) { return nil, nil }
	runContextPages = func(playwright.BrowserContext) []playwright.Page { return nil }
	runContextNewPage = func(playwright.BrowserContext) (playwright.Page, error) { return nil, nil }
	runPageSetViewport = func(playwright.Page, int, int) error { return nil }
	runRegisterInterceptor = func(playwright.BrowserContext, *tokens) {}
	var onClose func()
	runContextOnClose = func(_ playwright.BrowserContext, fn func()) { onClose = fn }
	// Like Playwright, closing the context fires its close event.
	runCloseContext = func(playwright.BrowserContext) error { onClose(); return nil }

	for _, tc := range []struct {
		name        string
		existing    bool // profile from an earlier successful login
		userCloses  bool // window closed during the flow, outside the helper
		signedIn    bool
		flowErr     error
		wantProfile bool
	}{
		{name: "reused profile, Graph rejected", existing: true, flowErr: errors.New("incompatible credential"), wantProfile: true},
		{name: "reused profile, partial capture", existing: true, wantProfile: true},
		{name: "first login, failure after sign-in", signedIn: true, flowErr: errGraphTimeout, wantProfile: true},
		{name: "first login, window closed after sign-in", signedIn: true, userCloses: true, wantProfile: true},
		{name: "first login, timeout without closing", flowErr: errGraphTimeout, wantProfile: true},
		{name: "first login, driver lost", userCloses: true, flowErr: playwright.ErrTargetClosed, wantProfile: true},
		{name: "first login, user closes window", userCloses: true, wantProfile: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			sessionDir := filepath.Join(home, ".config", "lazyteams", "browser-session")
			marker := filepath.Join(sessionDir, "cookies.sqlite")
			if tc.existing {
				if err := os.MkdirAll(sessionDir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(marker, []byte("session"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			fullRenewalRunner = func(_ *playwright.Playwright, page playwright.Page, ctx playwright.BrowserContext, captured *tokens, dir string, _ bool, _ <-chan struct{}, _ string) (playwright.Page, playwright.BrowserContext, error) {
				// Firefox writes the profile as soon as it starts.
				if err := os.WriteFile(filepath.Join(dir, "cookies.sqlite"), []byte("session"), 0o600); err != nil {
					t.Fatal(err)
				}
				if tc.signedIn {
					captured.mu.Lock()
					captured.webToken = "web"
					captured.mu.Unlock()
				}
				if tc.userCloses {
					onClose()
				}
				return page, ctx, tc.flowErr
			}
			os.Args = []string{"lazyteams-auth"}
			run()
			_, err := os.Stat(marker)
			if got := err == nil; got != tc.wantProfile {
				t.Fatalf("profile kept = %v, want %v", got, tc.wantProfile)
			}
		})
	}
}

// An EDU token intercepted while an earlier job loaded Teams is still a
// successful EDU renewal once the edu job validates and persists it.
func TestBatchAcceptsCredentialInterceptedBeforeItsJob(t *testing.T) {
	values := auth.TokenValues{}
	err := runBatchJobSequence("web,edu", func() auth.TokenValues { return values }, func(job string) error {
		if job == "web_notif" {
			values.Web, values.Notif, values.EDU = "web", "notif", "edu"
		}
		return nil
	}, func(job string, current auth.TokenValues, last map[string]string) (bool, error) {
		if job == "edu" {
			last["edu"] = current.EDU
		} else {
			last["web"], last["notif"] = current.Web, current.Notif
		}
		return true, nil
	}, func() string { return "" })
	if err != nil {
		t.Fatalf("batch failed although every credential was persisted: %v", err)
	}
}
