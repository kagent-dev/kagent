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

// errRuntimeUnrecoverable tells the client to delete the Session. Transient
// Substrate and database failures must not wrap it, so they stay retryable.
var errRuntimeUnrecoverable = fmt.Errorf("runtime cannot be recovered: %w", database.ErrFailedPrecondition)

func lifecycleIdle(session *apiv1.Session) bool {
	return session.State == apiv1.RuntimeState_RUNTIME_STATE_READY && session.Operation == apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE
}

type crashObservation struct {
	generation *database.SessionGeneration
	actor      *ateapipb.Actor
}

func (o crashObservation) crashed() bool {
	return o.actor.GetStatus().GetState() == ateapipb.ActorState_ACTOR_STATE_CRASHED
}

// observeCrash treats a missing, replaced, or retemplated Actor as unrecoverable,
// never as a crash: reverting it would restore the wrong runtime.
//
// The generation is read before the Actor. A concurrent revert keeps the Actor UID
// and returns to READY, so only the generation shows that an observation predates
// it. The UID changes only through a lifecycle operation, so admission's locked
// generation check also rejects a stale UID.
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
// Nothing retries an admitted recovery in the background; the caller retries until
// it succeeds. A runtime that is not crashed, including after a revert whose
// response was lost, is returned unchanged.
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
