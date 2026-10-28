package taskstore

import (
	"context"
	"math/rand/v2"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

const (
	firstRetryWait = 200 * time.Millisecond
	maxRetryWait   = 2 * time.Second
)

func positiveOrDefault(setting env.DurationVar) time.Duration {
	if value := setting.Get(); value > 0 {
		return value
	}
	return setting.DefaultValue()
}

// retryWait doubles in a loop rather than shifting, so a long outage cannot
// overflow it.
func retryWait(failures int) time.Duration {
	wait := firstRetryWait
	for i := 1; i < failures && wait < maxRetryWait; i++ {
		wait *= 2
	}
	return min(wait, maxRetryWait)
}

// jitter spreads runtimes that lost the same controller, so they do not all
// retry at once when it returns.
func jitter(wait time.Duration) time.Duration {
	half := wait / 2
	return half + rand.N(wait-half+1)
}

type operation struct {
	name, sessionID, taskID string
}

func (op operation) attributes(start time.Time, attempts int) []any {
	return []any{"operation", op.name, "session_id", op.sessionID, "task_id", op.taskID,
		"attempts", attempts, "elapsed", time.Since(start)}
}

// retry replays the same immutable request on Unavailable and
// DeadlineExceeded until the operation budget or the caller's deadline ends.
// The server deduplicates a replayed mutation, so a lost response returns the
// committed version instead of applying it twice. It never retries a conflict,
// advances a version locally or invokes the agent again.
func (s *Store) retry(ctx context.Context, op operation, call func(context.Context) error) error {
	start := time.Now()
	operationCtx, cancel := context.WithTimeout(ctx, s.operationBudget)
	defer cancel()
	for attempt := 1; ; attempt++ {
		attemptCtx, cancelAttempt := context.WithTimeout(operationCtx, s.attemptTimeout)
		rpcCtx, cancelRPC := s.callContext(attemptCtx)
		err := call(rpcCtx)
		cancelRPC()
		cancelAttempt()
		if err == nil {
			if attempt > 1 {
				logging.FromContext(ctx).InfoContext(ctx, "task store call succeeded after retries", op.attributes(start, attempt)...)
			}
			return nil
		}
		if code := status.Code(err); code != codes.Unavailable && code != codes.DeadlineExceeded {
			return err
		}
		// Keep waiting when the deadline falls inside the backoff instead of
		// giving up early, so a caller never sees DeadlineExceeded before its
		// deadline has passed.
		timer := time.NewTimer(jitter(retryWait(attempt)))
		select {
		case <-operationCtx.Done():
			timer.Stop()
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			attributes := append(op.attributes(start, attempt), "budget", s.operationBudget, "code", status.Code(err).String(), "error", err)
			logging.FromContext(ctx).ErrorContext(ctx, "task store retries exhausted", attributes...)
			return err
		case <-timer.C:
		}
	}
}
