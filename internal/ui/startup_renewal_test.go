package ui

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/agmonetti/lazyteams/internal/auth"
	"github.com/agmonetti/lazyteams/internal/graph"
	tea "github.com/charmbracelet/bubbletea"
)

// graphExpiredTransport answers Graph requests with 401 and leaves other
// services untouched, emulating a startup where MS_GRAPH_TOKEN was revoked or
// expired while the stored snapshot is still present.
func graphExpiredTransport() http.RoundTripper {
	return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		status := http.StatusOK
		body := `{"value":[],"id":"local-user"}`
		if strings.Contains(req.URL.Host, "graph.microsoft.com") {
			status = http.StatusUnauthorized
			body = `{"error":{"message":"Lifetime validation failed, the token is expired."}}`
		}
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
}

func runStartupGraphLoads(t *testing.T, client *graph.Client) (Model, []string) {
	t.Helper()
	m := New(client, "user", false)

	var jobs []string
	old := startBatchRenewal
	startBatchRenewal = func(m *Model, requested []string) tea.Cmd {
		jobs = append([]string(nil), requested...)
		// Mirror the state authHelperBatch establishes before the first event.
		m.renewalBatch = true
		m.batchRequestedProviders = make(map[string]bool, len(requested))
		for _, job := range requested {
			m.batchRequestedProviders[job] = true
		}
		return func() tea.Msg { return tokenRenewedMsg{tokenType: "batch"} }
	}
	t.Cleanup(func() { startBatchRenewal = old })

	for _, cmd := range []tea.Cmd{loadTeamsCmd(client), loadChatsCmd(client), loadMeCmd(client)} {
		msg := cmd()
		updated, _ := m.Update(msg)
		m = updated.(Model)
	}
	return m, jobs
}

// TestStartupExpiredGraphLaunchesBatchRenewal covers the TUI start with an
// expired MS_GRAPH_TOKEN: the first Graph loads fail with 401 and the model must
// hand the renewal to the existing batch lifecycle, not to another provider.
func TestStartupExpiredGraphLaunchesBatchRenewal(t *testing.T) {
	client := graph.NewClient(&auth.CredentialSet{
		Generation: 3,
		Graph:      auth.Credential{Token: "expired-graph-token"},
	})
	client.HTTPClient = &http.Client{Transport: graphExpiredTransport()}

	m, jobs := runStartupGraphLoads(t, client)

	if len(jobs) == 0 || !sameStrings(jobs, []string{CapabilityGraph}) {
		t.Fatalf("startup renewal jobs = %v, want [graph]", jobs)
	}
	if !m.tokenRenewing || m.tokenRenewingType != CapabilityGraph {
		t.Fatalf("renewal state = %v/%q, want graph renewing", m.tokenRenewing, m.tokenRenewingType)
	}
}

// TestStartupExpiredGraphDoesNotRenewWeb guards the regression where a Graph
// 401 was attributed to the Teams Web token, renewing the wrong credential.
func TestStartupExpiredGraphDoesNotRenewWeb(t *testing.T) {
	client := graph.NewClient(&auth.CredentialSet{
		Generation: 3,
		Graph:      auth.Credential{Token: "expired-graph-token"},
		Web:        auth.Credential{Token: "valid-web-token"},
	})
	client.HTTPClient = &http.Client{Transport: graphExpiredTransport()}

	m, jobs := runStartupGraphLoads(t, client)

	for _, job := range jobs {
		if job == CapabilityWeb {
			t.Fatalf("startup renewal jobs = %v, must not renew web for a Graph 401", jobs)
		}
	}
	if !m.tokenRenewing {
		t.Fatal("expired Graph token did not enter renewal state")
	}
}

// TestStartupGraphRenewalRecoveryCompletes covers the rest of the startup
// recovery: once the helper publishes the renewed Graph credential, the model
// reloads the Graph data, clears the renewal state and never touches another
// provider.
func TestStartupGraphRenewalRecoveryCompletes(t *testing.T) {
	client := graph.NewClient(&auth.CredentialSet{
		Generation: 4,
		Graph:      auth.Credential{Token: "expired-graph-token"},
	})
	client.HTTPClient = &http.Client{Transport: graphExpiredTransport()}

	m, jobs := runStartupGraphLoads(t, client)
	if !sameStrings(jobs, []string{CapabilityGraph}) {
		t.Fatalf("startup renewal jobs = %v, want [graph]", jobs)
	}

	updated, _ := m.Update(authEventMsg{event: stage4Event("started", "batch", "running", 4)})
	m = updated.(Model)
	updated, cmd := m.Update(authEventMsg{event: stage4Event("credential_ready", CapabilityGraph, "ready", 4)})
	m = updated.(Model)
	if cmd == nil || m.batchReadyProvider != CapabilityGraph {
		t.Fatalf("credential_ready did not request a Graph reload: cmd=%v provider=%q", cmd, m.batchReadyProvider)
	}

	updated, cmd = m.Update(tokensReloadedMsg{tokenType: CapabilityGraph})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("renewed Graph credential did not reload Graph data")
	}
	if m.capabilities[CapabilityGraph] != CapabilityReady {
		t.Fatalf("Graph capability = %q, want ready", m.capabilities[CapabilityGraph])
	}

	updated, _ = m.Update(authEventMsg{event: stage4Event("completed", "batch", "completed", 4)})
	m = updated.(Model)
	updated, _ = m.Update(tokenRenewedMsg{tokenType: "batch"})
	m = updated.(Model)

	if m.tokenRenewing || len(m.tokenRenewalQueue) != 0 {
		t.Fatalf("renewal did not finish: renewing=%v queue=%v", m.tokenRenewing, m.tokenRenewalQueue)
	}
	if !sameStrings(jobs, []string{CapabilityGraph}) {
		t.Fatalf("recovery launched extra jobs: %v", jobs)
	}
}

// TestStartupValidGraphDoesNotRenew keeps the complementary invariant: a live
// credential must not start the helper at all.
func TestStartupValidGraphDoesNotRenew(t *testing.T) {
	client := graph.NewClient(&auth.CredentialSet{
		Generation: 3,
		Graph:      auth.Credential{Token: "valid-graph-token"},
	})
	var requests atomic.Int32
	client.HTTPClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"value":[],"id":"local-user"}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}

	m, jobs := runStartupGraphLoads(t, client)

	if len(jobs) != 0 || m.tokenRenewing {
		t.Fatalf("valid Graph token launched renewal: jobs=%v renewing=%v", jobs, m.tokenRenewing)
	}
	if requests.Load() == 0 {
		t.Fatal("startup loads did not reach the API")
	}
}
