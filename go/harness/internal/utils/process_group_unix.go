//go:build unix

package utils

import (
	"os"
	"os/exec"
	"syscall"
)

// ConfigureProcessGroup prepares command for process-group signaling.
func ConfigureProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// InterruptProcessGroup interrupts the process group.
func InterruptProcessGroup(process *os.Process) error {
	return syscall.Kill(-process.Pid, syscall.SIGINT)
}

// TerminateProcessGroup terminates the process group.
func TerminateProcessGroup(process *os.Process) error {
	return syscall.Kill(-process.Pid, syscall.SIGTERM)
}

// KillProcessGroup kills the process group.
func KillProcessGroup(process *os.Process) error {
	return syscall.Kill(-process.Pid, syscall.SIGKILL)
}
