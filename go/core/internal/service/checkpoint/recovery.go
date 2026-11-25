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
	for ctx.Err() == nil {
		if err := w.sweep(ctx); err != nil && ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "recover checkpoints", "error", err)
		}
		// Start the delay after the sweep: a ticker can retain a stale tick
		// during slow recovery and immediately retry the last failed attempt.
		timer := time.NewTimer(w.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
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
func (s *Service) claimAndExecute(ctx context.Context, observed *database.CheckpointOperation) (*apiv1.Checkpoint, error) {
	executor := uuid.New()
	id, owner := observed.Checkpoint.Id, observed.Creator
	op, err := s.store.ClaimCheckpointOperation(ctx, id, owner, executor)
	if errors.Is(err, database.ErrConflict) {
		// A definite conflict acquired nothing. Do not let an unrelated release
		// failure replace the outcome this caller can only observe.
		return s.currentOutcome(ctx, id, owner)
	}
	if err != nil {
		if definiteAcquisitionError(err) {
			return nil, publicOperationError(err)
		}
		// A lost COMMIT reply can hide successful acquisition. Retire this
		// executor before returning the uncertain error, as on reservation.
		return nil, errors.Join(publicOperationError(err), s.release(ctx, id, executor))
	}
	return s.execute(ctx, op, executor, false)
}

// Losing execution authority permits observation, never another backend call.
// Absence lets Delete succeed; Create translates it to NotFound at its boundary.
func (s *Service) currentOutcome(ctx context.Context, id, owner string) (*apiv1.Checkpoint, error) {
	current, err := s.store.GetCheckpointOperation(ctx, id, owner)
	if errors.Is(err, database.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to observe checkpoint operation", err)
	}
	return createOutcome(current.Checkpoint)
}

func (s *Service) release(ctx context.Context, id string, executor uuid.UUID) error {
	return releaseClaim(ctx, id, func(releaseCtx context.Context) error {
		return s.store.ReleaseCheckpointOperation(releaseCtx, id, executor)
	})
}

// Release stays bounded after caller cancellation, and neither release path may
// expose storage details when its error is joined to the public attempt result.
func releaseClaim(ctx context.Context, id string, release func(context.Context) error) error {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := release(releaseCtx); err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "release checkpoint execution claim", "checkpoint_id", id, "error", err)
		// This error is joined to the current attempt's public status. Keep
		// database details in logs without replacing the original status code.
		return errors.New("checkpoint execution claim could not be released")
	}
	return nil
}

func (s *Service) execute(ctx context.Context, op *database.CheckpointOperation, executor uuid.UUID, firstAttempt bool) (result *apiv1.Checkpoint, err error) {
	releaseNeeded := true
	defer func() {
		if releaseNeeded {
			err = errors.Join(err, s.release(ctx, op.Checkpoint.Id, executor))
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if op.Checkpoint.State == apiv1.CheckpointState_CHECKPOINT_STATE_DELETING {
		err = s.finishDelete(ctx, op, executor)
		releaseNeeded = err != nil
		return nil, err
	}
	if op.Checkpoint.State == apiv1.CheckpointState_CHECKPOINT_STATE_FAILED {
		err = s.collectFailedTag(ctx, op, executor)
		releaseNeeded = err != nil
		return nil, err
	}
	tag, err := s.ensureTag(ctx, op.Checkpoint, op.Snapshot, firstAttempt)
	if err != nil && !errors.Is(err, errTagCopyFailed) {
		return nil, publicOperationError(err)
	}
	if err != nil && tag != nil && !validTagSource(tag, op.Checkpoint, op.Snapshot) {
		return nil, serviceerrors.NewFailedPrecondition("Checkpoint snapshot tag identity changed", err)
	}
	failure := ""
	if err != nil {
		failure = checkpointFailureMessage
	}
	// FAILED releases source admission; the sweep collects its retained tag by UID.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	checkpoint, err := s.store.FinalizeClaimedSessionCheckpoint(finishCtx, op.Checkpoint, executor,
		tag.GetMetadata().GetUid(), substrate.SnapshotURI(tag.GetStatus().GetSnapshot()), failure)
	if errors.Is(err, database.ErrConflict) {
		// The conditional write proves authority was lost. This is now an
		// observer; a stale release must not override the result it reads.
		releaseNeeded = false
		return s.currentOutcome(finishCtx, op.Checkpoint.Id, op.Creator)
	}
	if err != nil {
		return nil, publicOperationError(err)
	}
	releaseNeeded = false
	return createOutcome(checkpoint)
}

func (s *Service) collectFailedTag(ctx context.Context, op *database.CheckpointOperation, executor uuid.UUID) error {
	if err := s.deleteTag(ctx, op); err != nil {
		return err
	}
	err := s.store.ClearFailedCheckpointTag(ctx, op.Checkpoint.Id, op.Creator, op.TagUID, executor)
	if errors.Is(err, database.ErrConflict) {
		current, readErr := s.store.GetCheckpointOperation(ctx, op.Checkpoint.Id, op.Creator)
		if errors.Is(readErr, database.ErrNotFound) || (readErr == nil && current.TagUID == "") {
			return nil
		}
		if readErr != nil {
			return serviceerrors.NewInternal("Failed to observe checkpoint cleanup", readErr)
		}
		return serviceerrors.NewAborted("Checkpoint cleanup is still pending", nil)
	}
	if err != nil {
		return serviceerrors.NewInternal("Failed to collect checkpoint tag", err)
	}
	return nil
}
