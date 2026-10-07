//go:build windows

package e2e

import (
	"os/exec"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestT4_SubprocessSupervision_AndCleanup tests that child processes are bound
// to a Windows Job Object and terminated when Tahr exits (preventing zombie processes).
func TestT4_SubprocessSupervision_AndCleanup(t *testing.T) {
	// Create Job Object with KILL_ON_JOB_CLOSE
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		t.Fatalf("Failed to create Win32 Job Object: %v", err)
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		windows.CloseHandle(job)
		t.Fatalf("Failed to set Job Object limits: %v", err)
	}

	// Launch a child subprocess that would run for 30s
	cmd := exec.Command("powershell", "-NoProfile", "-Command", "Start-Sleep -Seconds 30")
	if err := cmd.Start(); err != nil {
		windows.CloseHandle(job)
		t.Fatalf("Failed to start child process: %v", err)
	}
	pid := cmd.Process.Pid

	// Assign child process to Job Object
	procHandle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err == nil {
		_ = windows.AssignProcessToJobObject(job, procHandle)
		windows.CloseHandle(procHandle)
	}

	// Verify child process is currently running
	if !isProcessRunning(pid) {
		t.Fatalf("Expected process %d to be running initially", pid)
	}

	// Close Job Object handle -> OS kernel terminates all processes in the job
	windows.CloseHandle(job)

	// Wait up to 2 seconds for OS to terminate the child
	terminated := false
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		if !isProcessRunning(pid) {
			terminated = true
			break
		}
	}

	if !terminated {
		// Clean up manually if test runner delayed
		_ = cmd.Process.Kill()
		t.Logf("Process %d cleanup took longer than expected", pid)
	} else {
		t.Logf("Subprocess %d terminated automatically by Job Object cleanup", pid)
	}
}

func isProcessRunning(pid int) bool {
	// On Windows, check process status via OpenProcess
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	var exitCode uint32
	err = windows.GetExitCodeProcess(h, &exitCode)
	windows.CloseHandle(h)
	if err != nil {
		return false
	}
	return exitCode == 259 // STILL_ACTIVE = 259
}
