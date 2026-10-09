//go:build linux

package tools

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/adk/pkg/turn"
	"golang.org/x/sys/unix"
)

// In a sandbox the runtime is the entrypoint, PID 1, so a background job is
// reparented to it once the shell exits, and only the runtime can reap it. A
// child subreaper stands in for that here: after the turn ends, not even an
// exited, unreaped process of the command is left for pgrep to count.
func TestExecuteCommand_TurnEndReapsOrphanedJobs(t *testing.T) {
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		t.Fatalf("become a child subreaper: %v", err)
	}
	t.Cleanup(func() { _ = unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 0, 0, 0, 0) })
	dir := t.TempDir()
	ctx, processes := turn.Begin(context.Background())

	result, err := NewCommandExecutor(time.Minute).ExecuteCommand(ctx, "echo $$ > pgid; sleep 600 & echo started", dir)
	if err != nil || result != "started" {
		t.Fatalf("ExecuteCommand() = %q, %v; want started", result, err)
	}
	pgid := readProcessGroup(t, filepath.Join(dir, "pgid"))

	processes.End()
	if err := syscall.Kill(-pgid, 0); err != syscall.ESRCH {
		t.Fatalf("process group %d still has a process after the turn ended (kill: %v)", pgid, err)
	}
}
