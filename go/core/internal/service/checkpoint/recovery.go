package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

type checkpointKey struct{ id, owner string }

type checkpointRetry struct {
	backoff retryBackoff
	readyAt time.Time
}

// RecoveryWorker is the sole owner of checkpoint tag mutations. API replicas
// admit durable work and wait for its outcome; losing a caller does not cancel
// the copy. On restart or leader change the worker resumes persisted phases.
type RecoveryWorker struct {
	service  *Service
	interval time.Duration
	queue    workqueue.TypedDelayingInterface[checkpointKey]
	mu       sync.Mutex
	retries  map[checkpointKey]*checkpointRetry
}

var _ manager.Runnable = (*RecoveryWorker)(nil)
var _ manager.LeaderElectionRunnable = (*RecoveryWorker)(nil)

func NewRecoveryWorker(service *Service) *RecoveryWorker {
	return &RecoveryWorker{service: service, interval: time.Second, retries: make(map[checkpointKey]*checkpointRetry)}
}

func (*RecoveryWorker) NeedLeaderElection() bool { return true }

func (w *RecoveryWorker) Start(ctx context.Context) error {
	if w.service.activation != nil {
		select {
		case <-ctx.Done():
			return nil
		case <-w.service.activation:
		}
	}
	w.queue = workqueue.NewTypedDelayingQueueWithConfig(workqueue.TypedDelayingQueueConfig[checkpointKey]{Clock: w.service.clock})
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for w.process(ctx) {
			}
		})
	}
	ticker := w.service.clock.NewTicker(w.interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := w.sweep(ctx); err != nil && ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "discover checkpoints", "error", err)
		}
		select {
		case <-ctx.Done():
		case <-ticker.C():
		case <-w.service.wake:
		}
	}
	w.queue.ShutDown()
	workers.Wait()
	return nil
}

// Discovery never waits for a copy. The keyed queue deduplicates IDs and keeps
// spare workers available to requests arriving behind a slow operation.
func (w *RecoveryWorker) sweep(ctx context.Context) error {
	var after string
	for ctx.Err() == nil {
		operations, err := w.service.store.ListPendingCheckpoints(ctx, after, 100)
		if err != nil {
			return err
		}
		for _, op := range operations {
			w.enqueue(checkpointKey{id: op.Checkpoint.Id, owner: op.Creator})
		}
		if len(operations) < 100 {
			return nil
		}
		after = operations[len(operations)-1].Checkpoint.Id
	}
	return ctx.Err()
}

func (w *RecoveryWorker) enqueue(key checkpointKey) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if retry := w.retries[key]; retry != nil {
		if delay := retry.readyAt.Sub(w.service.clock.Now()); delay > 0 {
			w.queue.AddAfter(key, delay)
			return
		}
	}
	w.queue.Add(key)
}

func (w *RecoveryWorker) process(ctx context.Context) bool {
	key, shutdown := w.queue.Get()
	if shutdown {
		return false
	}
	defer w.queue.Done(key)
	// A scan can mark a running key dirty just before it fails. Recheck here so
	// Done's immediate requeue cannot bypass the newly established retry delay.
	w.mu.Lock()
	if retry := w.retries[key]; retry != nil {
		if delay := retry.readyAt.Sub(w.service.clock.Now()); delay > 0 {
			w.queue.AddAfter(key, delay)
			w.mu.Unlock()
			return true
		}
	}
	w.mu.Unlock()
	err := w.service.recoverOperation(ctx, &database.CheckpointOperation{Checkpoint: &apiv1.Checkpoint{Id: key.id}, Creator: key.owner})
	w.mu.Lock()
	defer w.mu.Unlock()
	if err == nil {
		delete(w.retries, key)
		return true
	}
	if ctx.Err() != nil {
		return false
	}
	retry := w.retries[key]
	if retry == nil {
		retry = &checkpointRetry{backoff: retryBackoff{delay: time.Second, cap: 30 * time.Second}}
		w.retries[key] = retry
	}
	delay := retry.backoff.next()
	retry.readyAt = w.service.clock.Now().Add(delay)
	w.queue.AddAfter(key, delay)
	logging.FromContext(ctx).ErrorContext(ctx, "recover checkpoint operation", "checkpoint_id", key.id, "retry_after", delay, "error", err)
	return true
}

func (s *Service) recoverOperation(ctx context.Context, observed *database.CheckpointOperation) (err error) {
	// Establish the attempt deadline before claiming. A paused executor cannot
	// issue another RPC after its claim expires; unfinished copies synchronize
	// through Substrate's tag lease when the replacement retries CreateTag.
	ctx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancel()
	executor := uuid.New()
	id := observed.Checkpoint.Id
	// The claim reply can be lost after the UPDATE committed. Release by the
	// known executor even when acquisition returns an error; this cannot revoke
	// another attempt and avoids waiting for expiry on graceful cancellation.
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, s.store.ReleaseCheckpointOperation(releaseCtx, id, executor))
	}()
	op, err := s.store.ClaimCheckpointOperation(ctx, id, observed.Creator, executor)
	if errors.Is(err, database.ErrConflict) {
		return nil // Another executor owns it, or the listed phase already ended.
	}
	if err != nil {
		return err
	}
	defer func() {
		if failure := permanentOperationError(err); failure != nil {
			recordCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer stop()
			err = errors.Join(err, s.store.RecordCheckpointOperationError(recordCtx, id, executor, failure.Proto()))
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	switch op.Checkpoint.State {
	case apiv1.CheckpointState_CHECKPOINT_STATE_DELETING:
		return s.finishDelete(ctx, op, executor)
	case apiv1.CheckpointState_CHECKPOINT_STATE_CREATING:
		if op.Checkpoint.Failure != nil {
			return s.finishCleanup(ctx, op, executor)
		}
		tag, err := s.ensureTag(ctx, op.Checkpoint, op.Snapshot)
		if err == nil {
			_, err = s.store.FinalizeClaimedSessionCheckpoint(ctx, op.Checkpoint.Id, executor,
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
		if recordErr := s.store.BeginCheckpointCleanup(ctx, op.Checkpoint.Id, executor, uid, err.Error()); recordErr != nil {
			return recordErr
		}
		// Persist the exact phase before external cleanup. A restart must not
		// mistake the same tag for a copy that can still become READY.
		op, err = s.store.GetCheckpointOperation(ctx, op.Checkpoint.Id, op.Creator)
		if err != nil {
			return err
		}
		return s.finishCleanup(ctx, op, executor)
	default:
		return nil
	}
}

func (s *Service) finishCleanup(ctx context.Context, op *database.CheckpointOperation, executor uuid.UUID) error {
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
	_, err = s.store.FinalizeClaimedSessionCheckpoint(ctx, op.Checkpoint.Id, executor, "", "", op.Checkpoint.Failure.Message)
	return err
}
