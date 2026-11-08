package checkpoint

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// RecoveryWorker resumes expired durable work with the same execution path as
// requests. The sweep interval bounds retries; live requests are never queued.
type RecoveryWorker struct {
	service  *Service
	interval time.Duration
}

var _ manager.Runnable = (*RecoveryWorker)(nil)
var _ manager.LeaderElectionRunnable = (*RecoveryWorker)(nil)

func NewRecoveryWorker(service *Service) *RecoveryWorker {
	return &RecoveryWorker{service: service, interval: 30 * time.Second}
}

func (*RecoveryWorker) NeedLeaderElection() bool { return true }

func (w *RecoveryWorker) Start(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := w.sweep(ctx); err != nil && ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "recover checkpoints", "error", err)
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	return nil
}

func (w *RecoveryWorker) sweep(ctx context.Context) error {
	var after string
	for ctx.Err() == nil {
		operations, err := w.service.store.ListPendingCheckpoints(ctx, after, 100)
		if err != nil {
			return err
		}
		for _, op := range operations {
			if err := w.service.recoverOperation(ctx, op); err != nil && ctx.Err() == nil {
				logging.FromContext(ctx).ErrorContext(ctx, "recover checkpoint", "checkpoint_id", op.Checkpoint.Id, "error", err)
			}
		}
		if len(operations) < 100 {
			return nil
		}
		after = operations[len(operations)-1].Checkpoint.Id
	}
	return ctx.Err()
}

func (s *Service) recoverOperation(ctx context.Context, observed *database.CheckpointOperation) error {
	ctx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancel()
	_, err := s.claimAndExecute(ctx, observed)
	return err
}

// Claim rereads the current phase: a sweep's observation can already be stale.
// A failed acquisition returns the current outcome rather than polling or
// replaying an earlier attempt's error.
func (s *Service) claimAndExecute(ctx context.Context, observed *database.CheckpointOperation) (result *apiv1.Checkpoint, err error) {
	executor := uuid.New()
	id, owner := observed.Checkpoint.Id, observed.Creator
	acquired := false
	defer func() {
		if !acquired {
			err = errors.Join(err, s.release(ctx, id, executor))
		}
	}()
	op, err := s.store.ClaimCheckpointOperation(ctx, id, owner, executor)
	if errors.Is(err, database.ErrConflict) {
		current, readErr := s.store.GetCheckpointOperation(ctx, id, owner)
		if errors.Is(readErr, database.ErrNotFound) {
			return nil, nil
		}
		if readErr != nil {
			return nil, readErr
		}
		return createOutcome(current.Checkpoint)
	}
	if err != nil {
		return nil, err
	}
	acquired = true
	return s.execute(ctx, op, executor)
}

func (s *Service) release(ctx context.Context, id string, executor uuid.UUID) error {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.store.ReleaseCheckpointOperation(releaseCtx, id, executor); err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "release checkpoint execution claim", "checkpoint_id", id, "error", err)
		// This error is joined to the current attempt's public status. Keep
		// database details in logs without replacing the original status code.
		return errors.New("checkpoint execution claim could not be released")
	}
	return nil
}

func (s *Service) execute(ctx context.Context, op *database.CheckpointOperation, executor uuid.UUID) (result *apiv1.Checkpoint, err error) {
	completed := false
	defer func() {
		if !completed {
			err = errors.Join(err, s.release(ctx, op.Checkpoint.Id, executor))
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if op.Checkpoint.State == apiv1.CheckpointState_CHECKPOINT_STATE_DELETING {
		err = s.finishDelete(ctx, op, executor)
		completed = err == nil
		return nil, err
	}
	tag, err := s.ensureTag(ctx, op.Checkpoint, op.Snapshot)
	if err != nil && !errors.Is(err, errTagCopyFailed) {
		return nil, publicOperationError(err)
	}
	if err != nil && tag != nil && !validTagSource(tag, op.Checkpoint, op.Snapshot) {
		return nil, serviceerrors.NewFailedPrecondition("Checkpoint snapshot tag identity changed", err)
	}
	failure := ""
	if err != nil {
		failure = "Checkpoint snapshot copy could not be verified"
	}
	// CreateTag's serialized reply established that copying stopped. FAILED can
	// release admission now and retain the UID for a later explicit Delete.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	checkpoint, err := s.store.FinalizeClaimedSessionCheckpoint(finishCtx, op.Checkpoint, executor,
		tag.GetMetadata().GetUid(), substrate.SnapshotURI(tag.GetStatus().GetSnapshot()), failure)
	if err != nil {
		return nil, err
	}
	completed = true
	return createOutcome(checkpoint)
}
