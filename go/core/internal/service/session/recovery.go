package session

import (
	"context"
	"errors"
	"fmt"

	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

// Revert refuses a Session with nothing to act on for one of two reasons. They
// ask opposite things of a client, so the API reports them with distinct reasons.
// errRuntimeNotCrashed means nothing needs recovering: the runtime is running or
// the Session is at rest in another state. A READY Session whose Actor is
// suspended is not refused; Revert returns it, which also covers a retry after a
// revert whose response was lost.
// errRuntimeUnrecoverable means the recorded Actor is gone, was replaced, or no
// longer holds the admitted snapshot. No retry restores it, and the client has
// to start another Session. Transient
// Substrate and database failures wrap neither, so they stay retryable.
var (
	errRuntimeNotCrashed    = fmt.Errorf("runtime is not crashed: %w", database.ErrFailedPrecondition)
	errRuntimeUnrecoverable = fmt.Errorf("runtime cannot be recovered: %w", database.ErrFailedPrecondition)
)

// lifecycleIdle reports a Session with no lifecycle operation in flight. Only an
// idle Session can admit a new revert; any other operation owns the Actor.
func lifecycleIdle(session *apiv1.Session) bool {
	return session.State == apiv1.RuntimeState_RUNTIME_STATE_READY && session.Operation == apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE
}

// crashObservation is the Substrate state of a Session's recorded Actor, taken
// after the lifecycle generation it is valid for.
type crashObservation struct {
	generation *database.SessionGeneration
	actor      *ateapipb.Actor
}

func (o crashObservation) crashed() bool {
	return o.actor.GetStatus().GetState() == ateapipb.ActorState_ACTOR_STATE_CRASHED
}

// reverted reports a recorded Actor at rest, as a completed revert leaves it.
func (o crashObservation) reverted() bool {
	return o.actor.GetStatus().GetState() == ateapipb.ActorState_ACTOR_STATE_SUSPENDED
}

// observeCrash reads the Session's recorded Actor and reports whether Substrate
// confirms it crashed. Revert admits recovery only through this check. An Actor
// that is missing, replaced, or from another template is an error, never a
// crash: reverting it would restore the wrong runtime. Those errors wrap
// errRuntimeUnrecoverable; transient read failures do not.
//
// The generation is read before the Actor. A concurrent revert keeps the Actor UID
// and returns to READY, so only the generation shows that a crash observation
// predates it. The recorded UID changes only through a lifecycle operation,
// which also replaces the generation, so admission's locked generation check
// rejects an observation compared against a stale UID.
func (w *ActorWorkflow) observeCrash(ctx context.Context, session *apiv1.Session) (crashObservation, error) {
	generation, err := w.store.GetSessionGeneration(ctx, session.Id)
	if errors.Is(err, database.ErrNotFound) {
		return crashObservation{}, fmt.Errorf("runtime Session was deleted: %w", errRuntimeUnrecoverable)
	}
	if err != nil {
		return crashObservation{}, err
	}
	binding := substrate.ActorBinding{Atespace: generation.TemplateAtespace, Name: substrate.ActorName(session.Id),
		TemplateAtespace: generation.TemplateAtespace, TemplateName: generation.TemplateName}
	actor, err := w.actors.GetActor(ctx, binding.Atespace, binding.Name)
	if status.Code(err) == codes.NotFound {
		return crashObservation{}, fmt.Errorf("runtime Actor is missing: %w", errRuntimeUnrecoverable)
	}
	if err != nil {
		return crashObservation{}, err
	}
	if !binding.Matches(actor) {
		return crashObservation{}, fmt.Errorf("runtime Actor identity or template changed: %w", errRuntimeUnrecoverable)
	}
	if actor.GetMetadata().GetUid() != generation.ActorUID {
		return crashObservation{}, fmt.Errorf("runtime Actor was replaced: %w", errRuntimeUnrecoverable)
	}
	return crashObservation{generation: generation, actor: actor}, nil
}

// Revert restores a confirmed crashed runtime without replaying its interrupted turn.
// Retries join the same durable operation and preserve the Actor identity. Nothing
// retries an admitted recovery in the background: the caller retries RevertSession
// until it succeeds, as with CreateSession and DeleteSession.
func (w *ActorWorkflow) Revert(ctx context.Context, session *apiv1.Session) (*apiv1.Session, error) {
	if database.CrashRecoveryAdmitted(session) {
		return w.run(ctx, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_REVERT, false)
	}
	if session.State == apiv1.RuntimeState_RUNTIME_STATE_FAILED && session.Operation == apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE &&
		session.Failure.GetReason() == database.SessionRuntimeUnrecoverable {
		return nil, fmt.Errorf("%s: %w", session.Failure.GetMessage(), errRuntimeUnrecoverable)
	}
	// run bounds a joined attempt. Admission bounds its own reads and execution.
	ctx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancel()
	// Only an active lifecycle operation is contention: a retry converges once it
	// finishes. A Session at rest in another state, such as one suspended by
	// deadline cleanup, stays there until an interaction resumes it, so a retry
	// would spin until the caller's deadline.
	if session.Operation != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		return nil, database.ErrConflict
	}
	if !lifecycleIdle(session) {
		return nil, fmt.Errorf("runtime state is %s: %w", session.State, errRuntimeNotCrashed)
	}
	observed, err := w.observeCrash(ctx, session)
	if err != nil {
		return nil, err
	}
	if observed.reverted() {
		// A READY Session whose recorded Actor is at rest has nothing to recover.
		// This is also what a retry sees after a revert whose response was lost,
		// so it returns the Session the way Create and Delete retries do.
		return session, nil
	}
	if !observed.crashed() {
		return nil, errRuntimeNotCrashed
	}
	actor := observed.actor
	snapshot := substrate.LatestDurableActorSnapshot(actor)
	operation, err := w.store.BeginCrashedSessionRecovery(ctx, observed.generation, database.SessionTaskSnapshot{
		Atespace: actor.GetMetadata().GetAtespace(), URI: substrate.SnapshotURI(snapshot),
		Fidelity: snapshot.GetFidelity().String(),
	})
	if err != nil {
		return nil, err
	}
	return w.execute(ctx, operation)
}
