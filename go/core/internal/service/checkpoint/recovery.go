package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// RecoveryWorker is the sole owner of checkpoint tag mutations. API replicas
// admit durable work and wait for its outcome; losing a caller does not cancel
// the copy. On restart or leader change the worker resumes persisted phases.
type RecoveryWorker struct {
	service  *Service
	interval time.Duration
}

var _ manager.Runnable = (*RecoveryWorker)(nil)
var _ manager.LeaderElectionRunnable = (*RecoveryWorker)(nil)

func NewRecoveryWorker(service *Service) *RecoveryWorker {
	return &RecoveryWorker{service: service, interval: time.Second}
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
		case <-w.service.wake:
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
		var group errgroup.Group
		group.SetLimit(4)
		for _, op := range operations {
			group.Go(func() error {
				attempt, cancel := context.WithTimeout(ctx, 2*time.Minute)
				defer cancel()
				if err := w.service.recoverOperation(attempt, op); err != nil && ctx.Err() == nil {
					logging.FromContext(ctx).ErrorContext(ctx, "recover checkpoint operation", "checkpoint_id", op.Checkpoint.Id, "error", err)
				}
				return nil
			})
		}
		if err := group.Wait(); err != nil {
			return err
		}
		if len(operations) < 100 {
			return nil
		}
		after = operations[len(operations)-1].Checkpoint.Id
	}
	return ctx.Err()
}

func (s *Service) recoverOperation(ctx context.Context, op *database.CheckpointOperation) error {
	switch op.Checkpoint.State {
	case apiv1.CheckpointState_CHECKPOINT_STATE_DELETING:
		return s.finishDelete(ctx, op)
	case apiv1.CheckpointState_CHECKPOINT_STATE_CREATING:
		if op.Checkpoint.Failure != nil {
			return s.finishCleanup(ctx, op)
		}
		tag, err := s.ensureTag(ctx, op.Checkpoint, op.Snapshot)
		if err == nil {
			_, err = s.store.FinalizeSessionCheckpoint(ctx, op.Checkpoint.Id,
				tag.GetMetadata().GetUid(), substrate.SnapshotURI(tag.GetStatus().GetSnapshot()), "")
			return err
		}
		if !errors.Is(err, errTagCopyFailed) {
			return err
		}
		if tag != nil && !validTagSource(tag, op.Checkpoint, op.Snapshot) {
			return fmt.Errorf("refuse cleanup of an unverified checkpoint tag: %w", err)
		}
		uid := tag.GetMetadata().GetUid()
		if recordErr := s.store.BeginCheckpointCleanup(ctx, op.Checkpoint.Id, uid, err.Error()); recordErr != nil {
			return recordErr
		}
		// Persist the exact phase before external cleanup. A restart must not
		// mistake the same tag for a copy that can still become READY.
		op, err = s.store.GetCheckpointOperation(ctx, op.Checkpoint.Id, op.Creator)
		if err != nil {
			return err
		}
		return s.finishCleanup(ctx, op)
	default:
		return nil
	}
}

func (s *Service) finishCleanup(ctx context.Context, op *database.CheckpointOperation) error {
	name := tagName(op.Checkpoint.Id)
	tag, err := s.tags.GetTag(ctx, op.Snapshot.Atespace, name)
	if err != nil && status.Code(err) != codes.NotFound {
		return err
	}
	if err == nil {
		if op.TagUID == "" || tag.GetMetadata().GetUid() != op.TagUID || !validTagSource(tag, op.Checkpoint, op.Snapshot) {
			return status.Error(codes.FailedPrecondition, "checkpoint cleanup tag identity changed")
		}
		if err := s.tags.DeleteTag(ctx, op.Snapshot.Atespace, name, op.TagUID); err != nil && status.Code(err) != codes.NotFound {
			return err
		}
	}
	_, err = s.store.FinalizeSessionCheckpoint(ctx, op.Checkpoint.Id, "", "", op.Checkpoint.Failure.Message)
	return err
}
