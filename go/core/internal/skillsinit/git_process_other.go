//go:build !unix

package skillsinit

import "os/exec"

// configureGitProcess keeps the default cancellation.
func configureGitProcess(cmd *exec.Cmd) {}
