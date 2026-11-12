package main

import (
	"errors"
	"fmt"
	"io"
	"syscall"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

type disconnectedCapturePage struct {
	playwright.Page
	calls  int
	failAt int
}

func (p *disconnectedCapturePage) Goto(string, ...playwright.PageGotoOptions) (playwright.Response, error) {
	p.calls++
	if p.calls < p.failAt {
		return nil, nil
	}
	return nil, fmt.Errorf("driver disconnected: %w", playwright.ErrTargetClosed)
}

func TestFatalPlaywrightError(t *testing.T) {
	for _, err := range []error{playwright.ErrTargetClosed, io.EOF, io.ErrClosedPipe, syscall.EPIPE} {
		if !fatalPlaywrightError(fmt.Errorf("navigation: %w", err)) {
			t.Fatalf("not classified as fatal: %v", err)
		}
	}
	for _, err := range []error{nil, errors.New("navigation timeout"), errors.New("net::ERR_ABORTED")} {
		if fatalPlaywrightError(err) {
			t.Fatalf("ordinary navigation error classified as fatal: %v", err)
		}
	}
}

func TestCaptureLoopStopsOnDriverFailureWithoutCloseEvent(t *testing.T) {
	page := &disconnectedCapturePage{}
	result := captureLoop(page, nil, nil, "", &tokens{}, make(chan struct{}))
	if result != captureDisconnected || page.calls != 1 {
		t.Fatalf("result=%q, navigation calls=%d", result, page.calls)
	}
}

func TestCaptureLoopStopsOnActivityDriverFailure(t *testing.T) {
	oldSpin := globalSpin
	globalSpin = newSpinner("test")
	t.Cleanup(func() { globalSpin = oldSpin })
	page := &disconnectedCapturePage{failAt: 2}
	result := captureLoop(page, nil, nil, "", &tokens{}, make(chan struct{}))
	if result != captureDisconnected || page.calls != 2 {
		t.Fatalf("result=%q, navigation calls=%d", result, page.calls)
	}
}

func TestFullRenewalStopsOnDriverFailure(t *testing.T) {
	oldGoto, oldCapture, oldSleep, oldGraph := fullRenewalGoto, fullRenewalCapture, fullRenewalSleep, fullRenewalGraph
	t.Cleanup(func() {
		fullRenewalGoto, fullRenewalCapture, fullRenewalSleep, fullRenewalGraph = oldGoto, oldCapture, oldSleep, oldGraph
	})
	fullRenewalSleep = func(time.Duration) {}
	fullRenewalGraph = func(*playwright.Playwright, playwright.Page, playwright.BrowserContext, *tokens, string, string) (playwright.Page, playwright.BrowserContext, error) {
		t.Fatal("must not continue authentication after driver failure")
		return nil, nil, nil
	}
	for _, duringCapture := range []bool{false, true} {
		fullRenewalGoto = func(playwright.Page) error {
			if duringCapture {
				return nil
			}
			return playwright.ErrTargetClosed
		}
		fullRenewalCapture = func(playwright.Page, playwright.BrowserContext, *playwright.Playwright, string, *tokens, <-chan struct{}) string {
			if !duringCapture {
				t.Fatal("capture entered after fatal initial navigation")
			}
			return captureDisconnected
		}
		_, _, err := fullRenewal(nil, nil, nil, &tokens{}, "", false, make(chan struct{}), "")
		if !errors.Is(err, playwright.ErrTargetClosed) {
			t.Fatalf("duringCapture=%v: got %v", duringCapture, err)
		}
	}
}
