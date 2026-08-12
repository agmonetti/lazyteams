package graph

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type recordedRequest struct {
	method string
	url    string
	auth   string
	body   string
}

// captureTransport records every request it receives and answers with a
// canned status/body, so tests can inspect what the Client actually sent.
type captureTransport struct {
	mu       sync.Mutex
	status   int
	respBody string
	reqs     []recordedRequest
}

func (t *captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var b []byte
	if req.Body != nil {
		b, _ = io.ReadAll(req.Body)
	}
	rec := recordedRequest{
		method: req.Method,
		url:    req.URL.String(),
		auth:   req.Header.Get("Authorization"),
		body:   string(b),
	}
	t.mu.Lock()
	t.reqs = append(t.reqs, rec)
	t.mu.Unlock()
	return &http.Response{
		StatusCode: t.status,
		Body:       io.NopCloser(strings.NewReader(t.respBody)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func (t *captureTransport) last() recordedRequest {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.reqs[len(t.reqs)-1]
}

func (t *captureTransport) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.reqs)
}

func newPresenceTestClient(tr *captureTransport) *Client {
	return &Client{
		GraphToken: "test-token",
		HTTPClient: &http.Client{Transport: tr},
	}
}

func TestSetPresenceSuccess(t *testing.T) {
	tr := &captureTransport{status: http.StatusOK}
	client := newPresenceTestClient(tr)

	if err := client.SetPresence("user-1", "Busy", "Busy"); err != nil {
		t.Fatalf("SetPresence() = %v, want nil", err)
	}

	req := tr.last()
	if req.method != http.MethodPost {
		t.Errorf("method = %q, want POST", req.method)
	}
	wantURL := "https://graph.microsoft.com/v1.0/users/user-1/presence/setUserPreferredPresence"
	if req.url != wantURL {
		t.Errorf("url = %q, want %q", req.url, wantURL)
	}
	if req.auth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want Bearer test-token", req.auth)
	}
	for _, want := range []string{`"availability":"Busy"`, `"activity":"Busy"`, `"expirationDuration":"PT1H"`} {
		if !strings.Contains(req.body, want) {
			t.Errorf("body %q does not contain %q", req.body, want)
		}
	}
}

func TestSetPresenceError400(t *testing.T) {
	tr := &captureTransport{status: http.StatusBadRequest, respBody: `{"error":{"message":"presence not set"}}`}
	client := newPresenceTestClient(tr)

	err := client.SetPresence("user-1", "Busy", "Busy")
	if err == nil {
		t.Fatal("SetPresence() = nil, want error")
	}
	if !strings.Contains(err.Error(), "status 400") {
		t.Errorf("error = %q, want it to contain status 400", err)
	}
	if !strings.Contains(err.Error(), "presence not set") {
		t.Errorf("error = %q, want it to contain the response body", err)
	}
}

func TestClearPresenceSuccess(t *testing.T) {
	tr := &captureTransport{status: http.StatusOK}
	client := newPresenceTestClient(tr)

	if err := client.ClearPresence("user-1"); err != nil {
		t.Fatalf("ClearPresence() = %v, want nil", err)
	}

	req := tr.last()
	if req.method != http.MethodPost {
		t.Errorf("method = %q, want POST", req.method)
	}
	wantURL := "https://graph.microsoft.com/v1.0/users/user-1/presence/clearUserPreferredPresence"
	if req.url != wantURL {
		t.Errorf("url = %q, want %q", req.url, wantURL)
	}
	if req.auth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want Bearer test-token", req.auth)
	}
}

func TestClearPresenceError400(t *testing.T) {
	tr := &captureTransport{status: http.StatusBadRequest, respBody: `{"error":{"message":"nothing to clear"}}`}
	client := newPresenceTestClient(tr)

	err := client.ClearPresence("user-1")
	if err == nil {
		t.Fatal("ClearPresence() = nil, want error")
	}
	if !strings.Contains(err.Error(), "status 400") {
		t.Errorf("error = %q, want it to contain status 400", err)
	}
	if !strings.Contains(err.Error(), "nothing to clear") {
		t.Errorf("error = %q, want it to contain the response body", err)
	}
}

func TestSetPresenceEmptyUserID(t *testing.T) {
	tr := &captureTransport{status: http.StatusOK}
	client := newPresenceTestClient(tr)

	err := client.SetPresence("", "Busy", "Busy")
	if err == nil {
		t.Fatal("SetPresence with empty userID = nil, want error")
	}
	if !strings.Contains(err.Error(), "empty userID") {
		t.Errorf("error = %q, want it to mention the empty userID", err)
	}
	if n := tr.count(); n != 0 {
		t.Errorf("made %d request(s), want 0 (guard must reject before sending)", n)
	}
}

func TestClearPresenceEmptyUserID(t *testing.T) {
	tr := &captureTransport{status: http.StatusOK}
	client := newPresenceTestClient(tr)

	err := client.ClearPresence("")
	if err == nil {
		t.Fatal("ClearPresence with empty userID = nil, want error")
	}
	if !strings.Contains(err.Error(), "empty userID") {
		t.Errorf("error = %q, want it to mention the empty userID", err)
	}
	if n := tr.count(); n != 0 {
		t.Errorf("made %d request(s), want 0 (guard must reject before sending)", n)
	}
}
