package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// recoverSessionPage observes only accepted active turns and already admitted
// recoveries. A quiet runtime or a failed read is never evidence that execution
// stopped. Pages advance even past healthy Actors so one long turn cannot hide
// another Session's crash. Only one existing lifecycle worker performs this scan.
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
	if session.State == apiv1.RuntimeState_RUNTIME_STATE_FAILED {
		if session.Failure.GetReason() != database.SessionRuntimeCrashed {
			return nil
		}
		operation, err := w.store.BeginSessionOperation(ctx, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME)
		if err != nil {
			return err
		}
		_, err = w.execute(ctx, operation)
		return err
	}
	revision, err := w.store.GetRuntimeRevision(ctx, session.PreparedRevision)
	if err != nil {
		return err
	}
	actor, err := w.actors.GetActor(ctx, revision.ActorTemplateAtespace, substrate.ActorName(session.Id))
	if err != nil {
		return err
	}
	if err := w.verifyActor(ctx, session, revision, actor); err != nil {
		return err
	}
	if actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_CRASHED {
		return nil
	}
	operation, err := w.store.BeginCrashedSessionRecovery(ctx, session.Id, actor.GetMetadata().GetUid())
	if err != nil {
		return err
	}
	_, err = w.execute(ctx, operation)
	return err
}

// recoverCrashedSession executes only admitted recovery, using the ordinary
// bounded executor claim. A retry reads the same immutable Actor: REVERTING
// continues Substrate's workflow, while SUSPENDED proves a lost revert response
// completed. Neither an RPC timeout nor a changed Actor authorizes fresh work.
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
	if !validActorIdentity(actor, revision, name) || actor.GetMetadata().GetUid() != before.GetMetadata().GetUid() || actor.GetStatus().GetState() != ateapipb.ActorState_ACTOR_STATE_SUSPENDED || !proto.Equal(before.GetStatus().GetExternalSnapshot(), actor.GetStatus().GetExternalSnapshot()) {
		return nil, fmt.Errorf("revert Actor %s/%s returned unexpected identity or state", atespace, name)
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return w.store.FinishSessionOperation(finishCtx, session.Id, operation.ID, executorID, "", actor.GetMetadata().GetUid(), "")
}
