package ui

import (
	"testing"
	"time"

	"github.com/agmonetti/lazyteams/internal/graph"
)

func TestRetrySelfIDDue(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		m    Model
		want bool
	}{
		{
			name: "selfID already set",
			m:    Model{selfID: "u1"},
			want: false,
		},
		{
			name: "missing selfID, due",
			m:    Model{},
			want: true,
		},
		{
			name: "missing selfID but rate limited",
			m:    Model{selfIDRetryUntil: now.Add(time.Minute)},
			want: false,
		},
		{
			name: "retry budget exhausted",
			m:    Model{selfIDRetryCount: maxSelfRetries},
			want: false,
		},
		{
			name: "token renewal in progress",
			m:    Model{tokenRenewing: true},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.retrySelfIDDue(now); got != tt.want {
				t.Errorf("retrySelfIDDue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRetryTeamsDue(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		m    Model
		want bool
	}{
		{
			name: "teams already loaded (even if empty account)",
			m:    Model{teamsLoaded: true},
			want: false,
		},
		{
			name: "teams not loaded, due",
			m:    Model{},
			want: true,
		},
		{
			name: "teams not loaded but rate limited",
			m:    Model{teamsRetryUntil: now.Add(time.Minute)},
			want: false,
		},
		{
			name: "retry budget exhausted",
			m:    Model{teamsRetryCount: maxSelfRetries},
			want: false,
		},
		{
			name: "token renewal in progress",
			m:    Model{tokenRenewing: true},
			want: false,
		},
		{
			name: "global loading in progress",
			m:    Model{loading: true},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.m.retryTeamsDue(now); got != tt.want {
				t.Errorf("retryTeamsDue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTickSelfHealingArmsCommands(t *testing.T) {
	m := Model{}
	updated, cmd := m.handleTickMsg(tickMsg{})
	got := updated.(Model)
	if cmd == nil {
		t.Fatal("expected a non-nil batch cmd from the tick")
	}
	if got.selfIDRetryCount != 1 {
		t.Errorf("selfIDRetryCount = %d, want 1", got.selfIDRetryCount)
	}
	if got.teamsRetryCount != 1 {
		t.Errorf("teamsRetryCount = %d, want 1", got.teamsRetryCount)
	}
	if got.selfIDRetryUntil.IsZero() || got.teamsRetryUntil.IsZero() {
		t.Error("retry deadlines must be set when arming retries")
	}
}

func TestTickSelfHealingSkipsWhenLoaded(t *testing.T) {
	m := Model{selfID: "u1", teamsLoaded: true, teams: []graph.Team{{ID: "t1", DisplayName: "T"}}}
	updated, _ := m.handleTickMsg(tickMsg{})
	got := updated.(Model)
	if got.selfIDRetryCount != 0 || got.teamsRetryCount != 0 {
		t.Errorf("retry counters must stay 0 when data is loaded: selfID=%d teams=%d", got.selfIDRetryCount, got.teamsRetryCount)
	}
	// Rate limit must not be armed either.
	if !got.selfIDRetryUntil.IsZero() || !got.teamsRetryUntil.IsZero() {
		t.Error("retry deadlines must not be set when data is loaded")
	}
}

func TestTickSelfHealingRateLimited(t *testing.T) {
	// Recent attempt means the tick must not arm another retry yet.
	m := Model{selfIDRetryUntil: time.Now().Add(time.Minute), teamsRetryUntil: time.Now().Add(time.Minute)}
	updated, _ := m.handleTickMsg(tickMsg{})
	got := updated.(Model)
	if got.selfIDRetryCount != 0 || got.teamsRetryCount != 0 {
		t.Errorf("rate-limited tick must not retry: selfID=%d teams=%d", got.selfIDRetryCount, got.teamsRetryCount)
	}
}
