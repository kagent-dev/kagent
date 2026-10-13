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
}

// ExpirationWorker deletes idle sessions through their ordinary delete workflow.
// PostgreSQL admission and execution claims fence concurrent API requests.
type ExpirationWorker struct {
	store        expirationStore
	workflow     *ActorWorkflow
	idleTTL      time.Duration
	pollInterval time.Duration
	deleted      metric.Int64Counter
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
	deleted, err := otel.Meter("github.com/kagent-dev/kagent/go/core/internal/service/session").Int64Counter(
		"kagent.session.expired", metric.WithDescription("Sessions deleted by the idle expiration sweep."), metric.WithUnit("{session}"))
	if err != nil {
		return nil, fmt.Errorf("create session expiration counter: %w", err)
	}
	return &ExpirationWorker{store: store, workflow: workflow, idleTTL: idleTTL, pollInterval: pollInterval, deleted: deleted}, nil
}

func (*ExpirationWorker) NeedLeaderElection() bool { return true }

// Start scans bounded pages, like sandbox expiration. Ordinary pending lifecycle
// work is still client-driven. Zero disables both admission and expiration retries.
func (e *ExpirationWorker) Start(ctx context.Context) error {
	if e.idleTTL == 0 {
		<-ctx.Done()
		return nil
	}
	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()
	var afterID string
	for ctx.Err() == nil {
		before := time.Now().Add(-e.idleTTL)
		ids, err := e.store.ListIdleSessions(ctx, before, afterID, 100)
		if err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "list idle sessions", "error", err)
		} else {
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
			if err := group.Wait(); err != nil {
				return err
			}
			afterID = ""
			if len(ids) == 100 {
				afterID = ids[len(ids)-1]
				continue
			}
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	return nil
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
