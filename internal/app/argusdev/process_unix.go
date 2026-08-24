//go:build !windows

package argusdev

import (
	"os"
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func interruptProcess(process *os.Process) error {
	return syscall.Kill(-process.Pid, syscall.SIGINT)
}

func killProcessTree(process *os.Process) error {
	return syscall.Kill(-process.Pid, syscall.SIGKILL)
}
