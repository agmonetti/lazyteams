package ui

import (
	"testing"
	"time"
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

func TestAllActiveTokensFresh(t *testing.T) {
	if m := (Model{tokenRenewing: false, tokenRenewalQueue: nil, tokenRenewFailures: nil}); !m.allActiveTokensFresh() {
		t.Error("idle model with empty queue and no failures should be fresh")
	}
	if m := (Model{tokenRenewing: true, tokenRenewalQueue: nil, tokenRenewFailures: nil}); m.allActiveTokensFresh() {
		t.Error("a renewal in progress is not fresh")
	}
	if m := (Model{tokenRenewing: false, tokenRenewalQueue: []string{"graph"}, tokenRenewFailures: nil}); m.allActiveTokensFresh() {
		t.Error("a non-empty queue is not fresh")
	}
	if m := (Model{tokenRenewing: false, tokenRenewalQueue: nil, tokenRenewFailures: []string{"graph"}}); m.allActiveTokensFresh() {
		t.Error("prior renewal failures are not fresh")
	}
}
