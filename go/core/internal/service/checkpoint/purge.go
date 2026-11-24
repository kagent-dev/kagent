package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"github.com/kagent-dev/kagent/go/pkg/telemetry/conv"
)

type purgeStore interface {
	ListIdlePurgeCandidates(context.Context, uuid.UUID, int) ([]database.IdlePurgeCandidate, error)
	BeginIdlePurgeCheckpointDeletion(context.Context, uuid.UUID) (*database.IdlePurgeCheckpoint, bool, error)
	ListIdlePurgeForks(context.Context, uuid.UUID, string, int) ([]database.IdlePurgeFork, error)
	ReleaseIdlePurgeForkPin(context.Context, database.IdlePurgeFork, database.SessionTaskSnapshot) (bool, error)
	DeleteIdlePurgeHistory(context.Context, uuid.UUID) (bool, error)
	IdlePurgeLastEventAt(context.Context, uuid.UUID) (time.Time, error)
}

// PurgeWorker settles orphaned histories from idle expiry, including those
// left by earlier controller versions.
type PurgeWorker struct {
	store        purgeStore
	checkpoints  *Service
	pollInterval time.Duration
	completed    metric.Int64Counter
	blocked      metric.Int64Counter
}

var _ manager.Runnable = (*PurgeWorker)(nil)
var _ manager.LeaderElectionRunnable = (*PurgeWorker)(nil)
var _ purgeStore = (*database.Client)(nil)

func NewPurgeWorker(store purgeStore, checkpoints *Service, pollInterval time.Duration, meters metric.MeterProvider) (*PurgeWorker, error) {
	if pollInterval <= 0 {
		return nil, fmt.Errorf("session purge poll interval must be positive")
	}
	meter := meters.Meter("github.com/kagent-dev/kagent/go/core/internal/service/checkpoint")
	completed, err := meter.Int64Counter(conv.KagentSessionPurgeCompleted,
		metric.WithUnit(conv.KagentSessionPurgeCompletedUnit), metric.WithDescription(conv.KagentSessionPurgeCompletedDescription))
	if err != nil {
		return nil, fmt.Errorf("create session purge counter: %w", err)
	}
	blocked, err := meter.Int64Counter(conv.KagentSessionPurgeBlocked,
		metric.WithUnit(conv.KagentSessionPurgeBlockedUnit), metric.WithDescription(conv.KagentSessionPurgeBlockedDescription))
	if err != nil {
		return nil, fmt.Errorf("create blocked session purge counter: %w", err)
	}
	return &PurgeWorker{store: store, checkpoints: checkpoints, pollInterval: pollInterval, completed: completed, blocked: blocked}, nil
}

func (*PurgeWorker) NeedLeaderElection() bool { return true }

func (p *PurgeWorker) Start(ctx context.Context) error {
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()
	var afterID uuid.UUID
	for ctx.Err() == nil {
		afterID = p.sweep(ctx, afterID)
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	return nil
}

// sweep bounds both scanned rows and orphaned histories per interval. Advancing
// past failed work gives the rest of the histories a chance before the next pass.
func (p *PurgeWorker) sweep(ctx context.Context, afterID uuid.UUID) uuid.UUID {
	const pageSize = 256
	const maxHistories = 32
	histories := 0
	for range 4 {
		if ctx.Err() != nil {
			return afterID
		}
		candidates, err := p.store.ListIdlePurgeCandidates(ctx, afterID, pageSize)
		if err != nil {
			if ctx.Err() == nil {
				logging.FromContext(ctx).ErrorContext(ctx, "scan idle session history", "error", err)
			}
			return afterID
		}
		for _, candidate := range candidates {
			if ctx.Err() != nil || histories == maxHistories {
				return afterID
			}
			afterID = candidate.HistoryID
			if !candidate.Orphaned {
				continue
			}
			histories++
			attemptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := p.purge(attemptCtx, candidate.HistoryID)
			cancel()
			if err != nil && !errors.Is(err, database.ErrNotFound) && ctx.Err() == nil {
				logging.FromContext(ctx).ErrorContext(ctx, "purge expired session history", "history_id", candidate.HistoryID, "error", err)
			}
		}
		if len(candidates) < pageSize {
			return uuid.Nil
		}
	}
	return afterID
}

func (p *PurgeWorker) purge(ctx context.Context, historyID uuid.UUID) error {
	for range 8 {
		checkpoint, retired, err := p.store.BeginIdlePurgeCheckpointDeletion(ctx, historyID)
		if err != nil {
			return err
		}
		if checkpoint != nil {
			if err := p.checkpoints.deleteRetainedCheckpoint(ctx, checkpoint.ID, checkpoint.UserID, &checkpoint.Snapshot, checkpoint.TagUID); err != nil {
				return err
			}
			continue
		}
		if retired {
			continue
		}
		done, err := p.store.DeleteIdlePurgeHistory(ctx, historyID)
		if errors.Is(err, database.ErrConflict) {
			released, err := p.releaseForkPins(ctx, historyID)
			if err != nil {
				return err
			}
			if released {
				continue
			}
			p.blocked.Add(ctx, 1)
			lastEventAt, err := p.store.IdlePurgeLastEventAt(ctx, historyID)
			if err != nil {
				return err
			}
			logging.FromContext(ctx).DebugContext(ctx, "idle session purge blocked by retained checkpoints", "history_id", historyID, "idle_time", time.Since(lastEventAt))
			return nil
		}
		if err != nil {
			return err
		}
		if done {
			p.completed.Add(ctx, 1)
			return nil
		}
	}
	return nil
}

func (p *PurgeWorker) releaseForkPins(ctx context.Context, historyID uuid.UUID) (bool, error) {
	const pageSize = 16
	var afterID string
	for {
		forks, err := p.store.ListIdlePurgeForks(ctx, historyID, afterID, pageSize)
		if err != nil {
			return false, err
		}
		for _, fork := range forks {
			inspectCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			released, err := p.releaseForkPin(inspectCtx, fork)
			cancel()
			if err != nil {
				if ctx.Err() != nil {
					return false, ctx.Err()
				}
				logging.FromContext(ctx).ErrorContext(ctx, "inspect idle purge fork pin", "fork_id", fork.ID, "error", err)
			}
			if released {
				return true, nil
			}
			afterID = fork.ID
		}
		if len(forks) < pageSize {
			return false, nil
		}
	}
}

func (p *PurgeWorker) releaseForkPin(ctx context.Context, fork database.IdlePurgeFork) (bool, error) {
	name := substrate.ActorName(fork.ID)
	actor, err := p.checkpoints.tags.GetActor(ctx, fork.Atespace, name)
	if status.Code(err) == codes.NotFound {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect fork Actor %s: %w", name, err)
	}
	metadata := actor.GetMetadata()
	snapshot := substrate.LatestDurableActorSnapshot(actor)
	if metadata.GetUid() != fork.ActorUID || metadata.GetAtespace() != fork.Atespace || metadata.GetName() != name ||
		snapshot.GetOwner() != ateapipb.SnapshotOwner_SNAPSHOT_OWNER_ACTOR ||
		snapshot.GetFidelity() != ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES {
		return false, nil
	}
	return p.store.ReleaseIdlePurgeForkPin(ctx, fork, database.SessionTaskSnapshot{
		Atespace: fork.Atespace, URI: substrate.SnapshotURI(snapshot), Fidelity: snapshot.GetFidelity().String(),
	})
}
