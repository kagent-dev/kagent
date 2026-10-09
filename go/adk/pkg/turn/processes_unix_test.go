//go:build unix

package turn

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// startGroup starts a sleep leading a process group of its own and returns
// the group ID; the sleep is reaped in the background.
func startGroup(t *testing.T) (pgid int, exited <-chan syscall.Signal) {
	t.Helper()
	cmd := exec.Command("sleep", "600")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	done := make(chan syscall.Signal, 1)
	go func() {
		_ = cmd.Wait()
		done <- cmd.ProcessState.Sys().(syscall.WaitStatus).Signal()
	}()
	return cmd.Process.Pid, done
}

func requireKilled(t *testing.T, exited <-chan syscall.Signal) {
	t.Helper()
	select {
	case signal := <-exited:
		if signal != syscall.SIGKILL {
			t.Fatalf("process ended by %v, want SIGKILL", signal)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("process still running")
	}
}

func TestFromContext(t *testing.T) {
	if FromContext(context.Background()) != nil {
		t.Fatal("Processes outside a turn")
	}
	ctx, processes := Begin(context.Background())
	if FromContext(ctx) != processes {
		t.Fatal("FromContext() is not the turn's Processes")
	}
}

func TestEndKillsTrackedGroups(t *testing.T) {
	_, processes := Begin(context.Background())
	pgid, exited := startGroup(t)
	processes.Track(pgid)

	processes.End()
	requireKilled(t, exited)
	processes.End()
}

func TestTrackAfterEndKillsAtOnce(t *testing.T) {
	_, processes := Begin(context.Background())
	processes.End()
	pgid, exited := startGroup(t)

	processes.Track(pgid)
	requireKilled(t, exited)
}

func TestSettleForgetsAnExitedGroupOnly(t *testing.T) {
	_, processes := Begin(context.Background())
	running, _ := startGroup(t)
	exitedGroup, exited := startGroup(t)
	processes.Track(running)
	processes.Track(exitedGroup)
	_ = syscall.Kill(-exitedGroup, syscall.SIGKILL)
	<-exited

	processes.Settle(running)
	processes.Settle(exitedGroup)

	if _, ok := processes.groups[running]; !ok {
		t.Error("Settle forgot a group that is still running")
	}
	if _, ok := processes.groups[exitedGroup]; ok {
		t.Error("Settle kept a group none of whose processes is left")
	}
}
