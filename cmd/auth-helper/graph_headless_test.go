package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agmonetti/lazyteams/internal/auth"
	playwright "github.com/mxschmitt/playwright-go"
)

func TestGraphAccountPrivate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(os.Getenv("HOME"), ".config", "lazyteams", "graph-account")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture@example.test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := graphAccount(); got != "fixture@example.test" {
		t.Fatalf("account not read")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if got := graphAccount(); got != "" {
		t.Fatal("public account accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", path); err != nil {
		t.Fatal(err)
	}
	if got := graphAccount(); got != "" {
		t.Fatal("symlink account accepted")
	}
}

func TestGraphSelectionSynthetic(t *testing.T) {
	for _, mode := range []string{"popup", "redirect", "ambiguous", "mfa", "consent"} {
		t.Run(mode, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/graph", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				if mode == "popup" {
					fmt.Fprint(w, `<button aria-label="Sign in" onclick="window.open('/ms','selector')">Sign in</button>`)
				} else {
					fmt.Fprint(w, `<button aria-label="Sign in" onclick="location.href='/ms'">Sign in</button>`)
				}
			})
			mux.HandleFunc("/ms", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				switch mode {
				case "mfa":
					fmt.Fprint(w, `<input type="password">`)
				case "consent":
					fmt.Fprint(w, `<button>Consent</button>`)
				default:
					card := `<button onclick="if(window.opener){window.opener.location='/return';window.close()}else location='/return'"><span>fixture@example.test</span> Signed in</button>`
					fmt.Fprint(w, card)
					if mode == "ambiguous" {
						fmt.Fprint(w, card)
					}
				}
			})
			mux.HandleFunc("/return", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<button aria-label="Run query">Run query</button>`)
			})
			server := httptest.NewServer(mux)
			defer server.Close()
			pw, err := playwright.Run()
			if err != nil {
				t.Fatal(err)
			}
			defer pw.Stop()
			browser, err := pw.Firefox.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
			if err != nil {
				t.Fatal(err)
			}
			defer browser.Close()
			page, err := browser.NewPage()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := page.Goto(server.URL + "/graph"); err != nil {
				t.Fatal(err)
			}
			if mode == "popup" {
				t.Setenv("HOME", t.TempDir())
				if state := tryGraphHeadlessSignIn(page, &tokens{}); state != "interaction_required" || len(page.Context().Pages()) != 1 {
					t.Fatal("missing designated account must fall back before clicking")
				}
			}
			if err := page.Locator(`button[aria-label="Sign in"]`).Click(); err != nil {
				t.Fatal(err)
			}
			got := graphSelectAccount(page, "fixture@example.test", server.URL+"/ms")
			if want := mode == "popup" || mode == "redirect"; got != want {
				t.Fatalf("selected=%v want=%v", got, want)
			}
			if got {
				deadline := time.Now().Add(5 * time.Second)
				for !strings.HasSuffix(page.URL(), "/return") && time.Now().Before(deadline) {
					time.Sleep(50 * time.Millisecond)
				}
				if !strings.HasSuffix(page.URL(), "/return") {
					t.Fatal("no graph return")
				}
			}
		})
	}
}

func TestTryGraphHeadlessSignInAutomatesTwoClicksWithoutChangingCandidate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graph", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<button aria-label="Sign in" onclick="window.open('/ms','selector')">Sign in</button>`)
	})
	mux.HandleFunc("/ms", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		card := `<button onclick="if(window.opener){window.opener.location='/return';window.close()}else location='/return'"><span>fixture@example.test</span> Signed in</button>`
		fmt.Fprint(w, card)
	})
	mux.HandleFunc("/return", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<button aria-label="Run query">Run query</button>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	oldOrigin := graphMicrosoftOrigin
	graphMicrosoftOrigin = server.URL + "/ms"
	t.Cleanup(func() { graphMicrosoftOrigin = oldOrigin })

	home := t.TempDir()
	t.Setenv("HOME", home)
	accPath := filepath.Join(home, ".config", "lazyteams", "graph-account")
	if err := os.MkdirAll(filepath.Dir(accPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(accPath, []byte("fixture@example.test\n"), 0600); err != nil {
		t.Fatal(err)
	}

	pw, err := playwright.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer pw.Stop()
	browser, err := pw.Firefox.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	page, err := browser.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.Goto(server.URL + "/graph"); err != nil {
		t.Fatal(err)
	}

	// Sign-in must not change the request listener's chosen candidate.
	candidate := &auth.GraphCandidate{Token: "premature-sample-token"}
	captured := &tokens{graphCandidate: candidate}

	state := tryGraphHeadlessSignIn(page, captured)
	if state != "" {
		t.Fatalf("expected success (''), got %q", state)
	}

	captured.mu.Lock()
	rem := captured.graphCandidate
	captured.mu.Unlock()
	if rem != candidate {
		t.Fatal("sign-in changed the request listener's candidate")
	}

	// Verify the page returned to /return after account selection
	deadline := time.Now().Add(5 * time.Second)
	for !strings.HasSuffix(page.URL(), "/return") && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.HasSuffix(page.URL(), "/return") {
		t.Fatal("browser did not return to graph return page after two clicks")
	}

	// Re-entering the headless path after silent sign-in leaves Run query owned by existing logic.
	if state := tryGraphHeadlessSignIn(page, captured); state != "" {
		t.Fatalf("already signed in should not require interaction: %q", state)
	}
}
