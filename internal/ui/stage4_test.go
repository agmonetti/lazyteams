package ui

import (
	"testing"

	"github.com/agmonetti/lazyteams/internal/auth"
)

func stage4Event(event, provider, state string, generation uint64) auth.AuthEvent {
	return auth.AuthEvent{
		Version: auth.EventVersion, Event: event, BatchID: "batch",
		Provider: provider, State: state, Generation: generation,
	}
}

func TestStage4BatchProtocolRejectsTerminalViolations(t *testing.T) {
	model := Model{batchRequestedProviders: map[string]bool{"graph": true}}
	if err := validateBatchEvent(&model, stage4Event("started", "batch", "running", 4)); err != nil {
		t.Fatal(err)
	}
	if err := validateBatchEvent(&model, stage4Event("completed", "batch", "completed", 4)); err != nil {
		t.Fatal(err)
	}
	if err := validateBatchEvent(&model, stage4Event("provider_failed", "edu", "unknown", 4)); err == nil {
		t.Fatal("expected event after terminal to be rejected")
	}
}

func TestStage4BatchProtocolRejectsGenerationRegression(t *testing.T) {
	model := Model{batchRequestedProviders: map[string]bool{"web": true, "notif": true}}
	if err := validateBatchEvent(&model, stage4Event("started", "batch", "running", 8)); err != nil {
		t.Fatal(err)
	}
	if err := validateBatchEvent(&model, stage4Event("credential_ready", "web", "ready", 7)); err == nil {
		t.Fatal("expected generation regression to be rejected")
	}
}

func TestStage4BatchProtocolAllowsIncidentalCookie(t *testing.T) {
	model := Model{batchRequestedProviders: map[string]bool{"web": true, "notif": true}}
	if err := validateBatchEvent(&model, stage4Event("started", "batch", "running", 1)); err != nil {
		t.Fatal(err)
	}
	if err := validateBatchEvent(&model, stage4Event("credential_ready", "cookie", "ready", 2)); err != nil {
		t.Fatalf("cookie should be allowed for web/notif batch: %v", err)
	}
}
