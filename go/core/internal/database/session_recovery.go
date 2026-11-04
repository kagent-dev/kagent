package database

import (
	"context"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// SessionRuntimeCrashed identifies a recovery admitted after Substrate confirmed
// that the recorded Actor crashed. Transport failures cannot admit this work.
const SessionRuntimeCrashed = "RuntimeCrashed"

// ListSessionRecoveryCandidates finds recoveries admitted by an explicit request.
// Retrying durable work cannot admit recovery for a READY Session.
func (c *Client) ListSessionRecoveryCandidates(ctx context.Context, afterID string, limit int) ([]*apiv1.Session, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT id, user_id, prepared_revision, state, data, operation, context_id,
		    source_checkpoint_id, history_id, operation_id, executor_id
		FROM session_record i
		WHERE ($1::text = '' OR i.id > $1::uuid)
		  AND i.state = 'RUNTIME_STATE_FAILED' AND i.operation = 'RUNTIME_OPERATION_RESUME'
		ORDER BY i.id LIMIT $2
	`, pgx.RowToStructByName[sessionRow], afterID, limit)
	if err != nil {
		return nil, err
	}
	result := make([]*apiv1.Session, 0, len(rows))
	for _, row := range rows {
		session, err := toSession(row)
		if err != nil {
			return nil, err
		}
		result = append(result, session)
	}
	return result, nil
}

// SessionRecoveryFence is what crash recovery reads before it observes Substrate.
// Generation is the ID of the Session's latest lifecycle operation, or uuid.Nil
// when none is recorded. Every operation replaces it, and completion keeps it.
// ActorUID is the recorded runtime Actor, or "" when none is recorded. The
// template identifies where that Actor lives and what it must run.
type SessionRecoveryFence struct {
	Generation       uuid.UUID
	ActorUID         string
	TemplateAtespace string
	TemplateName     string
}

// SessionRecoveryFence reads the lifecycle generation, recorded Actor UID, and
// Actor template of a live Session in one statement. Deleted or missing Sessions
// return ErrNotFound. Callers authorize access. The read takes no lock: admission
// re-checks the generation and Actor UID under the Session lock.
func (c *Client) SessionRecoveryFence(ctx context.Context, id string) (*SessionRecoveryFence, error) {
	type fenceRow struct {
		OperationID           *uuid.UUID
		ActorUID              *string
		ActorTemplateAtespace string
		ActorTemplateName     string
	}
	row, err := queryOne(ctx, c.db, `
		SELECT i.operation_id, i.actor_uid, r.actor_template_atespace, r.actor_template_name
		FROM session_record i JOIN runtime_revision r ON r.revision = i.prepared_revision
		WHERE i.id = $1 AND i.state <> 'RUNTIME_STATE_DELETED'
	`, pgx.RowToStructByName[fenceRow], id)
	if err != nil {
		return nil, fmt.Errorf("read Session %s recovery fence: %w", id, notFoundOr(err))
	}
	fence := &SessionRecoveryFence{TemplateAtespace: row.ActorTemplateAtespace, TemplateName: row.ActorTemplateName}
	if row.OperationID != nil {
		fence.Generation = *row.OperationID
	}
	if row.ActorUID != nil {
		fence.ActorUID = *row.ActorUID
	}
	return fence, nil
}

// BeginCrashedSessionRecovery fences confirmed-crash recovery under the Session lock.
// It preserves durable results; obsolete Actors and conflicting operations fail.
// generation is the caller's SessionRecoveryFence generation, read before it
// observed the crash. A lifecycle operation begun since then makes the observation
// stale, because a concurrent revert keeps the Actor UID and returns to READY.
func (c *Client) BeginCrashedSessionRecovery(ctx context.Context, id string, generation uuid.UUID, actorUID string, snapshot SessionTaskSnapshot) (*SessionOperation, error) {
	var operation *SessionOperation
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockSession(ctx, tx, id)
		if err != nil {
			return notFoundOr(err)
		}
		// Both guards are read under the lock in one statement, and are evaluated in
		// their original order: Actor identity first, checkpoint creation last.
		type guards struct {
			Matches bool
			Busy    bool
		}
		guard, err := queryOne(ctx, tx, `
			SELECT actor_uid = $2 AS matches,
			    EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = $1 AND state = 'CREATING') AS busy
			FROM session WHERE id = $1
		`, pgx.RowToStructByName[guards], row.ID, actorUID)
		if err != nil {
			return err
		}
		if !guard.Matches || actorUID == "" {
			return ErrConflict
		}
		session, err := toSession(row)
		if err != nil {
			return err
		}
		if session.State == apiv1.RuntimeState_RUNTIME_STATE_FAILED && session.Operation == apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME && session.Failure.GetReason() == SessionRuntimeCrashed {
			operation, err = toSessionOperation(row)
			return err
		}
		if session.State != apiv1.RuntimeState_RUNTIME_STATE_READY || session.Operation != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE {
			return ErrConflict
		}
		current := uuid.Nil
		if row.OperationID != nil {
			current = *row.OperationID
		}
		if current != generation {
			return ErrConflict
		}
		// An issued idle suspension does not exclude recovery here. It cannot finish
		// against the crashed Actor, and its worker keeps the claim when suspension
		// fails, so waiting for it would block recovery permanently. Settlement below
		// retires the claim in this transaction; a late finish for it changes nothing.
		// The generation check keeps the crash observation current: without another
		// lifecycle operation, nothing returns the crashed Actor to service.
		if guard.Busy {
			return ErrConflict
		}
		row.State = apiv1.RuntimeState_RUNTIME_STATE_FAILED.String()
		row.runtimeInstanceRow, err = beginRuntimeOperation(ctx, tx, row.runtimeInstanceRow, runtimeKindAgent, apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME)
		if err != nil {
			return err
		}
		session.State, session.Operation = apiv1.RuntimeState_RUNTIME_STATE_FAILED, apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME
		session.Failure = &apiv1.Failure{Reason: SessionRuntimeCrashed, Message: "Runtime crashed; recovery to its last snapshot is pending"}
		session.UpdatedAt = timestamppb.Now()
		row.Data, err = marshalSession(session)
		if err != nil {
			return err
		}
		if err := execSQL(ctx, tx, `UPDATE session SET data = $2, dispatch_id = NULL, dispatch_expires_at = NULL WHERE id = $1`, row.ID, row.Data); err != nil {
			return err
		}
		if err := settleCrashedSessionTask(ctx, tx, row, snapshot); err != nil {
			return err
		}
		operation, err = toSessionOperation(row)
		return err
	})
	return operation, err
}

// settleCrashedSessionTask retains captured waits and fails discarded nonterminal work.
// Callers hold the Session lock; recovery cannot checkpoint discarded state.
func settleCrashedSessionTask(ctx context.Context, tx pgx.Tx, session sessionRow, snapshot SessionTaskSnapshot) error {
	type interruptedTask struct {
		ID      string
		Data    []byte
		Version *int64
		Pending bool
	}
	// The CTE clears the history's pending quiescence marker. The main query
	// reads the pre-update snapshot, but pending only tests that a boundary was
	// recorded, which clearing keeps true. A task without events is malformed.
	tasks, err := queryMany(ctx, tx, `
		WITH cleared AS (
		    UPDATE session_task_event SET quiescence_pending = FALSE
		    WHERE history_id = $1 AND quiescence_pending)
		SELECT t.id, t.data, latest.sequence AS version,
		    COALESCE(NOT latest.published AND latest.quiescence_pending IS NOT NULL, FALSE) AS pending
		FROM session_task t
		LEFT JOIN LATERAL (SELECT sequence, published, quiescence_pending FROM session_task_event e
		    WHERE e.history_id = t.history_id AND e.task_id = t.id
		    ORDER BY sequence DESC LIMIT 1) latest ON TRUE
		WHERE t.history_id = $1
		    AND (t.state IN ('TASK_STATE_SUBMITTED', 'TASK_STATE_WORKING')
		      OR (t.state IN ('TASK_STATE_INPUT_REQUIRED', 'TASK_STATE_AUTH_REQUIRED')
		        AND NOT EXISTS (SELECT 1 FROM session_task_event captured
		          WHERE captured.history_id = t.history_id AND $2::text <> ''
		            AND captured.snapshot_uri = $2 AND captured.snapshot_fidelity = $3
		            AND captured.snapshot_atespace = $4
		            AND captured.sequence >= latest.sequence)))
	`, pgx.RowToStructByName[interruptedTask], session.HistoryID, snapshot.URI, snapshot.Fidelity, snapshot.Atespace)
	if err != nil {
		return fmt.Errorf("read interrupted Session tasks: %w", err)
	}
	for _, interrupted := range tasks {
		taskID, data := interrupted.ID, interrupted.Data
		if interrupted.Version == nil {
			return fmt.Errorf("interrupted Session task %s has no events", taskID)
		}
		if interrupted.Pending {
			if err := settleSessionTask(ctx, tx, session, taskID, *interrupted.Version); err != nil {
				return err
			}
			// Settlement rewrote the projection, so the selected data is stale.
			stored, err := readSessionTask(ctx, tx, session.HistoryID, taskID)
			if err != nil {
				return err
			}
			data = stored.Data
		}
		task, err := unmarshalSessionTask(data)
		if err != nil {
			return err
		}
		if !task.Status.State.Terminal() {
			task.Status = a2a.TaskStatus{State: a2a.TaskStateFailed, Timestamp: new(time.Now().UTC()),
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("The runtime crashed before this turn completed. Send a new message to continue."))}
			apia2a.SetTimelinePosition(task.Status.Message, *task.Status.Timestamp)
			event := &a2a.TaskStatusUpdateEvent{TaskID: task.ID, ContextID: task.ContextID, Status: task.Status}
			if err := storeSessionTaskEvent(ctx, tx, session, task, event, false); err != nil {
				return err
			}
			version, err := taskVersion(ctx, tx, session.HistoryID, taskID)
			if err != nil {
				return err
			}
			if err := publishPushBoundary(ctx, tx, session.HistoryID, taskID, version, task); err != nil {
				return err
			}
		}
	}
	return nil
}
