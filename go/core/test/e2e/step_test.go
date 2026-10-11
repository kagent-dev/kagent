package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// waitForStep runs check every interval until it reports done, for at most
// timeout. Each check runs under the step's deadline, so a call that never
// answers is bounded too. A check error fails the step at once; the deadline
// fails it naming the step, the state it waited for and the last state check
// reported, so a stuck test says where it stopped instead of hanging until
// the job is cancelled.
func waitForStep(ctx context.Context, step, want string, timeout, interval time.Duration, check func(context.Context) (done bool, seen string, err error)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	seen := "no answer"
	for {
		done, last, err := check(ctx)
		if last != "" {
			seen = last
		}
		switch {
		case done && err == nil:
			return nil
		case ctx.Err() != nil:
		case err != nil:
			return fmt.Errorf("%s: %w", step, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: waited %s for %s; last seen: %s: %w", step, timeout, want, seen, ctx.Err())
		case <-time.After(interval):
		}
	}
}

func TestWaitForStepNamesWhatItWaitedOn(t *testing.T) {
	t.Parallel()
	t.Run("a state that never comes", func(t *testing.T) {
		t.Parallel()
		err := waitForStep(t.Context(), "MCP tasks/get of task t1", `status "completed"`, 50*time.Millisecond, time.Millisecond,
			func(context.Context) (bool, string, error) { return false, `status "working"`, nil })
		want := `MCP tasks/get of task t1: waited 50ms for status "completed"; last seen: status "working": context deadline exceeded`
		if err == nil || err.Error() != want || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("waitForStep() = %v, want %q", err, want)
		}
	})
	t.Run("a call that never answers", func(t *testing.T) {
		t.Parallel()
		err := waitForStep(t.Context(), "MCP tools/call invoke_session session_id=s1", "the turn's result", 50*time.Millisecond, time.Millisecond,
			func(ctx context.Context) (bool, string, error) {
				<-ctx.Done()
				return false, "", ctx.Err()
			})
		if err == nil || !strings.HasPrefix(err.Error(), "MCP tools/call invoke_session session_id=s1: waited 50ms for the turn's result; last seen: no answer") {
			t.Fatalf("waitForStep() = %v, want the step, what it waited for and no answer", err)
		}
	})
	t.Run("a failed check", func(t *testing.T) {
		t.Parallel()
		refused := errors.New("refused")
		calls := 0
		err := waitForStep(t.Context(), "create checkpoint", "a checkpoint", time.Minute, time.Millisecond,
			func(context.Context) (bool, string, error) {
				calls++
				return false, "", refused
			})
		if !errors.Is(err, refused) || calls != 1 || err.Error() != "create checkpoint: refused" {
			t.Fatalf("waitForStep() = %v after %d checks, want the step's refusal after one", err, calls)
		}
	})
	t.Run("done", func(t *testing.T) {
		t.Parallel()
		calls := 0
		err := waitForStep(t.Context(), "step", "done", time.Minute, time.Millisecond,
			func(context.Context) (bool, string, error) {
				calls++
				return calls == 3, "", nil
			})
		if err != nil || calls != 3 {
			t.Fatalf("waitForStep() = %v after %d checks, want nil after 3", err, calls)
		}
	})
}
