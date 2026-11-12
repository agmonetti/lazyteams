package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	playwright "github.com/mxschmitt/playwright-go"
)

// With a live Microsoft session the sign-in popup can finish by itself (SSO)
// and close without ever showing an account card. That is a completed sign-in,
// not a request for interaction; a popup that stays open on a challenge still is.
func TestGraphSignInPopupThatCompletesByItselfStaysHeadless(t *testing.T) {
	for _, tc := range []struct {
		name, popup, want string
	}{
		{"sso completes and closes", `<p>Signing you in…</p><script>setTimeout(function(){window.opener.location='/return';window.close()},400)</script>`, ""},
		{"challenge stays open", `<input type="password">`, "interaction_required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			page := func(path, body string) {
				mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/html")
					fmt.Fprint(w, body)
				})
			}
			page("/graph", `<button aria-label="Sign in" onclick="window.open('/ms','selector')">Sign in</button>`)
			page("/ms", tc.popup)
			page("/return", `<button aria-label="Sign out">Sign out</button><button aria-label="Run query">Run query</button>`)
			server := httptest.NewServer(mux)
			defer server.Close()

			oldOrigin, oldSettle := graphMicrosoftOrigin, graphSignInSettleTimeout
			graphMicrosoftOrigin = server.URL + "/ms"
			graphSignInSettleTimeout = 3e9
			t.Cleanup(func() { graphMicrosoftOrigin, graphSignInSettleTimeout = oldOrigin, oldSettle })
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
			p, err := browser.NewPage()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.Goto(server.URL + "/graph"); err != nil {
				t.Fatal(err)
			}
			if got := tryGraphHeadlessSignIn(p, &tokens{}); got != tc.want {
				t.Fatalf("sign-in state = %q, want %q", got, tc.want)
			}
		})
	}
}
