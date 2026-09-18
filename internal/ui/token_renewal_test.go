package ui

import (
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agmonetti/lazyteams/internal/auth"
	"github.com/agmonetti/lazyteams/internal/graph"
	tea "github.com/charmbracelet/bubbletea"
)

func TestEnqueueTokenRenewalDeduplicates(t *testing.T) {
	m := Model{}
	enqueueTokenRenewal(&m, "graph")
	enqueueTokenRenewal(&m, "edu")
	enqueueTokenRenewal(&m, "graph")

	want := []string{"graph", "edu"}
	if len(m.tokenRenewalQueue) != len(want) {
		t.Fatalf("expected queue %v, got %v", want, m.tokenRenewalQueue)
	}
	for i := range want {
		if m.tokenRenewalQueue[i] != want[i] {
			t.Errorf("queue %d: expected %q, got %q", i, want[i], m.tokenRenewalQueue[i])
		}
	}
}

func TestFinishTokenRenewalRemovesOnlyRequestedToken(t *testing.T) {
	m := Model{tokenRenewalQueue: []string{"graph", "edu", "notif"}}
	finishTokenRenewal(&m, "edu")
	want := []string{"graph", "notif"}
	if len(m.tokenRenewalQueue) != len(want) {
		t.Fatalf("expected queue %v, got %v", want, m.tokenRenewalQueue)
	}
	for i := range want {
		if m.tokenRenewalQueue[i] != want[i] {
			t.Errorf("queue %d: expected %q, got %q", i, want[i], m.tokenRenewalQueue[i])
		}
	}
}

func TestGraphReadyModelDoesNotLaunchHelper(t *testing.T) {
	old := startBatchRenewal
	oldStart := startAuthHelperProcess
	t.Cleanup(func() { startBatchRenewal = old; startAuthHelperProcess = oldStart })
	var helperLaunches atomic.Int32
	startBatchRenewal = func(_ *Model, _ []string) tea.Cmd {
		helperLaunches.Add(1)
		return nil
	}
	startAuthHelperProcess = func(*exec.Cmd) error {
		helperLaunches.Add(1)
		return nil
	}
	m := Model{client: graph.NewClient(&auth.CredentialSet{
		Graph: auth.Credential{Token: "local-graph-credential"},
	})}
	var requests atomic.Int32
	m.client.HTTPClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"local-user"}`)), Header: make(http.Header), Request: req}, nil
	})}
	if _, err := m.client.GetMe(); err != nil {
		t.Fatal(err)
	}
	if cmd := startNextTokenRenewal(&m); cmd != nil {
		t.Fatal("expected no renewal command for Graph-ready model")
	}
	if m.tokenRenewing {
		t.Fatal("Graph-ready model entered renewal state")
	}
	if helperLaunches.Load() != 0 || requests.Load() != 1 {
		t.Fatalf("helper launches/requests = %d/%d, want 0/1", helperLaunches.Load(), requests.Load())
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestStartNextTokenRenewalWithNoExpiredCredentialsDoesNotLaunchHelper(t *testing.T) {
	old := startBatchRenewal
	t.Cleanup(func() { startBatchRenewal = old })
	startBatchRenewal = func(_ *Model, _ []string) tea.Cmd {
		t.Fatal("valid credentials must not launch the auth helper")
		return nil
	}
	m := Model{}
	if cmd := startNextTokenRenewal(&m); cmd != nil {
		t.Fatal("expected no renewal command")
	}
	if m.tokenRenewing {
		t.Fatal("valid credentials must not enter renewal state")
	}
}

func TestStartNextGraphRenewalUsesBatchLifecycle(t *testing.T) {
	old := startBatchRenewal
	t.Cleanup(func() { startBatchRenewal = old })
	var got []string
	startBatchRenewal = func(_ *Model, jobs []string) tea.Cmd {
		got = append([]string(nil), jobs...)
		return func() tea.Msg { return nil }
	}
	m := Model{tokenRenewalQueue: []string{"graph"}}
	if cmd := startNextTokenRenewal(&m); cmd == nil {
		t.Fatal("expected renewal command")
	}
	if !sameStrings(got, []string{"graph"}) {
		t.Fatalf("batch jobs = %v, want [graph]", got)
	}
}

func TestBatchEligibleKeepsGraphOnSharedLifecycle(t *testing.T) {
	for _, jobs := range [][]string{{"graph"}, {"graph", "web"}, {"web", "notif"}} {
		if !batchEligible(jobs) {
			t.Fatalf("batchEligible(%v) = false, want true", jobs)
		}
	}
	if batchEligible([]string{"graph", "fabric"}) {
		t.Fatal("Fabric must not enter the normal batch lifecycle")
	}
}

func TestAuthHelperRenewalArgsUseGraphBatchLifecycle(t *testing.T) {
	if got, want := authHelperRenewalArgs("graph"), []string{"--events", "--renew-batch", "graph"}; !sameStrings(got, want) {
		t.Fatalf("graph renewal args = %v, want %v", got, want)
	}
	if got, want := authHelperRenewalArgs("web"), []string{"--renew", "web", "--headless"}; !sameStrings(got, want) {
		t.Fatalf("web renewal args = %v, want %v", got, want)
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRenewalTimeout(t *testing.T) {
	if got := renewalTimeout("edu"); got != 180*time.Second {
		t.Errorf("EDU timeout = %s, want 3m", got)
	}
	if got := renewalTimeout("web"); got != 90*time.Second {
		t.Errorf("web timeout = %s, want 90s", got)
	}
	if got := renewalTimeout("graph"); got != 0 {
		t.Errorf("graph timeout = %s, want no automatic timeout", got)
	}
}

func TestTokenDisplayName(t *testing.T) {
	tests := map[string]string{
		"graph":  "MS_GRAPH_TOKEN",
		"web":    "TEAMS_WEB_TOKEN",
		"notif":  "TEAMS_NOTIF_TOKEN",
		"edu":    "EDU_TOKEN",
		"fabric": "TEAMS_FABRIC_TOKEN",
	}
	for tokenType, want := range tests {
		if got := tokenDisplayName(tokenType); got != want {
			t.Errorf("tokenDisplayName(%q) = %q, want %q", tokenType, got, want)
		}
	}
}

func TestQueueTokenRenewalSkipsFailedTokens(t *testing.T) {
	m := Model{
		tokenRenewFailures: []string{"edu", "notif"},
	}
	cmd := queueTokenRenewal(&m, "edu")
	if cmd != nil {
		t.Errorf("expected queueTokenRenewal to return nil for failed token, got non-nil cmd")
	}
	if len(m.tokenRenewalQueue) != 0 {
		t.Errorf("expected queue to remain empty, got %v", m.tokenRenewalQueue)
	}
}

func TestRemoveString(t *testing.T) {
	vals := []string{"edu", "notif", "web"}
	got := removeString(vals, "notif")
	want := []string{"edu", "web"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("removeString(%v, 'notif') = %v, want %v", vals, got, want)
	}
}
