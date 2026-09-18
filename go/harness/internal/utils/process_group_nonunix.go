//go:build !unix

package utils

import (
	"os"
	"os/exec"
)

// ConfigureProcessGroup is a no-op on platforms without Unix process groups.
func ConfigureProcessGroup(*exec.Cmd) {}

// InterruptProcessGroup interrupts the process.
func InterruptProcessGroup(process *os.Process) error {
	return process.Signal(os.Interrupt)
}

// TerminateProcessGroup terminates the process.
func TerminateProcessGroup(process *os.Process) error {
	return process.Kill()
}

// KillProcessGroup kills the process.
func KillProcessGroup(process *os.Process) error {
	return process.Kill()
}
