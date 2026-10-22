package helpers

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

const authProcessGrace = 2 * time.Second
const authProcessWaitDelay = 250 * time.Millisecond

// AuthProcess owns a helper's Unix process group or Windows Job Object.
// Wait and Close may be called concurrently and return after helper cleanup.
// Unix children that create a separate group rely on the helper's graceful
// shutdown or their driver connection closing; group termination cannot reach them.
type AuthProcess struct {
	cancel chan struct{}
	done   chan struct{}
	once   sync.Once
	err    error
}

// StartAuthProcess isolates cmd from other browser sessions and starts its
// timeout immediately, independently of when the UI executes its wait command.
// A zero timeout allows interactive login until Close is called. Nil output
// streams use the OS null device, not pipes that a Playwright child can retain.
func StartAuthProcess(cmd *exec.Cmd, timeout time.Duration) (*AuthProcess, error) {
	return startAuthProcess(cmd, timeout, authProcessGrace)
}

func startAuthProcess(cmd *exec.Cmd, timeout, grace time.Duration) (*AuthProcess, error) {
	tree, err := prepareAuthProcess(cmd)
	if err != nil {
		return nil, err
	}
	cmd.WaitDelay = authProcessWaitDelay
	started := time.Now()
	if err := cmd.Start(); err != nil {
		tree.close()
		return nil, err
	}
	if err := tree.attach(cmd.Process); err != nil {
		_ = cmd.Process.Kill()
		tree.close()
		_ = cmd.Wait()
		return nil, err
	}
	p := &AuthProcess{cancel: make(chan struct{}), done: make(chan struct{})}
	go p.run(cmd, tree, timeout, grace, started)
	return p, nil
}

func (p *AuthProcess) run(cmd *exec.Cmd, tree *authProcessTree, timeout, grace time.Duration, started time.Time) {
	defer close(p.done)
	defer tree.close()
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	var deadline <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(time.Until(started.Add(timeout)))
		defer timer.Stop()
		deadline = timer.C
	}
	select {
	case p.err = <-waitDone:
		// Even successful helpers must not leave a Playwright driver behind.
		tree.kill()
	case <-deadline:
		// The deadline is a hard bound, not the start of another unbounded
		// graceful shutdown. WaitDelay also bounds inherited output pipes.
		tree.kill()
		<-waitDone
		p.err = fmt.Errorf("token renewal timed out after %s: %w", timeout, context.DeadlineExceeded)
	case <-p.cancel:
		tree.signal(cmd.Process)
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-waitDone:
			tree.kill()
		case <-timer.C:
			tree.kill()
			<-waitDone
		}
		p.err = context.Canceled
	}
}

// Wait returns the helper result after cleanup, including deadline errors.
func (p *AuthProcess) Wait() error {
	<-p.done
	return p.err
}

// Close requests graceful shutdown, then terminates this helper's process group
// or Job Object if it does not exit promptly. Calling it repeatedly is safe.
func (p *AuthProcess) Close() {
	p.once.Do(func() { close(p.cancel) })
	<-p.done
}
