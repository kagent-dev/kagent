//go:build unix

package turn

import (
	"syscall"
	"time"
)

func killGroup(pgid int) {
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

// groupAlive reports whether any process of the group pgid is left, an exited
// one not yet reaped included. Signal 0 checks for the group without
// signalling it.
func groupAlive(pgid int) bool {
	return syscall.Kill(-pgid, 0) != syscall.ESRCH
}

// reapGroup reaps the exited processes of the group pgid that are children of
// this process, waiting up to wait for the rest of them to exit. It only ever
// waits on that group, so it never takes a child os/exec waits for.
func reapGroup(pgid int, wait time.Duration) {
	deadline := time.Now().Add(wait)
	for {
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-pgid, &status, syscall.WNOHANG, nil)
		switch {
		case err == syscall.EINTR, pid > 0:
			continue
		case err != nil:
			// ECHILD: no child of this process is left in the group.
			return
		case time.Now().After(deadline):
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}
