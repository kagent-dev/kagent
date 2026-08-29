package processgroup

import (
	"os"
	"os/exec"
)

func configure(*exec.Cmd) {}

func interrupt(process *os.Process) error {
	return process.Signal(os.Interrupt)
}

func terminate(process *os.Process) error {
	return process.Kill()
}

func kill(process *os.Process) error {
	return process.Kill()
}
