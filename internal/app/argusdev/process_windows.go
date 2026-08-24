//go:build windows

package argusdev

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

const createNewProcessGroup = 0x00000200

func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func interruptProcess(process *os.Process) error {
	return exec.Command("taskkill", "/PID", fmt.Sprint(process.Pid), "/T").Run()
}

func killProcessTree(process *os.Process) error {
	return exec.Command("taskkill", "/PID", fmt.Sprint(process.Pid), "/T", "/F").Run()
}
