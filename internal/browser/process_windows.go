//go:build windows

package browser

import (
	"os/exec"
	"strconv"
	"syscall"
)

func configureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func killProcessTree(pid int) error {
	killer := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	killer.Stdout = nil
	killer.Stderr = nil
	return killer.Run()
}
