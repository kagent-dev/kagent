// Package processgroup manages subprocess trees started by Harness runtimes.
package processgroup

import (
	"os"
	"os/exec"
)

// Configure prepares command for process-group signaling when the platform supports it.
func Configure(command *exec.Cmd) {
	configure(command)
}

// Interrupt asks the process group, or the platform fallback process, to stop gracefully.
func Interrupt(process *os.Process) error {
	return interrupt(process)
}

// Terminate requests termination of the process group or platform fallback process.
func Terminate(process *os.Process) error {
	return terminate(process)
}

// Kill forcibly stops the process group or platform fallback process.
func Kill(process *os.Process) error {
	return kill(process)
}
