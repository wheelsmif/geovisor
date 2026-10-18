//go:build !windows

package browser

import (
	"os/exec"
	"syscall"
)

func configureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessTree(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if err != nil {
		return syscall.Kill(pid, syscall.SIGKILL)
	}
	return nil
}
