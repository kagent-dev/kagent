package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type expirationStore interface {
	ListIdleSessions(context.Context, time.Time, string, int) ([]string, error)
	BeginIdleSessionDeletion(context.Context, string, time.Time) (*database.IdleSessionDeletion, error)
	DeleteExpiredSessionShares(context.Context, time.Time) (int64, error)
}

// ExpirationWorker deletes idle sessions through their ordinary delete workflow
// and drops the session shares that expired. PostgreSQL admission and execution
// claims fence concurrent API requests.
type ExpirationWorker struct {
	store         expirationStore
	workflow      *ActorWorkflow
	idleTTL       time.Duration
	pollInterval  time.Duration
	deleted       metric.Int64Counter
	sharesExpired metric.Int64Counter
}

var _ manager.Runnable = (*ExpirationWorker)(nil)
var _ manager.LeaderElectionRunnable = (*ExpirationWorker)(nil)
var _ expirationStore = (*database.Client)(nil)

func NewExpirationWorker(store expirationStore, workflow *ActorWorkflow, idleTTL, pollInterval time.Duration) (*ExpirationWorker, error) {
	if idleTTL < 0 {
		return nil, fmt.Errorf("session idle TTL must be nonnegative")
	}
	if pollInterval <= 0 {
		return nil, fmt.Errorf("session expiration poll interval must be positive")
	}
	meter := otel.Meter("github.com/kagent-dev/kagent/go/core/internal/service/session")
	deleted, err := meter.Int64Counter(
		"kagent.session.expired", metric.WithDescription("Sessions deleted by the idle expiration sweep."), metric.WithUnit("{session}"))
	if err != nil {
		return nil, fmt.Errorf("create session expiration counter: %w", err)
	}
	sharesExpired, err := meter.Int64Counter(
		"kagent.session.share_expired", metric.WithDescription("Session shares deleted by the expiration sweep after they expired."), metric.WithUnit("{share}"))
	if err != nil {
		return nil, fmt.Errorf("create session share expiration counter: %w", err)
	}
	return &ExpirationWorker{store: store, workflow: workflow, idleTTL: idleTTL, pollInterval: pollInterval, deleted: deleted, sharesExpired: sharesExpired}, nil
}

func (*ExpirationWorker) NeedLeaderElection() bool { return true }

// Start sweeps on the poll interval: the idle sessions in bounded pages, like
// sandbox expiration, then the shares that expired. Ordinary pending lifecycle
// work is still client-driven. A zero idle TTL disables idle deletion, its
// admission and its retries; a share expires on its own expires_at and is
// swept regardless.
func (e *ExpirationWorker) Start(ctx context.Context) error {
	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if e.idleTTL > 0 {
			e.expireIdle(ctx)
		}
		e.expireShares(ctx, time.Now())
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	return nil
}

// expireIdle deletes the sessions idle for the TTL, page by page until a page
// is short. A listing that fails ends the sweep; the next tick lists again.
func (e *ExpirationWorker) expireIdle(ctx context.Context) {
	var afterID string
	for ctx.Err() == nil {
		before := time.Now().Add(-e.idleTTL)
		ids, err := e.store.ListIdleSessions(ctx, before, afterID, 100)
		if err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "list idle sessions", "error", err)
			return
		}
		var group errgroup.Group
		group.SetLimit(4)
		for _, id := range ids {
			group.Go(func() error {
				if err := e.expire(ctx, id, before); err != nil && !errors.Is(err, database.ErrConflict) && !errors.Is(err, database.ErrFailedPrecondition) && !errors.Is(err, database.ErrNotFound) && ctx.Err() == nil {
					logging.FromContext(ctx).ErrorContext(ctx, "expire session", "session_id", id, "error", err)
				}
				return nil
			})
		}
		_ = group.Wait() // every deletion logs its own error and returns nil
		if len(ids) < 100 {
			return
		}
		afterID = ids[len(ids)-1]
	}
}

func (e *ExpirationWorker) expire(ctx context.Context, id string, before time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancel()
	deletion, err := e.store.BeginIdleSessionDeletion(ctx, id, before)
	if err != nil {
		return err
	}
	if _, err := e.workflow.execute(ctx, deletion.Operation); err != nil {
		return err
	}
	e.deleted.Add(ctx, 1)
	logging.FromContext(ctx).DebugContext(ctx, "expired idle session", "session_id", id, "idle_time", time.Since(deletion.IdleSince))
	return nil
}

// expireShares deletes the shares expired at now, judged on the clock that set
// their expires_at, as the token lookup does. An expired share has granted
// nothing since its expiry; its row only waited for the owner to revoke it or
// for the session to be deleted.
func (e *ExpirationWorker) expireShares(ctx context.Context, now time.Time) {
	count, err := e.store.DeleteExpiredSessionShares(ctx, now)
	if err != nil {
		if ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "delete expired session shares", "error", err)
		}
		return
	}
	if count == 0 {
		return
	}
	e.sharesExpired.Add(ctx, count)
	logging.FromContext(ctx).DebugContext(ctx, "deleted expired session shares", "count", count)
}
