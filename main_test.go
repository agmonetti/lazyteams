package main

import (
	"errors"
	"testing"

	"github.com/agmonetti/lazyteams/internal/ui"
)

func TestHandleProgramExitRestartsWhenRequested(t *testing.T) {
	origRestart := runRestart
	t.Cleanup(func() { runRestart = origRestart })

	var restartedExe string
	var restartedArgs []string
	runRestart = func(exe string, args []string) error {
		restartedExe = exe
		restartedArgs = args
		return nil
	}

	model := ui.Model{}.WithRestart(true)
	if err := handleProgramExit(model, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if restartedExe == "" || len(restartedArgs) == 0 {
		t.Fatal("restart was not triggered")
	}
	if restartedArgs[0] != restartedExe {
		t.Fatalf("argv[0] = %q, want binary path %q", restartedArgs[0], restartedExe)
	}
}

func TestHandleProgramExitDoesNotRestartWhenNotRequested(t *testing.T) {
	origRestart := runRestart
	t.Cleanup(func() { runRestart = origRestart })

	called := false
	runRestart = func(string, []string) error {
		called = true
		return nil
	}

	model := ui.Model{}.WithRestart(false)
	if err := handleProgramExit(model, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Fatal("restart was triggered when not requested")
	}
}

func TestHandleProgramExitPropagatesRunError(t *testing.T) {
	expected := errors.New("terminal error")
	err := handleProgramExit(nil, expected)
	if !errors.Is(err, expected) {
		t.Fatalf("handleProgramExit = %v, want %v", err, expected)
	}
}
