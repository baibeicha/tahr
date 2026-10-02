//go:build windows

package tui

import (
	"os/exec"
	"syscall"
)

// isolateProcess prevents spawned background process from inheriting console handles or flashing windows.
func isolateProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
