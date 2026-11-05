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
// the Session is in another lifecycle state.
// errRuntimeUnrecoverable means the recorded Actor is gone or was replaced. No
// retry restores it, and the client has to start another Session. Transient
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
// Retries join the same durable operation and preserve the Actor identity; a
// client retry is what continues an interrupted recovery. A retry after a revert
// whose response was lost finds the same Actor SUSPENDED and returns the Session.
func (w *ActorWorkflow) Revert(ctx context.Context, session *apiv1.Session) (*apiv1.Session, error) {
	if database.CrashRecoveryAdmitted(session) {
		return w.run(ctx, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_REVERT, false)
	}
	if session.State == apiv1.RuntimeState_RUNTIME_STATE_FAILED && session.Failure.GetReason() == database.SessionRuntimeUnrecoverable {
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
	switch observed.actor.GetStatus().GetState() {
	case ateapipb.ActorState_ACTOR_STATE_CRASHED:
	case ateapipb.ActorState_ACTOR_STATE_SUSPENDED:
		// The recorded Actor rests at a snapshot, which is what a revert leaves.
		return session, nil
	default:
		return nil, errRuntimeNotCrashed
	}
	operation, err := w.store.BeginCrashedSessionRecovery(ctx, observed.generation)
	if err != nil {
		return nil, err
	}
	return w.execute(ctx, operation)
}

// closeLostRecovery lets Delete proceed past an admitted recovery that can never
// finish because its Actor is gone or was replaced. An Actor that is still there,
// or a failed read, leaves the recovery for Delete to supersede or wait for.
func (w *ActorWorkflow) closeLostRecovery(ctx context.Context, sessionID string) error {
	operation, err := w.store.BeginSessionOperation(ctx, sessionID, apiv1.RuntimeOperation_RUNTIME_OPERATION_REVERT, false)
	if err != nil || !database.CrashRecoveryAdmitted(operation.Instance) {
		return nil // Delete reports the Session's current state.
	}
	binding, err := w.actorBinding(ctx, operation.Instance)
	if err != nil {
		return nil
	}
	if _, err := w.prepareRevert(ctx, operation.Instance, binding); errors.Is(err, errRuntimeUnrecoverable) {
		_, err = w.store.AbandonCrashedSessionRecovery(ctx, sessionID, operation.ID, err.Error())
		return err
	}
	return nil
}

// prepareRevert reads the crashed Actor before any claim. A missing or replaced
// Actor wraps errRuntimeUnrecoverable.
func (w *ActorWorkflow) prepareRevert(ctx context.Context, session *apiv1.Session, binding substrate.ActorBinding) (*substrate.ActorTransition, error) {
	transition, err := substrate.PrepareActorRevert(ctx, w.actors, binding)
	if errors.Is(err, substrate.ErrActorLost) {
		return nil, fmt.Errorf("%w: %w", err, errRuntimeUnrecoverable)
	}
	if err != nil {
		return nil, err
	}
	_, err = w.store.GetSessionForRuntime(ctx, session.Id, transition.ActorUID())
	if errors.Is(err, database.ErrNotFound) {
		return nil, fmt.Errorf("runtime Actor was replaced: %w", errRuntimeUnrecoverable)
	}
	if err != nil {
		return nil, fmt.Errorf("verify runtime actor UID: %w", err)
	}
	return transition, nil
}
