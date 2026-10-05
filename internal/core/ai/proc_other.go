//go:build !windows

package ai

import "os/exec"

// isolateProcess is a no-op on non-Windows platforms.
func isolateProcess(cmd *exec.Cmd) {
}
