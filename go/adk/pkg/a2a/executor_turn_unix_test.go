//go:build unix

package a2a

import (
	"context"
	"iter"
	"log/slog"
	"os/exec"
	"syscall"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/adk/pkg/turn"
)

// backgroundJobExecutor starts a long-running process group the way the bash
// tool does when a command backgrounds a job, then ends the turn.
type backgroundJobExecutor struct {
	t    *testing.T
	pgid int
}

func (e *backgroundJobExecutor) Execute(ctx context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		processes := turn.FromContext(ctx)
		if processes == nil {
			yield(nil, context.Canceled)
			return
		}
		cmd := exec.Command("sleep", "600")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			yield(nil, err)
			return
		}
		e.pgid = cmd.Process.Pid
		e.t.Cleanup(func() { _ = syscall.Kill(-e.pgid, syscall.SIGKILL) })
		processes.Track(e.pgid)
		if !yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, nil), nil) {
			return
		}
		yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCompleted, nil), nil)
	}
}

func (e *backgroundJobExecutor) Cancel(context.Context, *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(func(a2atype.Event, error) bool) {}
}

// A process a tool started in turn N is gone before the event that ends the
// turn reaches the caller, so it is not running when turn N+1 begins.
func TestKAgentExecutor_EndsTheTurnsProcessesBeforeYieldingTheTerminalEvent(t *testing.T) {
	builtin := &backgroundJobExecutor{t: t}
	executor := &KAgentExecutor{builtin: builtin, logger: slog.New(slog.DiscardHandler)}
	reqCtx := &a2asrv.ExecutorContext{
		TaskID: "task-1", ContextID: "ctx-1",
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("sleep 600 &")),
	}

	for event, err := range executor.Execute(context.Background(), reqCtx) {
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		switch state := event.(*a2atype.TaskStatusUpdateEvent).Status.State; state {
		case a2atype.TaskStateWorking:
			if syscall.Kill(-builtin.pgid, 0) != nil {
				t.Fatal("the background job was not running during the turn")
			}
		case a2atype.TaskStateCompleted:
			if !killed(builtin.pgid) {
				t.Fatal("the background job was not killed when the terminal event was yielded")
			}
		}
	}
}

// killed reaps the group's only process, a child of the test, and reports
// whether SIGKILL ended it. Delivery is asynchronous, so it waits briefly.
func killed(pid int) bool {
	var status syscall.WaitStatus
	for range 100 {
		if reaped, _ := syscall.Wait4(pid, &status, syscall.WNOHANG, nil); reaped == pid {
			return status.Signaled() && status.Signal() == syscall.SIGKILL
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
