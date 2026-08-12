package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func presenceModel() Model {
	return Model{
		selfID:   "me",
		presence: make(map[string]string),
	}
}

// applyPresenceRefresh runs the refresh handler and returns the updated model.
func applyPresenceRefresh(m Model, presences map[string]string) Model {
	updated, _ := m.handlePresenceResultMsg(presenceTickResultMsg{presences: presences})
	return updated.(tea.Model).(Model)
}

func TestPresenceWindowKeepsOptimisticValue(t *testing.T) {
	m := presenceModel()
	m.presence["me"] = "Busy" // optimistic value set on confirm
	m.preferredPresence = "Busy"
	m.preferredPresenceSetAt = time.Now().Add(-time.Minute) // inside the 3 min window

	// A slow Graph refresh returns the stale effective presence (Away).
	got := applyPresenceRefresh(m, map[string]string{"me": "Away"})

	if got.presence["me"] != "Busy" {
		t.Errorf("presence[me] = %q, want %q (refresh must not override within the window)", got.presence["me"], "Busy")
	}
}

func TestPresenceWindowAppliesOtherUsersImmediately(t *testing.T) {
	m := presenceModel()
	m.presence["me"] = "Busy"
	m.preferredPresence = "Busy"
	m.preferredPresenceSetAt = time.Now() // inside the window

	got := applyPresenceRefresh(m, map[string]string{"me": "Away", "someone-else": "Available"})

	if got.presence["someone-else"] != "Available" {
		t.Errorf("presence[someone-else] = %q, want %q (other users must update within the window)", got.presence["someone-else"], "Available")
	}
}

func TestPresenceWindowExpires(t *testing.T) {
	m := presenceModel()
	m.presence["me"] = "Busy"
	m.preferredPresence = "Busy"
	m.preferredPresenceSetAt = time.Now().Add(-10 * time.Minute) // past the window

	got := applyPresenceRefresh(m, map[string]string{"me": "Away"})

	if got.presence["me"] != "Away" {
		t.Errorf("presence[me] = %q, want %q (refresh must apply once the window passes)", got.presence["me"], "Away")
	}
}

func TestPresenceWindowWithoutPreferredApplies(t *testing.T) {
	m := presenceModel()
	m.preferredPresence = "" // e.g. after "Reset (Automatic)"

	got := applyPresenceRefresh(m, map[string]string{"me": "Away"})

	if got.presence["me"] != "Away" {
		t.Errorf("presence[me] = %q, want %q (no preferred presence: refresh must apply)", got.presence["me"], "Away")
	}
}

func TestPresenceWindowConfirmationKeepsOptimisticValue(t *testing.T) {
	m := presenceModel()
	m.presence["me"] = "Busy"
	m.preferredPresence = "Busy"
	m.preferredPresenceSetAt = time.Now().Add(-time.Minute)

	// Graph confirms the preferred value quickly: refresh returns Busy.
	got := applyPresenceRefresh(m, map[string]string{"me": "Busy"})

	if got.presence["me"] != "Busy" {
		t.Errorf("presence[me] = %q, want %q", got.presence["me"], "Busy")
	}
}
