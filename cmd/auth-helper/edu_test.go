package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

func eduJWTForTest(t *testing.T, audience string, expiry, notBefore int64) string {
	t.Helper()
	payload, err := json.Marshal(map[string]interface{}{"aud": audience, "exp": expiry, "nbf": notBefore})
	if err != nil {
		t.Fatal(err)
	}
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

const eduAudienceForTest = "api://8f348934-64be-4bb2-bc16-c54c96789f43"

func TestFreshEduToken(t *testing.T) {
	now := time.Unix(1800000000, 0)
	for _, tc := range []struct {
		name string
		aud  string
		exp  int64
		nbf  int64
		want bool
	}{
		{"current", eduAudienceForTest, now.Unix() + 3600, now.Unix() - 60, true},
		{"expired cache", eduAudienceForTest, now.Unix() - 1765175, 0, false},
		{"expires during handoff", eduAudienceForTest, now.Unix() + 30, 0, false},
		{"missing expiry", eduAudienceForTest, 0, 0, false},
		{"not active", eduAudienceForTest, now.Unix() + 3600, now.Unix() + 60, false},
		{"wrong audience", "https://graph.microsoft.com", now.Unix() + 3600, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := freshEduToken(eduJWTForTest(t, tc.aud, tc.exp, tc.nbf), now); got != tc.want {
				t.Fatalf("freshEduToken = %t, want %t", got, tc.want)
			}
		})
	}
	for _, token := range []string{"", "not-a-jwt", "header.invalid.signature"} {
		if freshEduToken(token, now) {
			t.Error("malformed token accepted")
		}
	}
}

type eduTestFrame struct {
	playwright.Frame
	values []interface{}
	err    error
	url    string
}

func (f *eduTestFrame) Evaluate(_ string, _ ...interface{}) (interface{}, error) {
	return f.values, f.err
}

func (f *eduTestFrame) URL() string { return f.url }

type eduTestPage struct {
	playwright.Page
	frames []playwright.Frame
}

func (p *eduTestPage) Frames() []playwright.Frame { return p.frames }
func (p *eduTestPage) URL() string                { return "https://teams.microsoft.com/v2/" }

func TestEduStorageSkipsExpiredEntryAndReadsAssignmentsFrame(t *testing.T) {
	now := time.Now().Unix()
	expired := eduJWTForTest(t, eduAudienceForTest, now-1765175, 0)
	fresh := eduJWTForTest(t, eduAudienceForTest, now+3600, 0)
	for _, tc := range []struct {
		name   string
		frames []playwright.Frame
	}{
		{"same storage", []playwright.Frame{&eduTestFrame{values: []interface{}{expired, fresh}}}},
		{"iframe storage", []playwright.Frame{
			&eduTestFrame{values: []interface{}{expired}},
			&eduTestFrame{values: []interface{}{expired, fresh}, url: "https://assignments.edu.cloud.microsoft/"},
		}},
		{"inaccessible frame", []playwright.Frame{
			&eduTestFrame{err: errors.New("detached frame")},
			&eduTestFrame{values: []interface{}{fresh}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captured := &tokens{}
			if err := tryExtractEduTokenFromJS(&eduTestPage{frames: tc.frames}, captured); err != nil {
				t.Fatal(err)
			}
			if captured.eduToken != fresh {
				t.Fatal("did not select the fresh EDU candidate")
			}
		})
	}
	captured := &tokens{}
	if err := tryExtractEduTokenFromJS(&eduTestPage{frames: []playwright.Frame{&eduTestFrame{values: []interface{}{expired}}}}, captured); err != nil {
		t.Fatal(err)
	}
	if captured.eduToken != "" {
		t.Fatal("expired-only cache must not produce a capture")
	}
}

type eduTestContext struct {
	playwright.BrowserContext
	onRequest func(playwright.Request)
}

func (c *eduTestContext) On(event string, handler interface{}) {
	if event == "request" {
		c.onRequest = handler.(func(playwright.Request))
	}
}

type eduTestRequest struct {
	playwright.Request
	url   string
	token string
}

func (r *eduTestRequest) URL() string { return r.url }
func (r *eduTestRequest) Headers() map[string]string {
	return map[string]string{"authorization": "Bearer " + r.token}
}

func TestEduRequestCallbackRejectsStaleTokenWithoutDriverCalls(t *testing.T) {
	ctx := &eduTestContext{}
	captured := &tokens{}
	registerFullInterceptor(ctx, captured)
	expired := eduJWTForTest(t, eduAudienceForTest, time.Now().Unix()-3600, 0)
	fresh := eduJWTForTest(t, eduAudienceForTest, time.Now().Unix()+3600, 0)
	ctx.onRequest(&eduTestRequest{url: "https://assignments.edu.cloud.microsoft/api/v1.0/edu/me/work", token: expired})
	if captured.eduToken != "" || !captured.eduRequestSeen {
		t.Fatal("request observation must not accept an expired token")
	}
	ctx.onRequest(&eduTestRequest{url: "https://assignments.edu.cloud.microsoft/api/v1.0/edu/me/work", token: fresh})
	if captured.eduToken != fresh {
		t.Fatal("fresh request token not captured")
	}
	// Embedded nil Playwright methods panic if the callback adds a driver RPC.
	graph := eduJWTForTest(t, "https://graph.microsoft.com", 0, 0)
	ctx.onRequest(&eduTestRequest{url: "https://graph.microsoft.com/v1.0/me", token: graph})
	if captured.graphToken != graph {
		t.Fatal("EDU freshness filter changed Graph capture")
	}
}

type eduRoundTripper func(*http.Request) (*http.Response, error)

func (f eduRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestEduValidationRequiresAssignmentsSuccess(t *testing.T) {
	for _, status := range []int{200, 302, 401, 403, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			client := &http.Client{Transport: eduRoundTripper(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host != "assignments.edu.cloud.microsoft" || req.URL.Path != "/api/v1.0/edu/me/work" || req.URL.Query().Get("$top") != "1" {
					t.Fatal("validation did not call the Assignments endpoint")
				}
				if req.Header.Get("Authorization") != "Bearer test-token" {
					t.Fatal("validation omitted bearer authentication")
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("sensitive-body"))}, nil
			})}
			err := validateEduToken(client, "test-token")
			if (err == nil) != (status == 200) {
				t.Fatalf("validation error = %v, status %d", err, status)
			}
			if err != nil && strings.Contains(err.Error(), "sensitive-body") {
				t.Fatal("validation exposed response contents")
			}
		})
	}
}

func TestSaveTokensDoesNotPersistUnvalidatedEduRenewal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.env")
	if err := os.WriteFile(path, []byte("export EDU_TOKEN=old-edu\nexport MS_GRAPH_TOKEN=keep-graph\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	captured := &tokens{eduToken: "candidate", eduValidationRequired: true, webToken: "new-web"}
	if err := saveTokens(captured, dir); err != nil {
		t.Fatal(err)
	}
	got := readTokensFileForTest(t, path)
	if got["EDU_TOKEN"] != "old-edu" || got["MS_GRAPH_TOKEN"] != "keep-graph" || got["TEAMS_WEB_TOKEN"] != "new-web" {
		t.Fatal("partial save replaced unvalidated EDU or lost other tokens")
	}
	captured.eduValidated = true
	if err := saveTokens(captured, dir); err != nil {
		t.Fatal(err)
	}
	if readTokensFileForTest(t, path)["EDU_TOKEN"] != "candidate" {
		t.Fatal("validated EDU was not persisted")
	}
}
