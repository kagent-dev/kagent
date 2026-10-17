package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// fakeMCPTasks answers the polls of retryLostMCPTurn: each invocation's task
// reports the statuses in its row, the last one for as long as it is polled.
type fakeMCPTasks struct {
	turns     [][]string
	invoked   int
	cancelled []string
	lost      []string
}

func (f *fakeMCPTasks) invoke() string {
	f.invoked++
	return fmt.Sprintf("task-%d", f.invoked)
}

func (f *fakeMCPTasks) poll(handle, want string, timeout time.Duration) (map[string]any, string, error) {
	var turn int
	if _, err := fmt.Sscanf(handle, "task-%d", &turn); err != nil {
		return nil, "", err
	}
	statuses := f.turns[turn-1]
	if handle == f.lastCancelled() {
		statuses = []string{"cancelled"}
	}
	last := statuses[len(statuses)-1]
	if last == want {
		return map[string]any{"status": last, "handle": handle}, last, nil
	}
	return nil, last, fmt.Errorf("MCP tasks/get of task %s: waited %s for status %q; last seen: status %q: %w", handle, timeout, want, last, context.DeadlineExceeded)
}

func (f *fakeMCPTasks) cancel(handle string) error {
	f.cancelled = append(f.cancelled, handle)
	return nil
}

func (f *fakeMCPTasks) lastCancelled() string {
	if len(f.cancelled) == 0 {
		return ""
	}
	return f.cancelled[len(f.cancelled)-1]
}

func (f *fakeMCPTasks) run() (map[string]any, error) {
	_, result, err := retryLostMCPTurn(time.Minute, "completed", f.invoke, f.poll, f.cancel, func(handle string) { f.lost = append(f.lost, handle) })
	return result, err
}

func TestRetryLostMCPTurn(t *testing.T) {
	t.Parallel()
	t.Run("a turn that completes is invoked once", func(t *testing.T) {
		t.Parallel()
		tasks := &fakeMCPTasks{turns: [][]string{{"working", "completed"}}}
		result, err := tasks.run()
		if err != nil || result["handle"] != "task-1" || tasks.invoked != 1 || len(tasks.cancelled) != 0 {
			t.Fatalf("result %v, error %v, invoked %d, cancelled %v", result, err, tasks.invoked, tasks.cancelled)
		}
	})
	t.Run("a turn lost in working is cancelled and invoked once more", func(t *testing.T) {
		t.Parallel()
		tasks := &fakeMCPTasks{turns: [][]string{{"working"}, {"completed"}}}
		result, err := tasks.run()
		if err != nil || result["handle"] != "task-2" {
			t.Fatalf("result %v, error %v", result, err)
		}
		if tasks.invoked != 2 || len(tasks.cancelled) != 1 || tasks.cancelled[0] != "task-1" || len(tasks.lost) != 1 {
			t.Fatalf("invoked %d, cancelled %v, lost %v", tasks.invoked, tasks.cancelled, tasks.lost)
		}
	})
	t.Run("a second lost turn fails", func(t *testing.T) {
		t.Parallel()
		tasks := &fakeMCPTasks{turns: [][]string{{"working"}, {"working"}}}
		_, err := tasks.run()
		if !errors.Is(err, context.DeadlineExceeded) || tasks.invoked != 2 || len(tasks.cancelled) != 1 {
			t.Fatalf("error %v, invoked %d, cancelled %v", err, tasks.invoked, tasks.cancelled)
		}
	})
	for _, status := range []string{"submitted", "failed", "input_required"} {
		t.Run("a task "+status+" is not resent", func(t *testing.T) {
			t.Parallel()
			tasks := &fakeMCPTasks{turns: [][]string{{status}, {"completed"}}}
			_, err := tasks.run()
			if err == nil || tasks.invoked != 1 || len(tasks.cancelled) != 0 {
				t.Fatalf("error %v, invoked %d, cancelled %v", err, tasks.invoked, tasks.cancelled)
			}
		})
	}
}
