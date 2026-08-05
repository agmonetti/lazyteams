package graph

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type unauthorizedTransport struct{}

func (unauthorizedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusUnauthorized,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
	}, nil
}

func TestCheckAllTokensReturnsDeterministicOrderAndExcludesFabric(t *testing.T) {
	client := &Client{
		GraphToken:  "graph",
		WebToken:    "web",
		NotifToken:  "notif",
		EduToken:    "edu",
		FabricToken: "fabric",
		HTTPClient:  &http.Client{Transport: unauthorizedTransport{}},
	}

	got := client.CheckAllTokens()
	want := []string{"graph", "web", "notif", "edu"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d: expected %q, got %q", i, want[i], got[i])
		}
	}
}
