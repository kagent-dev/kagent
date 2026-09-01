package utils

import (
	"os"
	"os/exec"
)

func configureProcessGroup(*exec.Cmd) {}

func interruptProcessGroup(process *os.Process) error {
	return process.Signal(os.Interrupt)
}

func terminateProcessGroup(process *os.Process) error {
	return process.Kill()
}

func killProcessGroup(process *os.Process) error {
	return process.Kill()
}
