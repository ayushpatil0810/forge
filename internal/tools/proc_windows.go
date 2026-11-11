//go:build windows

package tools

import (
	"os/exec"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

type ProcessGroup struct {
	job windows.Handle
}

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
	return &ProcessGroup{job: job}, nil
}

func (pg *ProcessGroup) Attach(cmd *exec.Cmd) error {
	if pg == nil || pg.job == 0 || cmd == nil || cmd.Process == nil {
		return nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.AssignProcessToJobObject(pg.job, h)
}

func (pg *ProcessGroup) Kill(cmd *exec.Cmd) {
	if pg != nil && pg.job != 0 {
		_ = windows.CloseHandle(pg.job)
		pg.job = 0
	}
	if cmd != nil && cmd.Process != nil {
		// Run taskkill /F /T as a fallback guarantee for all descendant processes
		_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
		_ = cmd.Process.Kill()
	}
}

