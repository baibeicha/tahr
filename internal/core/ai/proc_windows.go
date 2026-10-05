//go:build windows

package ai

import (
	"os/exec"
	"syscall"
)

// isolateProcess prevents llama-server child process from flashing console windows.
func isolateProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
