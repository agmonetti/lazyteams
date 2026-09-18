package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/agmonetti/lazyteams/internal/auth"
	"github.com/agmonetti/lazyteams/internal/graph"
	tea "github.com/charmbracelet/bubbletea"
)

func TestNextRenewalPlanIgnoresMissingExpiryAndChoosesSoonest(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	set := &auth.CredentialSet{Generation: 4,
		Graph: auth.Credential{Token: "graph"},
		Web:   auth.Credential{Token: "web", ExpiresAt: expires},
		Notif: auth.Credential{Token: "notif", ExpiresAt: expires.Add(-time.Minute)},
	}
	plan, ok := nextRenewalPlan(set, nil)
	if !ok || plan.provider != CapabilityNotif || plan.generation != 4 {
		t.Fatalf("unexpected plan: %+v, ok=%v", plan, ok)
	}
}

func TestRenewalTimerIgnoresObsoleteGeneration(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	m := Model{client: graph.NewClient(&auth.CredentialSet{Generation: 2,
		Graph: auth.Credential{Token: "graph", ExpiresAt: expires},
	})}
	if m.timerMatches(renewalTimerMsg{provider: CapabilityGraph, generation: 1, expiresAt: expires}) {
		t.Fatal("obsolete generation matched renewal timer")
	}
	if m.timerMatches(renewalTimerMsg{provider: CapabilityGraph, generation: 2, expiresAt: expires.Add(time.Second)}) {
		t.Fatal("changed expiry matched renewal timer")
	}
	if !m.timerMatches(renewalTimerMsg{provider: CapabilityGraph, generation: 2, expiresAt: expires}) {
		t.Fatal("current renewal timer did not match")
	}
}

func TestNextRenewalPlanSupportsGenerationZero(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	set := &auth.CredentialSet{Generation: 0, Graph: auth.Credential{Token: "graph", ExpiresAt: expires}}
	plan, ok := nextRenewalPlan(set, map[string]bool{})
	if !ok || plan.generation != 0 || plan.provider != CapabilityGraph {
		t.Fatalf("generation-zero plan = %+v, ok=%v", plan, ok)
	}
}

func TestRenewalAttemptPresenceBlocksGenerationZero(t *testing.T) {
	m := Model{client: graph.NewClient(&auth.CredentialSet{Generation: 0, Graph: auth.Credential{Token: "graph"}})}
	if queueTokenRenewal(&m, CapabilityGraph) == nil {
		t.Fatal("generation-zero renewal was not queued")
	}
	m.tokenRenewing = false
	m.tokenRenewalQueue = nil
	if queueTokenRenewal(&m, CapabilityGraph) != nil {
		t.Fatal("generation-zero renewal was queued twice")
	}
}

func TestRenewalErrorClassification(t *testing.T) {
	if !isRetryableRenewalError(fmt.Errorf("permission denied")) == false {
		t.Fatal("permission errors must not be retryable")
	}
	if !isRetryableRenewalError(fmt.Errorf("network timeout")) {
		t.Fatal("network errors must be retryable on resume")
	}
}

func TestQueueTokenRenewalAllowsNewGenerationOnly(t *testing.T) {
	m := Model{client: graph.NewClient(&auth.CredentialSet{Generation: 3, Graph: auth.Credential{Token: "graph"}})}
	if queueTokenRenewal(&m, CapabilityGraph) == nil {
		t.Fatal("first renewal was not queued")
	}
	if queueTokenRenewal(&m, CapabilityGraph) != nil {
		t.Fatal("same generation was queued twice")
	}
	m.tokenRenewing = false
	m.tokenRenewalQueue = nil
	m.client = graph.NewClient(&auth.CredentialSet{Generation: 4, Graph: auth.Credential{Token: "graph-2"}})
	if queueTokenRenewal(&m, CapabilityGraph) == nil {
		t.Fatal("new generation was not eligible for renewal")
	}
}

func TestRenewalTimerUsesAttemptGate(t *testing.T) {
	expires := time.Now().Add(time.Hour)
	set := &auth.CredentialSet{Generation: 3, Graph: auth.Credential{Token: "graph", ExpiresAt: expires}}
	m := Model{
		client:           graph.NewClient(set),
		renewalAttempted: map[string]bool{renewalAttemptKey(CapabilityGraph, 3): true},
	}
	old := startBatchRenewal
	startBatchRenewal = func(*Model, []string) tea.Cmd {
		t.Fatal("duplicate timer launched auth helper")
		return nil
	}
	t.Cleanup(func() { startBatchRenewal = old })

	updated, _ := m.Update(renewalTimerMsg{provider: CapabilityGraph, generation: 3, expiresAt: expires})
	m = updated.(Model)
	if m.tokenRenewing || len(m.tokenRenewalQueue) != 0 {
		t.Fatalf("duplicate timer queued renewal: renewing=%v queue=%v", m.tokenRenewing, m.tokenRenewalQueue)
	}
}

func TestResumeReleasesOnlyNetworkUnavailableProvider(t *testing.T) {
	graphKey := renewalAttemptKey(CapabilityGraph, 5)
	eduKey := renewalAttemptKey(CapabilityEDU, 5)
	m := Model{
		client:           graph.NewClient(&auth.CredentialSet{Generation: 5}),
		renewalAttempted: map[string]bool{graphKey: true, eduKey: true},
		renewalRetryable: map[string]bool{graphKey: true},
	}
	updated, _ := m.Update(tea.ResumeMsg{})
	m = updated.(Model)
	if m.renewalAttempted[graphKey] || m.renewalRetryable[graphKey] {
		t.Fatal("network-unavailable Graph attempt was not released on resume")
	}
	if !m.renewalAttempted[eduKey] {
		t.Fatal("non-retryable EDU attempt was released on resume")
	}
}

func TestBatchOutcomesKeepRetryabilityPerProvider(t *testing.T) {
	graphKey := renewalAttemptKey(CapabilityGraph, 5)
	eduKey := renewalAttemptKey(CapabilityEDU, 5)
	m := Model{
		client:                   graph.NewClient(&auth.CredentialSet{Generation: 5}),
		capabilities:             make(map[string]CapabilityState),
		renewalBatch:             true,
		batchStarted:             true,
		batchID:                  "batch",
		batchGeneration:          5,
		batchRequestedProviders:  map[string]bool{CapabilityGraph: true, CapabilityEDU: true},
		renewalAttempted:         map[string]bool{graphKey: true, eduKey: true},
		renewalAttemptGeneration: map[string]uint64{CapabilityGraph: 5, CapabilityEDU: 5},
	}

	network := stage4Event("provider_failed", CapabilityGraph, "network_unavailable", 5)
	network.ErrorCode = "network_unavailable"
	updated, _ := m.Update(authEventMsg{event: network})
	m = updated.(Model)

	permission := stage4Event("provider_failed", CapabilityEDU, "permission_denied", 5)
	permission.ErrorCode = "permission_denied"
	updated, _ = m.Update(authEventMsg{event: permission})
	m = updated.(Model)

	updated, _ = m.Update(tokenRenewedMsg{tokenType: "batch", err: fmt.Errorf("auth batch ended with partial_failure: network timeout")})
	m = updated.(Model)
	if !m.renewalRetryable[graphKey] {
		t.Fatal("network-unavailable Graph outcome was not marked retryable")
	}
	if m.renewalRetryable[eduKey] {
		t.Fatal("aggregate batch error made permission-denied EDU retryable")
	}
}

func TestOnly401QueuesReactiveRenewal(t *testing.T) {
	old := startBatchRenewal
	launches := 0
	startBatchRenewal = func(*Model, []string) tea.Cmd {
		launches++
		return func() tea.Msg { return nil }
	}
	t.Cleanup(func() { startBatchRenewal = old })

	for _, test := range []struct {
		err       error
		wantQueue bool
	}{
		{fmt.Errorf("MS_GRAPH request failed: 401"), true},
		{fmt.Errorf("request failed: 403 permission denied"), false},
		{fmt.Errorf("network unavailable"), false},
		{fmt.Errorf("request timeout"), false},
	} {
		m := Model{client: graph.NewClient(&auth.CredentialSet{Generation: 1, Graph: auth.Credential{Token: "graph"}})}
		before := launches
		updated, _ := m.Update(errMsg{err: test.err})
		m = updated.(Model)
		if got := launches > before; got != test.wantQueue {
			t.Errorf("error %q launched helper = %v, want %v", test.err, got, test.wantQueue)
		}
	}
}

func TestCredentialReadyPreservesActiveView(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	set, err := auth.PersistTokenValues(home+"/.config/lazyteams", auth.TokenValues{Graph: "graph"})
	if err != nil {
		t.Fatal(err)
	}
	m := New(graph.NewClient(set), "user", false)
	m.workspace = WorkspaceDMs
	m.selectedChat = 2
	m.input.SetValue("draft")
	m.batchStarted = true
	m.batchID = "batch"
	m.batchGeneration = set.Generation
	m.batchRequestedProviders = map[string]bool{CapabilityGraph: true}

	ready := stage4Event("credential_ready", CapabilityGraph, "ready", set.Generation)
	updated, cmd := m.Update(authEventMsg{event: ready})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("credential_ready did not request snapshot reload")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.capabilities[CapabilityGraph] != CapabilityReady {
		t.Fatalf("Graph capability = %q, want ready", m.capabilities[CapabilityGraph])
	}
	if m.workspace != WorkspaceDMs || m.selectedChat != 2 || m.input.Value() != "draft" {
		t.Fatalf("active view changed after credential reload: workspace=%v chat=%d draft=%q", m.workspace, m.selectedChat, m.input.Value())
	}
}
