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

// errRuntimeUnrecoverable is Revert's only refusal: the recorded Actor is gone,
// was replaced, or no longer holds the admitted snapshot. No retry restores it,
// and the client deletes the Session. Transient Substrate and database failures
// do not wrap it, so they stay retryable.
var errRuntimeUnrecoverable = fmt.Errorf("runtime cannot be recovered: %w", database.ErrFailedPrecondition)

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
// until it succeeds, as with CreateSession and DeleteSession. A runtime that is
// not crashed has nothing to recover, so Revert returns the Session unchanged.
// That is also what a retry sees after a revert whose response was lost.
func (w *ActorWorkflow) Revert(ctx context.Context, session *apiv1.Session) (*apiv1.Session, error) {
	if database.CrashRecoveryAdmitted(session) {
		return w.run(ctx, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_REVERT, false)
	}
	// Another lifecycle operation owns the Actor; a retry converges once it finishes.
	if session.Operation != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		return nil, database.ErrConflict
	}
	if !lifecycleIdle(session) {
		return session, nil
	}
	// run bounds a joined attempt. Admission bounds its own reads and execution.
	ctx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancel()
	observed, err := w.observeCrash(ctx, session)
	if err != nil {
		return nil, err
	}
	if !observed.crashed() {
		return session, nil
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
