//go:build !windows

package tui

import (
	"io"
	"os"
	"os/exec"
	"sync"
)

type nonWindowsPTY struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	closed bool
}

func (p *nonWindowsPTY) Read(b []byte) (int, error) {
	return p.stdout.Read(b)
}

func (p *nonWindowsPTY) Write(b []byte) (int, error) {
	return p.stdin.Write(b)
}

func (p *nonWindowsPTY) Resize(cols, rows int) error {
	return nil
}

func (p *nonWindowsPTY) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	if p.stdout != nil {
		_ = p.stdout.Close()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	return nil
}

func (p *nonWindowsPTY) Wait() (*os.ProcessState, error) {
	if p.cmd != nil {
		err := p.cmd.Wait()
		return p.cmd.ProcessState, err
	}
	return nil, nil
}

func (p *nonWindowsPTY) Process() *os.Process {
	if p.cmd != nil {
		return p.cmd.Process
	}
	return nil
}

// StartShell launches an interactive shell on Unix/macOS.
func StartShell(cwd string, cols, rows int) (PTYSession, error) {
	if cwd == "" {
		cwd = "."
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	if _, err := exec.LookPath(shell); err != nil {
		shell = "/bin/sh"
	}

	cmd := exec.Command(shell)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &nonWindowsPTY{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
	}, nil
}
