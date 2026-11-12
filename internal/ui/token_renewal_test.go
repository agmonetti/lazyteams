package ui

import (
	"context"
	"errors"
	"io"
	"net/http"
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
	t.Cleanup(func() { startBatchRenewal = old })
	var helperLaunches atomic.Int32
	startBatchRenewal = func(_ *Model, _ []string) tea.Cmd {
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

func TestEDUTimeoutStopsRenewingAndShowsAssignmentsError(t *testing.T) {
	m := Model{
		tokenRenewing:             true,
		tokenRenewingType:         "edu",
		tokenRenewalQueue:         []string{"edu"},
		tokenRenewalSpinnerActive: true,
		tokenRenewFailures: []string{
			"graph", "web", "notif",
		},
	}
	updated, _ := m.Update(tokenRenewedMsg{tokenType: "edu", err: context.DeadlineExceeded})
	m = updated.(Model)
	if m.tokenRenewing || m.tokenRenewingType != "" || len(m.tokenRenewalQueue) != 0 {
		t.Fatal("timed-out renewal still appears active")
	}
	if !containsString(m.tokenRenewFailures, "edu") || !errors.Is(m.assignErr, context.DeadlineExceeded) {
		t.Fatalf("EDU timeout was not retained: failures=%v, assignments=%v", m.tokenRenewFailures, m.assignErr)
	}
	view := renderAssignList(m)
	if !strings.Contains(view, "Assignments Error") || strings.Contains(view, "No assignments") {
		t.Fatalf("timeout rendered as an empty successful load: %s", view)
	}
	updated, cmd := m.Update(tokenRenewalTickMsg{})
	if updated.(Model).tokenRenewalSpinnerActive || cmd != nil {
		t.Fatal("spinner continued after renewal timeout")
	}
}

func TestEDUHelperSuccessRequiresAssignmentsAcceptance(t *testing.T) {
	m := Model{
		tokenRenewing:      true,
		tokenRenewingType:  "edu",
		tokenRenewalQueue:  []string{"edu"},
		tokenRenewFailures: []string{"graph", "web", "notif"},
	}
	updated, _ := m.Update(tokenRenewedMsg{tokenType: "edu"})
	m = updated.(Model)
	if !m.eduRenewalPending || m.assignLoaded {
		t.Fatal("helper exit was treated as Assignments success")
	}
	if view := renderAssignList(m); !strings.Contains(view, "Validating") || strings.Contains(view, "No assignments") {
		t.Fatalf("missing validation state: %s", view)
	}
	// An earlier request must not decide whether the renewed token works.
	updated, cmd := m.Update(assignmentsErrMsg{err: errors.New("old request: 401")})
	m = updated.(Model)
	if !m.eduRenewalPending || cmd != nil {
		t.Fatal("stale request changed renewal validation")
	}
	apiErr := errors.New("Assignments API: 401 unauthorized")
	updated, cmd = m.Update(assignmentsErrMsg{err: apiErr, afterRenewal: true})
	m = updated.(Model)
	if m.eduRenewalPending || m.tokenRenewing || cmd != nil || !containsString(m.tokenRenewFailures, "edu") {
		t.Fatal("rejected renewed token started another renewal")
	}
	if !errors.Is(m.assignErr, apiErr) || !strings.Contains(renderAssignList(m), "401 unauthorized") {
		t.Fatal("Assignments API rejection was hidden")
	}
	_, cmd = m.Update(assignmentsErrMsg{err: apiErr})
	if cmd != nil {
		t.Fatal("repeated API rejection restarted a failed renewal")
	}
}

func TestEDUAssignmentsAcceptanceClearsPendingFailure(t *testing.T) {
	m := Model{
		eduRenewalPending:  true,
		tokenRenewFailures: []string{"edu"},
		tokenRenewErr:      "previous attempt failed",
		assignFilter:       FilterUpcoming,
	}
	updated, cmd := m.Update(assignmentsMsg{
		items:        []graph.Assignment{{ID: "assignment", DisplayName: "Coursework", SubmissionStatus: "working"}},
		afterRenewal: true,
	})
	m = updated.(Model)
	if m.eduRenewalPending || !m.assignLoaded || m.assignErr != nil || cmd != nil {
		t.Fatal("accepted Assignments response did not finish validation")
	}
	if containsString(m.tokenRenewFailures, "edu") || m.tokenRenewErr != "" {
		t.Fatal("successful Assignments validation retained a renewal failure")
	}
	if !strings.Contains(renderAssignList(m), "Coursework") {
		t.Fatal("accepted Assignments were not rendered")
	}
}

func TestEDUReloadFailureIsAnAssignmentsError(t *testing.T) {
	m := Model{
		eduRenewalPending: true,
		tokenRenewFailures: []string{
			"graph", "web", "notif",
		},
	}
	reloadErr := errors.New("cannot reload credentials")
	updated, _ := m.Update(tokensReloadedMsg{tokenType: "edu", err: reloadErr})
	m = updated.(Model)
	if m.eduRenewalPending || !m.assignLoaded || !errors.Is(m.assignErr, reloadErr) {
		t.Fatal("reload failure left Assignments pending or hid its error")
	}
}

func TestAssignmentsUnauthorizedRemainsVisibleWhenRenewalFailed(t *testing.T) {
	m := Model{tokenRenewFailures: []string{"edu"}}
	apiErr := errors.New("Assignments API: 401 unauthorized")
	updated, cmd := m.Update(assignmentsErrMsg{err: apiErr})
	m = updated.(Model)
	if cmd != nil || !m.assignLoaded || !errors.Is(m.assignErr, apiErr) {
		t.Fatal("unauthorized Assignments response was discarded")
	}
	if strings.Contains(renderAssignList(m), "No assignments") {
		t.Fatal("unauthorized response was presented as an empty list")
	}
}

func TestAssignmentsResponsesBeforeRenewalCannotOverwriteItsResult(t *testing.T) {
	apiErr := errors.New("renewed token rejected")
	m := Model{
		assignGeneration:   2,
		assignErr:          apiErr,
		assignLoaded:       true,
		tokenRenewFailures: []string{"edu"},
	}
	updated, cmd := m.Update(assignmentsMsg{generation: 1})
	m = updated.(Model)
	if cmd != nil || !errors.Is(m.assignErr, apiErr) {
		t.Fatal("stale empty response erased the renewal failure")
	}
	updated, cmd = m.Update(assignmentsErrMsg{err: errors.New("stale error"), generation: 1})
	if cmd != nil || !errors.Is(updated.(Model).assignErr, apiErr) {
		t.Fatal("stale error overwrote the current Assignments result")
	}
}

func TestBatchEndRenewsProvidersQueuedMeanwhile(t *testing.T) {
	old := startBatchRenewal
	t.Cleanup(func() { startBatchRenewal = old })
	var started [][]string
	startBatchRenewal = func(_ *Model, jobs []string) tea.Cmd {
		started = append(started, append([]string(nil), jobs...))
		return func() tea.Msg { return nil }
	}
	m := Model{tokenRenewalQueue: []string{"web"}}
	if cmd := startNextTokenRenewal(&m); cmd == nil {
		t.Fatal("expected first batch")
	}
	// Graph expires while the web batch is still running.
	enqueueTokenRenewal(&m, "graph")
	if cmd := startNextTokenRenewal(&m); cmd != nil {
		t.Fatal("second helper started while one is running")
	}
	updated, cmd := m.Update(tokenRenewedMsg{tokenType: "batch"})
	m = updated.(Model)
	if cmd == nil || len(started) != 2 || !sameStrings(started[1], []string{"graph"}) {
		t.Fatalf("batches = %v, want [web] then [graph]", started)
	}
	updated, _ = m.Update(tokenRenewedMsg{tokenType: "batch"})
	if m = updated.(Model); m.tokenRenewing || len(m.tokenRenewalQueue) != 0 {
		t.Fatalf("renewal still pending: renewing=%v queue=%v", m.tokenRenewing, m.tokenRenewalQueue)
	}
}
