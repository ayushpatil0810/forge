//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
)

type ProcessGroup struct{}

func setupProcessGroup(cmd *exec.Cmd) (*ProcessGroup, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &ProcessGroup{}, nil
}

func (pg *ProcessGroup) Attach(cmd *exec.Cmd) error {
	return nil
}

func (pg *ProcessGroup) Close() {}

func (pg *ProcessGroup) Kill(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		pgid, err := syscall.Getpgid(cmd.Process.Pid)
		if err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		_ = cmd.Process.Kill()
	}
}
