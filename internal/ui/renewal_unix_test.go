//go:build !windows

package ui

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestModelCloseOwnsRenewalAcrossModelCopies(t *testing.T) {
	initial := Model{renewal: &renewalOwner{}}
	updated := initial
	process, err := updated.renewal.start(exec.Command("sleep", "30"), 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(initial.Close)
	closed := make(chan struct{})
	go func() {
		initial.Close()
		updated.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(4 * time.Second):
		t.Fatal("closing the initial model did not stop the updated model's helper")
	}
	if err := process.Wait(); !errors.Is(err, context.Canceled) {
		t.Fatalf("helper result = %v, want canceled", err)
	}
	late := exec.Command("sleep", "30")
	if _, err := updated.renewal.start(late, 0); !errors.Is(err, context.Canceled) || late.Process != nil {
		t.Fatalf("renewal started after application shutdown: %v", err)
	}
}

func TestQuitStopsRenewalBeforeReturningQuit(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'q'}},
		{Type: tea.KeyCtrlC},
	} {
		t.Run(key.String(), func(t *testing.T) {
			m := Model{renewal: &renewalOwner{}}
			if key.Type == tea.KeyCtrlC {
				// Ctrl+C must bypass editor and popup key capture.
				m.isTyping = true
				m.showHelp = true
			}
			process, err := m.renewal.start(exec.Command("sleep", "30"), 0)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(m.Close)
			_, cmd := m.Update(key)
			if cmd == nil {
				t.Fatal("quit key did not return a command")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatal("quit key did not return QuitMsg")
			}
			if err := process.Wait(); !errors.Is(err, context.Canceled) {
				t.Fatalf("quit left renewal running: %v", err)
			}
		})
	}
}
