//go:build unix

package skillsinit

import (
	"os/exec"
	"syscall"
)

// configureGitProcess makes cancellation kill git's whole process group.
func configureGitProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
