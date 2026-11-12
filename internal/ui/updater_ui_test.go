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
	updated, quitCmd := m.Update(result)
	m = updated.(Model)
	if called != 1 || m.updateStage != "done" || m.latestVersion != "" || quitCmd == nil {
		t.Fatalf("unexpected completion: stage=%q calls=%d cmd=%v", m.updateStage, called, quitCmd)
	}
	if _, ok := quitCmd().(tea.QuitMsg); !ok {
		t.Fatalf("update completion command = %T, want QuitMsg", quitCmd())
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

func TestUpdateSuccessClosesResourcesAndReturnsQuit(t *testing.T) {
	renewal := &renewalOwner{}
	m := Model{latestVersion: "v1.2.3", updateStage: "installing", renewal: renewal}

	updated, cmd := m.Update(updateInstallMsg{err: nil})
	got := updated.(Model)
	if got.updateStage != "done" || got.latestVersion != "" {
		t.Fatalf("expected done state, got stage=%q version=%q", got.updateStage, got.latestVersion)
	}
	if cmd == nil {
		t.Fatal("expected quit command on successful update, got nil")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("command = %T, want tea.QuitMsg", cmd())
	}

	renewal.mu.Lock()
	closed := renewal.closed
	renewal.mu.Unlock()
	if !closed {
		t.Fatal("resources were not closed on successful update quit")
	}
}

func TestUpdateErrorsAndCancellationDoNotQuit(t *testing.T) {
	// 1. User cancels with 'n'
	m := Model{latestVersion: "v1.2.3", updateStage: "confirm"}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if updated.(Model).updateStage != "" || cmd != nil {
		t.Fatalf("cancel 'n' must reset stage and not quit: stage=%q cmd=%v", updated.(Model).updateStage, cmd)
	}

	// 2. User cancels with 'esc'
	m = Model{latestVersion: "v1.2.3", updateStage: "confirm"}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.(Model).updateStage != "" || cmd != nil {
		t.Fatalf("cancel 'esc' must reset stage and not quit: stage=%q cmd=%v", updated.(Model).updateStage, cmd)
	}

	// 3. Various installation errors (download, checksum, rollback, permissions)
	errorCases := []string{
		"download failed: connection reset",
		"sha256 mismatch for lazyteams",
		"sha256 mismatch for lazyteams-auth",
		"install failed: permission denied; rollback succeeded",
	}
	for _, errMsg := range errorCases {
		m = Model{latestVersion: "v1.2.3", updateStage: "installing"}
		updated, cmd = m.Update(updateInstallMsg{err: errors.New(errMsg)})
		got := updated.(Model)
		if got.updateStage != "error" || cmd != nil {
			t.Fatalf("error %q must set error stage and not quit: stage=%q cmd=%v", errMsg, got.updateStage, cmd)
		}
		if got.latestVersion != "v1.2.3" {
			t.Fatalf("error %q must retain latestVersion for retry", errMsg)
		}
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
