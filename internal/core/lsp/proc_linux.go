//go:build linux

package lsp

import (
	"os/exec"
	"syscall"
)

// ProcessSupervisor manages child process lifecycle on Linux via PR_SET_PDEATHSIG.
type ProcessSupervisor struct{}

// NewProcessSupervisor initializes a Linux process supervisor.
func NewProcessSupervisor() (*ProcessSupervisor, error) {
	return &ProcessSupervisor{}, nil
}

// Attach sets SysProcAttr Pdeathsig to SIGTERM so the child dies if parent exits.
func (s *ProcessSupervisor) Attach(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGTERM
	return nil
}

// Close is a no-op on Linux.
func (s *ProcessSupervisor) Close() error {
	return nil
}
