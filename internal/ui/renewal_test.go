package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/agmonetti/lazyteams/internal/auth"
	"github.com/agmonetti/lazyteams/internal/graph"
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
