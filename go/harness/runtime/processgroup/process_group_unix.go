//go:build unix

package processgroup

import (
	"os"
	"os/exec"
	"syscall"
)

func configure(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func interrupt(process *os.Process) error {
	return syscall.Kill(-process.Pid, syscall.SIGINT)
}

func terminate(process *os.Process) error {
	return syscall.Kill(-process.Pid, syscall.SIGTERM)
}

func kill(process *os.Process) error {
	return syscall.Kill(-process.Pid, syscall.SIGKILL)
}
