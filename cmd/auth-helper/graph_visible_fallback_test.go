package main

import (
	"errors"
	"testing"
	"time"

	playwright "github.com/mxschmitt/playwright-go"
)

// These doubles cannot access the network or a browser profile.
type signInOnlyPage struct{ playwright.Page }

func (*signInOnlyPage) On(string, any) {}
func (*signInOnlyPage) IsClosed() bool { return false }
func (*signInOnlyPage) URL() string    { return "about:blank" }
func (*signInOnlyPage) Goto(string, ...playwright.PageGotoOptions) (playwright.Response, error) {
	return nil, nil
}
func (*signInOnlyPage) Locator(string, ...playwright.PageLocatorOptions) playwright.Locator {
	return signInOnlyLocator{}
}

type embeddedLocator = playwright.Locator
type signInOnlyLocator struct{ embeddedLocator }

func (signInOnlyLocator) Count() (int, error) { return 1, nil }
func (signInOnlyLocator) IsVisible(...playwright.LocatorIsVisibleOptions) (bool, error) {
	return true, nil
}

func TestGraphVisibleFallbackAllowsManualWait(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // No designated account: automation must stop before clicking.
	oldEvents := eventMode
	t.Cleanup(func() { eventMode = oldEvents })
	for _, events := range []bool{false, true} {
		name := "without_events"
		if events {
			name = "with_events"
		}
		t.Run(name, func(t *testing.T) {
			eventMode = events
			for _, outcome := range []string{"ready", "timeout", "cancelled"} {
				t.Run(outcome, func(t *testing.T) {
					captured := &tokens{}
					page := &signInOnlyPage{}
					waited, persisted, interacted := false, false, false
					err := runGraphLifecycle(time.Minute,
						func() (string, error) {
							err := tryExtractGraphTokenViaGraphExplorer(page, captured, true)
							if errors.Is(err, errGraphInteractionRequired) {
								return "interaction_required", nil
							}
							return "", err
						},
						func() (string, error) {
							if err := tryExtractGraphTokenViaGraphExplorer(page, captured, false); err != nil {
								return "", err
							}
							waited = true
							return outcome, nil
						},
						func() error { persisted = true; return nil },
						func() error { interacted = true; return nil },
						func() bool { return false },
					)
					if !waited || !interacted {
						t.Fatalf("manual wait unreachable: waited=%v interaction=%v err=%v", waited, interacted, err)
					}
					switch outcome {
					case "ready":
						if err != nil || !persisted {
							t.Fatalf("manual success not persisted: %v", err)
						}
					case "timeout":
						if !errors.Is(err, errGraphTimeout) || persisted {
							t.Fatalf("timeout contract: %v", err)
						}
					case "cancelled":
						if !errors.Is(err, errGraphCancelled) || persisted {
							t.Fatalf("cancel contract: %v", err)
						}
					}
				})
			}
		})
	}
}
