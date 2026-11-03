package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// Revert refuses a Session with nothing to act on for one of two reasons. They
// ask opposite things of a client, so the API reports them with distinct reasons.
// errRuntimeNotCrashed means nothing needs recovering: the runtime is healthy or
// suspended. A retry after a revert whose response was lost lands here, and the
// next interaction proceeds.
// errRuntimeUnrecoverable means the recorded Actor is gone or was replaced. No
// retry restores it, and the client has to start another Session. Transient
// Substrate and database failures wrap neither, so they stay retryable.
var (
	errRuntimeNotCrashed    = fmt.Errorf("runtime is not crashed: %w", database.ErrFailedPrecondition)
	errRuntimeUnrecoverable = fmt.Errorf("runtime cannot be recovered: %w", database.ErrFailedPrecondition)
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
	// Only an active lifecycle operation is contention: a retry converges once it
	// finishes. A Session at rest in another state, such as one suspended by
	// deadline cleanup, stays there until an interaction resumes it, so a retry
	// would spin until the caller's deadline.
	if session.Operation != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		return nil, database.ErrConflict
	}
	if session.State != apiv1.RuntimeState_RUNTIME_STATE_READY {
		return nil, fmt.Errorf("runtime state is %s: %w", session.State, errRuntimeNotCrashed)
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
	if status.Code(err) == codes.NotFound {
		return nil, fmt.Errorf("runtime Actor is missing: %w", errRuntimeUnrecoverable)
	}
	if err != nil {
		return nil, err
	}
	if !validActorIdentity(actor, revision, substrate.ActorName(session.Id)) {
		return nil, fmt.Errorf("runtime Actor identity or template changed: %w", errRuntimeUnrecoverable)
	}
	if err := w.verifyActor(ctx, session, revision, actor); errors.Is(err, database.ErrNotFound) {
		return nil, fmt.Errorf("runtime Actor was replaced: %w", errRuntimeUnrecoverable)
	} else if err != nil {
		return nil, err
	}
	if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_CRASHED {
		return nil, errRuntimeNotCrashed
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

// recoverCrashedSession reconciles lost revert responses without replacing the Actor.
func (w *ActorWorkflow) recoverCrashedSession(ctx context.Context, operation *database.SessionOperation) (_ *apiv1.Session, err error) {
	session := operation.Instance
	if session.Operation != apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME {
		return nil, database.ErrConflict
	}
	executorID := uuid.New()
	claimed, err := w.store.ClaimSessionOperation(ctx, session.Id, operation.ID, executorID)
	if err != nil {
		return nil, err
	}
	if !claimed {
		current, err := w.store.GetSessionOperation(ctx, session.Id, operation.ID)
		if err != nil {
			return nil, err
		}
		return operationOutcome(current)
	}
	defer func() {
		if err != nil {
			releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			err = errors.Join(err, w.store.ReleaseRuntimeOperation(releaseCtx, session.Id, operation.ID, executorID))
		}
	}()
	revision, err := w.store.GetRuntimeRevision(ctx, session.PreparedRevision)
	if err != nil {
		return nil, err
	}
	atespace, name := revision.ActorTemplateAtespace, substrate.ActorName(session.Id)
	actor, err := w.actors.GetActor(ctx, atespace, name)
	if err != nil {
		return nil, err
	}
	if err := w.verifyActor(ctx, session, revision, actor); err != nil {
		return nil, err
	}
	before := actor
	beforeSnapshot := proto.CloneOf(substrate.LatestDurableActorSnapshot(actor))
	if beforeSnapshot != nil {
		beforeSnapshot.Locality = ""
	}
	switch actor.GetStatus().GetState() {
	case ateapipb.ActorState_ACTOR_STATE_CRASHED, ateapipb.ActorState_ACTOR_STATE_REVERTING:
		actor, err = w.actors.RevertActor(ctx, atespace, name)
		if err != nil {
			return nil, fmt.Errorf("revert crashed Actor %s/%s: %w", atespace, name, err)
		}
	case ateapipb.ActorState_ACTOR_STATE_SUSPENDED:
	default:
		return nil, fmt.Errorf("crashed Actor %s/%s has unexpected state %s", atespace, name, actor.GetStatus().GetState())
	}
	if !validActorIdentity(actor, revision, name) || actor.GetMetadata().GetUid() != before.GetMetadata().GetUid() || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED || !proto.Equal(beforeSnapshot, substrate.LatestActorSnapshot(actor)) {
		return nil, fmt.Errorf("revert Actor %s/%s returned unexpected identity or state", atespace, name)
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return w.store.FinishSessionOperation(finishCtx, session.Id, operation.ID, executorID, "", actor.GetMetadata().GetUid(), "")
}
