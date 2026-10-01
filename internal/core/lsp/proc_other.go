//go:build !windows && !linux

package lsp

import "os/exec"

// ProcessSupervisor is a fallback supervisor for non-Windows, non-Linux systems.
type ProcessSupervisor struct{}

func NewProcessSupervisor() (*ProcessSupervisor, error) {
	return &ProcessSupervisor{}, nil
}

func (s *ProcessSupervisor) Attach(cmd *exec.Cmd) error {
	return nil
}

func (s *ProcessSupervisor) Close() error {
	return nil
}
