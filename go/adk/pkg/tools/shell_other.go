//go:build !unix

package tools

import "os/exec"

// killProcessGroupOnCancel keeps the default cancellation, which kills the
// shell process, on platforms without Unix process groups.
func killProcessGroupOnCancel(*exec.Cmd) {}
