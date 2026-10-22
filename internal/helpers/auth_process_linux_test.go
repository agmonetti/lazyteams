package helpers

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func authProcessChild(t *testing.T, script string, timeout time.Duration, pipes bool) (*AuthProcess, int) {
	t.Helper()
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.Command("sh", "-c", script, "auth-process-test", pidPath)
	if pipes {
		// This reproduces the old non-debug UI output configuration. The
		// background child inherits these pipes after the shell exits.
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
	}
	p, err := startAuthProcess(cmd, timeout, 30*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidPath)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err == nil {
				return p, pid
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("helper did not publish its child PID")
	return nil, 0
}

func assertAuthChildStopped(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		if os.IsNotExist(err) {
			return
		}
		// A killed grandchild can briefly await reaping by the host init.
		if err == nil {
			end := strings.LastIndexByte(string(stat), ')')
			if end >= 0 && len(stat) > end+2 && stat[end+2] == 'Z' {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("owned child %d is still running", pid)
}

func waitAuthProcess(t *testing.T, p *AuthProcess) error {
	t.Helper()
	select {
	case <-p.done:
		return p.Wait()
	case <-time.After(3 * time.Second):
		t.Fatal("auth process did not finish within its bounded wait")
		return nil
	}
}

func TestAuthProcessDeadlineStopsTreeWithoutWaitingForUI(t *testing.T) {
	// An unrelated process must survive even while the owned helper ignores
	// SIGTERM and its descendant retains stdout/stderr.
	unrelated := exec.Command("sleep", "30")
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = unrelated.Process.Kill()
		_ = unrelated.Wait()
	})
	p, child := authProcessChild(t, `trap '' TERM; sleep 30 & echo $! > "$1"; wait`, 500*time.Millisecond, true)
	if err := waitAuthProcess(t, p); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait error = %v, want deadline exceeded", err)
	}
	assertAuthChildStopped(t, child)
	if err := unrelated.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("unrelated process was stopped: %v", err)
	}
}

func TestAuthProcessWaitDoesNotHangOnInheritedPipes(t *testing.T) {
	p, child := authProcessChild(t, `sleep 30 & echo $! > "$1"`, 0, true)
	if err := waitAuthProcess(t, p); !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("Wait error = %v, want inherited-pipe wait delay", err)
	}
	assertAuthChildStopped(t, child)
}

func TestAuthProcessSuccessfulExitStillStopsDescendants(t *testing.T) {
	p, child := authProcessChild(t, `sleep 30 & echo $! > "$1"`, 0, false)
	if err := waitAuthProcess(t, p); err != nil {
		t.Fatal(err)
	}
	assertAuthChildStopped(t, child)
}

func TestAuthProcessCloseStopsUncooperativeTree(t *testing.T) {
	p, child := authProcessChild(t, `trap '' TERM; sleep 30 & echo $! > "$1"; wait`, 0, true)
	closed := make(chan struct{})
	go func() {
		p.Close()
		p.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close hung on an uncooperative helper")
	}
	if err := p.Wait(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait error = %v, want cancellation", err)
	}
	assertAuthChildStopped(t, child)
}
