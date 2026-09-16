package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/agmonetti/lazyteams/internal/auth"
	"github.com/agmonetti/lazyteams/internal/graph"
	tea "github.com/charmbracelet/bubbletea"
)

func TestStage4CoreReadySurvivesEDUFailure(t *testing.T) {
	model := Model{batchStarted: true, batchID: "batch", batchGeneration: 2, capabilities: map[string]CapabilityState{CapabilityGraph: CapabilityReady}, batchRequestedProviders: map[string]bool{"graph": true, "edu": true}}
	event := stage4Event("provider_failed", "edu", "unknown", 2)
	event.ErrorCode = "acquisition_failed"
	updated, next := model.Update(authEventMsg{event: event})
	model = updated.(Model)
	if next == nil || model.capabilities[CapabilityGraph] != CapabilityReady {
		t.Fatal("EDU failure blocked the ready Graph capability")
	}
	if model.capabilities[CapabilityEDU] != CapabilityUnknown {
		t.Fatalf("EDU capability = %q, want unknown (capabilities=%v)", model.capabilities[CapabilityEDU], model.capabilities)
	}
}

func TestStage4TUICancellationSignalsAndDrainsHelper(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	configDir := filepath.Join(home, ".config", "lazyteams")
	set, err := auth.PersistTokenValues(configDir, auth.TokenValues{Graph: "graph-token"})
	if err != nil {
		t.Fatal(err)
	}

	helper := filepath.Join(t.TempDir(), "fake-auth-helper")
	started := fmt.Sprintf(`{"version":1,"event":"started","batch_id":"cancel","provider":"batch","generation":%d,"state":"running","duration_ms":0,"error_code":""}`, set.Generation)
	cancelled := fmt.Sprintf(`{"version":1,"event":"cancelled","batch_id":"cancel","provider":"batch","generation":%d,"state":"cancelled","duration_ms":1,"error_code":"cancelled"}`, set.Generation)
	script := fmt.Sprintf("#!/usr/bin/python3\nimport signal, sys, time\nprint(%q, flush=True)\ndef stop(signum, frame):\n    print(%q, flush=True)\n    sys.exit(1)\nsignal.signal(signal.SIGTERM, stop)\nsignal.signal(signal.SIGINT, stop)\nwhile True: time.sleep(10)\n", started, cancelled)
	if err := os.WriteFile(helper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	oldExecutable := authHelperExecutable
	authHelperExecutable = func() (string, error) { return helper, nil }
	t.Cleanup(func() { authHelperExecutable = oldExecutable })

	model := Model{client: graph.NewClient(set)}
	if msg := authHelperBatch(&model, []string{"graph"})(); msg == nil {
		t.Fatal("batch command returned no start message")
	} else {
		updated, next := model.Update(msg)
		model = updated.(Model)
		if next == nil || !model.batchStarted {
			t.Fatal("TUI did not consume helper started event")
		}
		updated, _ = model.handleMainSwitch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
		model = updated.(Model)
		result, ok := readBatchEventCmd(&model)().(tokenRenewedMsg)
		if !ok || !model.batchTerminal || model.batchTerminalState != "cancelled" {
			t.Fatalf("drain result = %#v (%v), terminal=%v/%q", result, result.err, model.batchTerminal, model.batchTerminalState)
		}
		updated, quit := model.Update(result)
		model = updated.(Model)
		if quit == nil || quit() != (tea.QuitMsg{}) {
			t.Fatal("TUI did not finish through quit-after-renewal")
		}
		if model.batchEventScanner.Scan() {
			t.Fatal("helper output was not drained to EOF")
		}
	}
}

func TestStage4FakeHelperPublishesBeforeCompleted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	configDir := filepath.Join(home, ".config", "lazyteams")
	set, err := auth.PersistTokenValues(configDir, auth.TokenValues{Graph: "graph-token"})
	if err != nil {
		t.Fatal(err)
	}

	helper := filepath.Join(t.TempDir(), "fake-auth-helper")
	started := fmt.Sprintf(`{"version":1,"event":"started","batch_id":"fake","provider":"batch","generation":%d,"state":"running","duration_ms":0,"error_code":""}`, set.Generation)
	ready := fmt.Sprintf(`{"version":1,"event":"credential_ready","batch_id":"fake","provider":"graph","generation":%d,"state":"ready","duration_ms":1,"error_code":""}`, set.Generation)
	completed := fmt.Sprintf(`{"version":1,"event":"completed","batch_id":"fake","provider":"batch","generation":%d,"state":"completed","duration_ms":2,"error_code":""}`, set.Generation)
	// Keep the fake alive after publishing ready, then close with a valid terminal.
	script := fmt.Sprintf("#!/bin/sh\nprintf '%s\\n'\nprintf '%s\\n'\nsleep 0.2\nprintf '%s\\n'\n", started, ready, completed)
	if err := os.WriteFile(helper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	oldExecutable := authHelperExecutable
	authHelperExecutable = func() (string, error) { return helper, nil }
	t.Cleanup(func() { authHelperExecutable = oldExecutable })

	model := Model{client: graph.NewClient(set)}
	cmd := authHelperBatch(&model, []string{"graph"})
	msg := cmd()
	updated, next := model.Update(msg)
	model = updated.(Model)
	if next == nil || !model.batchStarted {
		t.Fatal("fake helper did not publish started")
	}

	msg = next()
	updated, next = model.Update(msg)
	model = updated.(Model)
	if next == nil || model.batchReadyProvider != "graph" {
		t.Fatal("credential_ready was not received before completed")
	}

	msg = next()
	updated, next = model.Update(msg)
	model = updated.(Model)
	if model.capabilities[CapabilityGraph] != CapabilityReady {
		t.Fatalf("graph capability = %q, want ready", model.capabilities[CapabilityGraph])
	}
	if model.batchTerminal {
		t.Fatal("helper completed before the readiness reload finished")
	}
	if next == nil {
		t.Fatal("stream reader was not rearmed after readiness")
	}
}
