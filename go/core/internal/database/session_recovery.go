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

// SessionRuntimeCrashed is the failure reason set once Substrate confirms the
// recorded Actor crashed. Transport failures never set it.
const SessionRuntimeCrashed = "RuntimeCrashed"

// CrashRecoveryAdmitted reports an admitted, unfinished crash recovery. Only
// BeginCrashedSessionRecovery starts a REVERT, so the operation alone identifies it.
func CrashRecoveryAdmitted(session *apiv1.Session) bool {
	return session.Operation == apiv1.RuntimeOperation_RUNTIME_OPERATION_REVERT
}

// SessionGeneration is read before crash recovery observes Substrate. OperationID
// is uuid.Nil when none is recorded; every operation replaces it and completion
// keeps it. ActorUID is "" when none is recorded.
type SessionGeneration struct {
	SessionID        string
	OperationID      uuid.UUID
	ActorUID         string
	TemplateAtespace string
	TemplateName     string
}

// GetSessionGeneration returns ErrNotFound for deleted Sessions. Callers authorize
// access. The read takes no lock; admission re-checks it under the Session lock.
func (c *Client) GetSessionGeneration(ctx context.Context, id string) (*SessionGeneration, error) {
	type generationRow struct {
		OperationID           *uuid.UUID
		ActorUID              *string
		ActorTemplateAtespace string
		ActorTemplateName     string
	}
	row, err := queryOne(ctx, c.db, `
		SELECT i.operation_id, i.actor_uid, r.actor_template_atespace, r.actor_template_name
		FROM session_record i JOIN runtime_revision r ON r.revision = i.prepared_revision
		WHERE i.id = $1 AND i.state <> 'RUNTIME_STATE_DELETED'
	`, pgx.RowToStructByName[generationRow], id)
	if err != nil {
		return nil, fmt.Errorf("read Session %s generation: %w", id, notFoundOr(err))
	}
	generation := &SessionGeneration{SessionID: id, TemplateAtespace: row.ActorTemplateAtespace, TemplateName: row.ActorTemplateName}
	if row.OperationID != nil {
		generation.OperationID = *row.OperationID
	}
	if row.ActorUID != nil {
		generation.ActorUID = *row.ActorUID
	}
	return generation, nil
}

// BeginCrashedSessionRecovery admits recovery of a confirmed crash and preserves
// durable results. generation must be read before the crash was observed: a
// concurrent revert keeps the Actor UID and returns to READY, so only a newer
// lifecycle operation shows that the observation is stale. An issued dispatch
// cleanup blocks recovery until its owner completes or conclusively stops.
func (c *Client) BeginCrashedSessionRecovery(ctx context.Context, generation *SessionGeneration, snapshot SessionTaskSnapshot) (*SessionOperation, error) {
	var operation *SessionOperation
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockSession(ctx, tx, generation.SessionID)
		if err != nil {
			return notFoundOr(err)
		}
		session, err := toSession(row)
		if err != nil {
			return err
		}
		if CrashRecoveryAdmitted(session) {
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
		if current != generation.OperationID {
			return ErrConflict
		}
		// Task-boundary suspension keeps its existing recovery semantics. Dispatch
		// cleanup may still resume and then suspend the Actor, so its owner must
		// finish or conclusively stop before recovery can retire the reservation.
		busy, err := queryOne(ctx, tx, `
			SELECT EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = $1 AND state = 'CREATING')
			    OR EXISTS (SELECT 1 FROM session WHERE id = $1 AND dispatch_cleanup_executor_id IS NOT NULL)
		`, pgx.RowTo[bool], row.ID)
		if err != nil {
			return err
		}
		if busy {
			return ErrConflict
		}
		row.State = apiv1.RuntimeState_RUNTIME_STATE_FAILED.String()
		row.runtimeInstanceRow, err = beginRuntimeOperation(ctx, tx, row.runtimeInstanceRow, runtimeKindAgent, apiv1.RuntimeOperation_RUNTIME_OPERATION_REVERT)
		if err != nil {
			return err
		}
		session.State, session.Operation = apiv1.RuntimeState_RUNTIME_STATE_FAILED, apiv1.RuntimeOperation_RUNTIME_OPERATION_REVERT
		session.Failure = &apiv1.Failure{Reason: SessionRuntimeCrashed, Message: "Runtime crashed; recovery to its last snapshot is pending"}
		session.UpdatedAt = timestamppb.Now()
		row.Data, err = marshalSession(session)
		if err != nil {
			return err
		}
		if err := execSQL(ctx, tx, `
			UPDATE session SET data = $2, dispatch_id = NULL, dispatch_expires_at = NULL, dispatch_cleanup_executor_id = NULL
			WHERE id = $1
		`, row.ID, row.Data); err != nil {
			return err
		}
		if err := settleCrashedSessionTasks(ctx, tx, row, snapshot); err != nil {
			return err
		}
		operation, err = toSessionOperation(row)
		return err
	})
	return operation, err
}

// settleCrashedSessionTasks publishes saved results, keeps waits the snapshot
// captured, and fails the rest of the open work the revert discards.
func settleCrashedSessionTasks(ctx context.Context, tx pgx.Tx, session sessionRow, snapshot SessionTaskSnapshot) error {
	if err := publishSavedSessionResults(ctx, tx, session); err != nil {
		return err
	}
	type interruptedTask struct {
		ID      string
		Data    []byte
		Version *int64
	}
	// A wait is captured when an event recording the snapshot follows the task's
	// latest event.
	tasks, err := queryMany(ctx, tx, `
		SELECT t.id, t.data, latest.sequence AS version
		FROM session_task t
		LEFT JOIN LATERAL (SELECT MAX(sequence) AS sequence FROM session_task_event e
		    WHERE e.history_id = t.history_id AND e.task_id = t.id) latest ON TRUE
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
		if interrupted.Version == nil {
			return fmt.Errorf("interrupted Session task %s has no events", interrupted.ID)
		}
		task, err := unmarshalSessionTask(interrupted.Data)
		if err != nil {
			return err
		}
		task.Status = a2a.TaskStatus{State: a2a.TaskStateFailed, Timestamp: new(time.Now().UTC()),
			Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("The runtime crashed before this turn completed. Send a new message to continue."))}
		apia2a.SetTimelinePosition(task.Status.Message, *task.Status.Timestamp)
		event := &a2a.TaskStatusUpdateEvent{TaskID: task.ID, ContextID: task.ContextID, Status: task.Status}
		if err := storeSessionTaskEvent(ctx, tx, session, task, event, false); err != nil {
			return err
		}
		version, err := taskVersion(ctx, tx, session.HistoryID, interrupted.ID)
		if err != nil {
			return err
		}
		if err := publishPushBoundary(ctx, tx, session.HistoryID, interrupted.ID, version, task); err != nil {
			return err
		}
	}
	return retireSessionQuiescence(ctx, tx, session)
}
