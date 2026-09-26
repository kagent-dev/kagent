package sandbox

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"golang.org/x/sync/errgroup"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// run executes one attempt inline. Errors retain durable intent; only another
// lifecycle request retries it. Expiration independently requests deletion.
func (s *Service) run(ctx context.Context, id string, kind apiv1alpha1.RuntimeOperation) (_ *apiv1alpha1.Sandbox, err error) {
	ctx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancel()
	op, err := s.config.Store.BeginSandboxOperation(ctx, id, kind)
	if err != nil {
		return nil, sandboxError(err)
	}
	if op.Instance.Operation != kind {
		return op.Instance, nil
	}
	defer func() {
		if err != nil {
			finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			err = errors.Join(err, s.config.Store.RecordSandboxOperationFailure(finishCtx, id, op.ID))
		}
	}()
	revision, err := s.config.Store.GetSandboxRevision(ctx, op.Instance.PreparedRevision)
	if err != nil {
		return nil, sandboxError(err)
	}
	binding := substrate.ActorBinding{Atespace: revision.ActorTemplateAtespace, Name: substrate.ActorName(id),
		TemplateAtespace: revision.ActorTemplateAtespace, TemplateName: revision.ActorTemplateName}
	var creation *substrate.ActorCreation
	if kind == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE {
		policy, err := substrate.ActorEgressPolicy(binding.Atespace, nil, nil)
		if err != nil {
			return nil, sandboxError(err)
		}
		creation = &substrate.ActorCreation{EgressPolicy: policy, Resume: true}
	}
	prepare := substrate.PrepareActorTransition
	if op.ExecutorID != uuid.Nil {
		prepare = substrate.PrepareActorRetry
	}
	transition, err := prepare(ctx, s.config.Actors, binding, kind, creation)
	if err != nil {
		return nil, sandboxError(err)
	}
	executorID := uuid.New()
	claimed, err := s.config.Store.ClaimSandboxOperation(ctx, id, op.ID, executorID)
	if err != nil {
		return nil, sandboxError(err)
	}
	if !claimed {
		return nil, sandboxError(database.ErrConflict)
	}
	defer func() {
		if err == nil {
			return // Completion already cleared the claim.
		}
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, s.config.Store.ReleaseRuntimeOperation(finishCtx, id, op.ID, executorID))
	}()
	if err := substrate.ApplyActorTransition(ctx, s.config.Actors, transition); err != nil {
		return nil, sandboxError(err)
	}
	finishCtx, cancelFinish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelFinish()
	result, err := s.config.Store.FinishSandboxOperation(finishCtx, id, op.ID, executorID, "")
	return result, sandboxError(err)
}

var (
	_ manager.Runnable               = (*Service)(nil)
	_ manager.LeaderElectionRunnable = (*Service)(nil)
)

func (s *Service) NeedLeaderElection() bool { return false }

// Start only deletes expired sandboxes. Ordinary pending lifecycle operations
// are client-driven. Database claims coordinate expiration with inline callers.
func (s *Service) Start(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var afterID string
	for {
		ids, err := s.config.Store.ListExpiredSandboxes(ctx, afterID, 100)
		if err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "list expired sandboxes", "error", err)
		} else {
			var group errgroup.Group
			group.SetLimit(4)
			for _, id := range ids {
				group.Go(func() error {
					if _, err := s.run(ctx, id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE); err != nil && !errors.Is(err, database.ErrConflict) && ctx.Err() == nil {
						logging.FromContext(ctx).ErrorContext(ctx, "expire sandbox", "sandbox_id", id, "error", err)
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
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
