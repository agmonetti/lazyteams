package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/agmonetti/lazyteams/internal/version"

	tea "github.com/charmbracelet/bubbletea"
)

func TestUpdateKeyConfirmsAndReportsResult(t *testing.T) {
	original := runUIUpdate
	t.Cleanup(func() { runUIUpdate = original })
	called := 0
	runUIUpdate = func(tag string) error {
		called++
		if tag != "v9.9.9" {
			t.Errorf("installed tag %q", tag)
		}
		return nil
	}

	m := Model{latestVersion: "v9.9.9", width: 90, height: 25}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = updated.(Model)
	if m.updateStage != "confirm" || called != 0 || !strings.Contains(m.View(), "both lazyteams and lazyteams-auth") {
		t.Fatalf("update did not require visible confirmation: stage=%q calls=%d view=%q", m.updateStage, called, m.View())
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.updateStage != "installing" || cmd == nil || called != 0 {
		t.Fatalf("installation not deferred: stage=%q calls=%d", m.updateStage, called)
	}
	updated, quit := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(Model)
	if m.updateStage != "installing" || quit != nil {
		t.Fatal("Ctrl+C must not interrupt a paired installation")
	}
	result := cmd()
	updated, _ = m.Update(result)
	m = updated.(Model)
	if called != 1 || m.updateStage != "done" || m.latestVersion != "" || !strings.Contains(m.View(), "Restart LazyTeams") {
		t.Fatalf("unexpected completion: stage=%q calls=%d view=%q", m.updateStage, called, m.View())
	}
}

func TestUpdateCancelAndFailureKeepHint(t *testing.T) {
	original := runUIUpdate
	t.Cleanup(func() { runUIUpdate = original })
	runUIUpdate = func(string) error { return errors.New("cannot replace helper: permission denied") }
	m := Model{latestVersion: "v9.9.9", width: 90, height: 25}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.updateStage != "" {
		t.Fatal("cancellation did not restore workspace")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = updated.(Model)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.updateStage != "error" || m.latestVersion != "v9.9.9" || !strings.Contains(m.View(), "permission denied") {
		t.Fatalf("failure not shown or hint lost: stage=%q view=%q", m.updateStage, m.View())
	}
}

func TestPendingUpdateQuitsWithoutClaimingSuccess(t *testing.T) {
	m := Model{updateStage: "installing", latestVersion: "v9.9.9"}
	updated, cmd := m.Update(updateInstallMsg{err: version.ErrUpdatePending})
	got := updated.(Model)
	if got.updateStage != "installing" || got.latestVersion != "v9.9.9" || cmd == nil {
		t.Fatal("pending Windows update must exit for the stager, not claim success")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("pending update command = %T, want QuitMsg", cmd())
	}
	restarted := Model{width: 90, height: 25}.WithUpdateResult(nil)
	if !strings.Contains(restarted.View(), "new release is running") {
		t.Fatal("post-restart confirmation missing")
	}
}
