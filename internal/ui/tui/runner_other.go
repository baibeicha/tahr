//go:build !windows

package tui

import "os/exec"

// isolateProcess is a no-op on POSIX systems where standard pipes already isolate output.
func isolateProcess(cmd *exec.Cmd) {}
