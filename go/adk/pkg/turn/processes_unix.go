//go:build unix

package turn

import "syscall"

func killGroup(pgid int) {
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

// groupAlive reports whether any process of the group pgid is left. Signal 0
// checks for the group without signalling it.
func groupAlive(pgid int) bool {
	return syscall.Kill(-pgid, 0) != syscall.ESRCH
}
