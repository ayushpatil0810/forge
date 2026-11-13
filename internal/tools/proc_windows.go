//go:build windows

package tools

import (
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ProcessGroup places a command in a Windows job object.
//
// os/exec does not expose PROC_THREAD_ATTRIBUTE_JOB_LIST (checked against
// golang.org/x/sys v0.35). The process is created suspended, assigned to the
// job, and only then resumed, so it cannot spawn a child before the assignment.
// If assignment or resume fails, the process is terminated and the error is
// returned. This is still not a sandbox: a process that cannot be placed in a
// job is not run.
type ProcessGroup struct {
	job windows.Handle
}

var procNtResumeProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess")

func setupProcessGroup(cmd *exec.Cmd) (*ProcessGroup, error) {
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
		_ = windows.CloseHandle(job)
		return nil, err
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	return &ProcessGroup{job: job}, nil
}

func (pg *ProcessGroup) Attach(cmd *exec.Cmd) error {
	if pg == nil || pg.job == 0 || cmd == nil || cmd.Process == nil {
		return fmt.Errorf("process group is not ready")
	}
	h, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	if err := windows.AssignProcessToJobObject(pg.job, h); err != nil {
		_ = windows.TerminateProcess(h, 1)
		return fmt.Errorf("assign process to job: %w", err)
	}
	if err := ntResumeProcess(h); err != nil {
		_ = windows.TerminateProcess(h, 1)
		return fmt.Errorf("resume process after job assignment: %w", err)
	}
	return nil
}

func ntResumeProcess(h windows.Handle) error {
	r, _, err := procNtResumeProcess.Call(uintptr(h))
	if r != 0 {
		if err != nil && err != windows.ERROR_SUCCESS {
			return err
		}
		return fmt.Errorf("NtResumeProcess status 0x%x", r)
	}
	return nil
}

func (pg *ProcessGroup) Close() {
	if pg != nil && pg.job != 0 {
		_ = windows.CloseHandle(pg.job)
		pg.job = 0
	}
}

func (pg *ProcessGroup) Kill(cmd *exec.Cmd) {
	if pg != nil && pg.job != 0 {
		_ = windows.CloseHandle(pg.job)
		pg.job = 0
	}
	if cmd != nil && cmd.Process != nil {
		_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
		_ = cmd.Process.Kill()
	}
}
