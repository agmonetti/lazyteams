package ui

import (
	"context"
	"os/exec"
	"sync"
	"time"

	"github.com/agmonetti/lazyteams/internal/helpers"
)

// Model values are copied by Bubble Tea. The owner is shared with the initial
// model so its deferred Close also covers terminal errors and external signals.
type renewalOwner struct {
	mu      sync.Mutex
	closed  bool
	process *helpers.AuthProcess
}

func (o *renewalOwner) start(cmd *exec.Cmd, timeout time.Duration) (*helpers.AuthProcess, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil, context.Canceled
	}
	process, err := helpers.StartAuthProcess(cmd, timeout)
	if err == nil {
		o.process = process
	}
	return process, err
}

// Close stops any authentication helper before the terminal application exits.
// It is safe to call on the initial model, on updated copies, or more than once.
func (m Model) Close() {
	if m.renewal == nil {
		return
	}
	m.renewal.mu.Lock()
	m.renewal.closed = true
	process := m.renewal.process
	m.renewal.mu.Unlock()
	if process != nil {
		process.Close()
	}
}
