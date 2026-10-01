//go:build windows

package lsp

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ProcessSupervisor binds child processes to a Windows Job Object to prevent zombie processes.
type ProcessSupervisor struct {
	job windows.Handle
}

// NewProcessSupervisor creates a Windows Job Object configured to kill on job close.
func NewProcessSupervisor() (*ProcessSupervisor, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
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
		return nil, err
	}

	return &ProcessSupervisor{job: job}, nil
}

// Attach binds the given command's started process to this Job Object.
func (s *ProcessSupervisor) Attach(cmd *exec.Cmd) error {
	if s == nil || s.job == 0 || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid
	procHandle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(procHandle)
	return windows.AssignProcessToJobObject(s.job, procHandle)
}

// Close releases the job object handle, triggering kernel cleanup of all child processes.
func (s *ProcessSupervisor) Close() error {
	if s != nil && s.job != 0 {
		h := s.job
		s.job = 0
		return windows.CloseHandle(h)
	}
	return nil
}
