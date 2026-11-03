package session

import (
	"context"
	"errors"
	"fmt"

	ateapipb "go.ate.dev/substrate/api/ateapi/v1"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// recoverSessionPage retries admitted recoveries in ID order.
// READY Sessions are excluded, so a background retry cannot admit new work.
func (w *ActorWorkflow) recoverSessionPage(ctx context.Context, afterID string) (string, error) {
	const pageSize = 100
	sessions, err := w.store.ListSessionRecoveryCandidates(ctx, afterID, pageSize)
	if err != nil {
		return afterID, err
	}
	for _, session := range sessions {
		attemptCtx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
		err := w.recoverSessionCandidate(attemptCtx, session)
		cancel()
		if err != nil && !errors.Is(err, database.ErrConflict) && ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "recover crashed session", "session_id", session.Id, "error", err)
		}
		afterID = session.Id
		if ctx.Err() != nil {
			return afterID, ctx.Err()
		}
	}
	if len(sessions) < pageSize {
		afterID = ""
	}
	return afterID, nil
}

func (w *ActorWorkflow) recoverSessionCandidate(ctx context.Context, session *apiv1.Session) error {
	_, err := w.Revert(ctx, session)
	if errors.Is(err, database.ErrFailedPrecondition) {
		return nil
	}
	return err
}

// Revert restores a confirmed crashed runtime without replaying its interrupted turn.
// Retries join the same durable operation and preserve the Actor identity.
func (w *ActorWorkflow) Revert(ctx context.Context, session *apiv1.Session) (*apiv1.Session, error) {
	ctx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancel()
	if session.State == apiv1.RuntimeState_RUNTIME_STATE_FAILED && session.Failure.GetReason() == database.SessionRuntimeCrashed {
		return w.run(ctx, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME)
	}
	if session.State != apiv1.RuntimeState_RUNTIME_STATE_READY || session.Operation != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		return nil, database.ErrConflict
	}
	revision, err := w.store.GetRuntimeRevision(ctx, session.PreparedRevision)
	if err != nil {
		return nil, err
	}
	// Read before observing Substrate. A concurrent revert keeps the Actor UID and
	// returns to READY, so only the generation shows that this crash observation
	// predates it.
	generation, err := w.store.SessionLifecycleGeneration(ctx, session.Id)
	if err != nil {
		return nil, err
	}
	actor, err := w.actors.GetActor(ctx, revision.ActorTemplateAtespace, substrate.ActorName(session.Id))
	if err != nil {
		return nil, err
	}
	if err := w.verifyActor(ctx, session, revision, actor); err != nil {
		return nil, err
	}
	if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_CRASHED {
		return nil, fmt.Errorf("runtime is not crashed: %w", database.ErrFailedPrecondition)
	}
	snapshot := substrate.LatestDurableActorSnapshot(actor)
	operation, err := w.store.BeginCrashedSessionRecovery(ctx, session.Id, generation, actor.GetMetadata().GetUid(), database.SessionTaskSnapshot{
		Atespace: actor.GetMetadata().GetAtespace(), URI: substrate.SnapshotURI(snapshot),
		Fidelity: snapshot.GetFidelity().String(),
	})
	if err != nil {
		return nil, err
	}
	return w.execute(ctx, operation)
}
