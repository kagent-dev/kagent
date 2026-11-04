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

// crashRecoveryAdmitted reports a revert already admitted for a confirmed crash.
// Revert continues it without observing Substrate again.
func crashRecoveryAdmitted(session *apiv1.Session) bool {
	return session.State == apiv1.RuntimeState_RUNTIME_STATE_FAILED && session.Failure.GetReason() == database.SessionRuntimeCrashed
}

// lifecycleIdle reports a Session with no lifecycle operation in flight. Only an
// idle Session can admit a new revert; any other operation owns the Actor.
func lifecycleIdle(session *apiv1.Session) bool {
	return session.State == apiv1.RuntimeState_RUNTIME_STATE_READY && session.Operation == apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE
}

// observeCrash reads the Session's recorded Actor and reports whether Substrate
// confirms it crashed. Revert admits recovery only through this check. An Actor
// that is missing, replaced, or from another template is an error, never a
// crash: reverting it would restore the wrong runtime. Those errors wrap
// errRuntimeUnrecoverable; transient read failures do not.
func (w *ActorWorkflow) observeCrash(ctx context.Context, session *apiv1.Session) (*ateapipb.Actor, bool, error) {
	revision, err := w.store.GetRuntimeRevision(ctx, session.PreparedRevision)
	if err != nil {
		return nil, false, err
	}
	actor, err := w.actors.GetActor(ctx, revision.ActorTemplateAtespace, substrate.ActorName(session.Id))
	if status.Code(err) == codes.NotFound {
		return nil, false, fmt.Errorf("runtime Actor is missing: %w", errRuntimeUnrecoverable)
	}
	if err != nil {
		return nil, false, err
	}
	if !validActorIdentity(actor, revision, substrate.ActorName(session.Id)) {
		return nil, false, fmt.Errorf("runtime Actor identity or template changed: %w", errRuntimeUnrecoverable)
	}
	if err := w.verifyActor(ctx, session, revision, actor); errors.Is(err, database.ErrNotFound) {
		return nil, false, fmt.Errorf("runtime Actor was replaced: %w", errRuntimeUnrecoverable)
	} else if err != nil {
		return nil, false, err
	}
	return actor, actor.GetStatus().GetState() == ateapipb.ActorState_ACTOR_STATE_CRASHED, nil
}

// scanRecovery runs one recovery interval and returns the cursor for the next.
// Admitted recoveries page through PostgreSQL in ID order.
func (w *ActorWorkflow) scanRecovery(ctx context.Context, afterID string) string {
	scanCtx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	after, err := w.recoverSessionPage(scanCtx, afterID)
	cancel()
	if err != nil && ctx.Err() == nil {
		logging.FromContext(ctx).ErrorContext(ctx, "scan interrupted sessions", "error", err)
	}
	return after
}

// recoverSessionPage retries one ID-ordered page of admitted recoveries.
// READY Sessions are excluded, so a background retry cannot admit new work.
func (w *ActorWorkflow) recoverSessionPage(ctx context.Context, afterID string) (string, error) {
	const pageSize = 100
	sessions, err := w.store.ListSessionRecoveryCandidates(ctx, afterID, pageSize)
	if err != nil {
		return afterID, err
	}
	for _, session := range sessions {
		w.recoverCandidate(ctx, session)
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

func (w *ActorWorkflow) recoverCandidate(ctx context.Context, session *apiv1.Session) {
	attemptCtx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	err := w.recoverSessionCandidate(attemptCtx, session)
	cancel()
	if err != nil && !errors.Is(err, database.ErrConflict) && ctx.Err() == nil {
		logging.FromContext(ctx).ErrorContext(ctx, "recover crashed session", "session_id", session.Id, "error", err)
	}
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
	if crashRecoveryAdmitted(session) {
		return w.run(ctx, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME, false)
	}
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
	// Read before observing Substrate. A concurrent revert keeps the Actor UID and
	// returns to READY, so only the generation shows that this crash observation
	// predates it.
	generation, err := w.store.SessionLifecycleGeneration(ctx, session.Id)
	if err != nil {
		return nil, err
	}
	actor, crashed, err := w.observeCrash(ctx, session)
	if err != nil {
		return nil, err
	}
	if !crashed {
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
