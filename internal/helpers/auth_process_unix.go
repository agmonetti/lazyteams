//go:build !windows

package helpers

import (
	"os"
	"os/exec"
	"syscall"
)

type authProcessTree struct {
	pid int
}

func prepareAuthProcess(cmd *exec.Cmd) (*authProcessTree, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Pgid = 0
	return &authProcessTree{}, nil
}

func (t *authProcessTree) attach(proc *os.Process) error {
	t.pid = proc.Pid
	return nil
}

func (t *authProcessTree) signal(proc *os.Process) {
	_ = proc.Signal(syscall.SIGTERM)
}

func (t *authProcessTree) kill() {
	if t.pid > 0 {
		// The group was created for this helper; never match names or profiles.
		_ = syscall.Kill(-t.pid, syscall.SIGKILL)
	}
}

func (t *authProcessTree) close() {}
