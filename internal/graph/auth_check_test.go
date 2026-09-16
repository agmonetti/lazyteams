package graph

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/agmonetti/lazyteams/internal/auth"
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
	client := NewClient(&auth.CredentialSet{
		Graph:  auth.Credential{Token: "graph"},
		Web:    auth.Credential{Token: "web"},
		Notif:  auth.Credential{Token: "notif"},
		EDU:    auth.Credential{Token: "edu"},
		Fabric: auth.Credential{Token: "fabric"},
	})
	client.HTTPClient = &http.Client{Transport: unauthorizedTransport{}}

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

func TestCredentialPublicationIsGenerationCoherent(t *testing.T) {
	client := NewClient(&auth.CredentialSet{})
	var wg sync.WaitGroup
	var mismatch atomic.Bool
	wg.Add(2)
	go func() {
		defer wg.Done()
		for generation := uint64(1); generation <= 1000; generation++ {
			client.credentials.Store(&auth.CredentialSet{
				Generation: generation,
				Graph:      auth.Credential{Token: fmt.Sprintf("graph-%d", generation)},
				Web:        auth.Credential{Token: fmt.Sprintf("web-%d", generation)},
			})
		}
	}()
	go func() {
		defer wg.Done()
		for range 10000 {
			set := client.Credentials()
			if set.Generation == 0 {
				continue
			}
			want := fmt.Sprintf("%d", set.Generation)
			if strings.TrimPrefix(set.Graph.Token, "graph-") != want || strings.TrimPrefix(set.Web.Token, "web-") != want {
				mismatch.Store(true)
				return
			}
		}
	}()
	wg.Wait()
	if mismatch.Load() {
		t.Fatal("reader observed credentials from mixed generations")
	}
}
