package driver

import (
	"os"
	"os/exec"
)

func configureProcessGroup(*exec.Cmd) {}

func interruptProcessGroup(process *os.Process) error {
	return process.Signal(os.Interrupt)
}

func killProcessGroup(process *os.Process) error {
	return process.Kill()
}
