package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SessionOperation is the current lifecycle generation on a session. Once
// Session.Operation is unspecified, callers may return the current session.
// ExecutorID marks possibly issued runtime work and never expires. The value is
// an observation, not permission to execute: callers must win Claim first.
type SessionOperation struct {
	ID                 uuid.UUID
	Session            *apiv1alpha1.Session
	SourceCheckpointID *uuid.UUID
	ExecutorID         uuid.UUID
}

// BeginSessionOperation admits or joins current lifecycle work under the
// session lock. Creation retries and already-at-target requests return current
// state. Delete alone may supersede unclaimed work; uncertain work blocks it.
// Callers authorize access. Deleted sessions return ErrNotFound, except Delete
// can observe the tombstone for a caller already authorized before deletion.
func (c *Client) BeginSessionOperation(ctx context.Context, sessionID string, kind apiv1alpha1.SessionOperation) (*SessionOperation, error) {
	var operation *SessionOperation
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return notFoundOr(err)
		}
		operation, err = toSessionOperation(row)
		if err != nil {
			return err
		}
		session := operation.Session
		if session.State == apiv1alpha1.SessionState_SESSION_STATE_DELETED {
			if kind == apiv1alpha1.SessionOperation_SESSION_OPERATION_DELETE {
				return nil
			}
			return ErrNotFound
		}
		pending := row.OperationID != nil && session.Operation != apiv1alpha1.SessionOperation_SESSION_OPERATION_UNSPECIFIED
		if pending {
			if session.Operation == kind {
				return nil
			}
			canSupersede := kind == apiv1alpha1.SessionOperation_SESSION_OPERATION_DELETE && row.ExecutorID == nil
			if !canSupersede {
				return fmt.Errorf("session has an unfinished lifecycle operation: %w", ErrConflict)
			}
			session.Operation = apiv1alpha1.SessionOperation_SESSION_OPERATION_UNSPECIFIED
		}
		var expected, target apiv1alpha1.SessionState
		switch kind {
		case apiv1alpha1.SessionOperation_SESSION_OPERATION_CREATE:
			// Request deduplication reserves identity, not a historical READY response.
			if session.State != apiv1alpha1.SessionState_SESSION_STATE_CREATING && session.Operation == apiv1alpha1.SessionOperation_SESSION_OPERATION_UNSPECIFIED {
				return nil
			}
			expected = apiv1alpha1.SessionState_SESSION_STATE_CREATING
		case apiv1alpha1.SessionOperation_SESSION_OPERATION_RESUME:
			expected, target = apiv1alpha1.SessionState_SESSION_STATE_SUSPENDED, apiv1alpha1.SessionState_SESSION_STATE_READY
		case apiv1alpha1.SessionOperation_SESSION_OPERATION_SUSPEND:
			expected, target = apiv1alpha1.SessionState_SESSION_STATE_READY, apiv1alpha1.SessionState_SESSION_STATE_SUSPENDED
		case apiv1alpha1.SessionOperation_SESSION_OPERATION_DELETE:
			expected = session.State
		default:
			return fmt.Errorf("invalid lifecycle operation %s", kind)
		}
		alreadyAtTarget := target != apiv1alpha1.SessionState_SESSION_STATE_UNSPECIFIED && session.State == target
		if alreadyAtTarget && session.Operation == apiv1alpha1.SessionOperation_SESSION_OPERATION_UNSPECIFIED {
			return nil
		}
		expectedOperation := apiv1alpha1.SessionOperation_SESSION_OPERATION_UNSPECIFIED
		if kind == apiv1alpha1.SessionOperation_SESSION_OPERATION_CREATE {
			expectedOperation = kind
		}
		deletingUnissuedCreation := kind == apiv1alpha1.SessionOperation_SESSION_OPERATION_DELETE && session.Operation == apiv1alpha1.SessionOperation_SESSION_OPERATION_CREATE
		canStart := session.State == expected && (session.Operation == expectedOperation || deletingUnissuedCreation)
		if !canStart {
			return fmt.Errorf("session cannot start %s from %s with operation %s: %w", kind, session.State, session.Operation, ErrConflict)
		}
		if err := requireSettledRuntime(ctx, tx, row.HistoryID, ""); err != nil {
			return err
		}
		session.Operation, session.UpdatedAt = kind, timestamppb.Now()
		data, err := marshalSession(session)
		if err != nil {
			return err
		}
		operation.ID, operation.ExecutorID = uuid.New(), uuid.Nil
		tag, err := tx.Exec(ctx, `
			UPDATE session SET operation = $2, data = $3, operation_id = $5, executor_id = NULL WHERE id = $1
			AND NOT EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = $1 AND state = 'CREATING')
			AND ($2::text <> 'SESSION_OPERATION_SUSPEND' OR NOT EXISTS (
			    SELECT 1 FROM session_task WHERE history_id = $4
			    AND state NOT IN ('TASK_STATE_COMPLETED', 'TASK_STATE_CANCELED', 'TASK_STATE_FAILED',
			        'TASK_STATE_REJECTED', 'TASK_STATE_INPUT_REQUIRED', 'TASK_STATE_AUTH_REQUIRED')))
		`, sessionID, kind.String(), data, row.HistoryID, operation.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("session has an active task or checkpoint: %w", ErrConflict)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("begin Session operation: %w", err)
	}
	return operation, nil
}

// ClaimSessionOperation grants one executor permission to issue this
// generation's runtime work. False means observe only. A stale generation cannot
// claim, even when a later operation has the same kind and state. Claims never
// expire: losing an RPC response does not prove that its effects have stopped.
func (c *Client) ClaimSessionOperation(ctx context.Context, sessionID string, id, executorID uuid.UUID) (bool, error) {
	if id == uuid.Nil || executorID == uuid.Nil {
		return false, fmt.Errorf("lifecycle generation and executor IDs are required")
	}
	tag, err := c.db.Exec(ctx, `
		UPDATE session SET executor_id = $3
		WHERE id = $1 AND operation_id = $2 AND executor_id IS NULL
		  AND operation <> 'SESSION_OPERATION_UNSPECIFIED'
		  AND state <> 'SESSION_STATE_DELETED'
	`, sessionID, id, executorID)
	return tag.RowsAffected() == 1, err
}

// FinishSessionOperation publishes known success only for the claiming
// executor. A nonempty failure releases only unclaimed preparation and invalidates
// its generation. Uncertain issued work must remain pending. Stale completion or
// release returns ErrConflict. Deletion retains a tombstone and revokes shares;
// PostgreSQL releases its resource pins atomically with the state change.
func (c *Client) FinishSessionOperation(ctx context.Context, sessionID string, id, executorID uuid.UUID, authority, actorUID, failure string) (*apiv1alpha1.Session, error) {
	var result *apiv1alpha1.Session
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockSession(ctx, tx, sessionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		operation, err := toSessionOperation(row)
		if err != nil {
			return err
		}
		result = operation.Session
		failedPreparation := executorID == uuid.Nil && failure != ""
		successfulExecution := executorID != uuid.Nil && failure == ""
		if id == uuid.Nil || operation.ID != id || result.Operation == apiv1alpha1.SessionOperation_SESSION_OPERATION_UNSPECIFIED || operation.ExecutorID != executorID || (!failedPreparation && !successfulExecution) {
			return fmt.Errorf("lifecycle operation no longer belongs to this executor: %w", ErrConflict)
		}
		kind := result.Operation
		result.Operation = apiv1alpha1.SessionOperation_SESSION_OPERATION_UNSPECIFIED
		result.UpdatedAt = timestamppb.Now()
		if successfulExecution {
			switch kind {
			case apiv1alpha1.SessionOperation_SESSION_OPERATION_CREATE:
				if authority == "" || actorUID == "" {
					return fmt.Errorf("created Session requires runtime authority and actor UID")
				}
				result.State, result.A2AAuthority, result.Failure = apiv1alpha1.SessionState_SESSION_STATE_READY, authority, nil
			case apiv1alpha1.SessionOperation_SESSION_OPERATION_RESUME:
				result.State = apiv1alpha1.SessionState_SESSION_STATE_READY
			case apiv1alpha1.SessionOperation_SESSION_OPERATION_SUSPEND:
				result.State = apiv1alpha1.SessionState_SESSION_STATE_SUSPENDED
			case apiv1alpha1.SessionOperation_SESSION_OPERATION_DELETE:
				if actorUID != "" {
					matches, err := queryOne(ctx, tx, `SELECT actor_uid = $2 FROM session WHERE id = $1`, pgx.RowTo[bool], sessionID, actorUID)
					if err != nil {
						return err
					}
					if !matches {
						return fmt.Errorf("runtime actor UID changed: %w", ErrConflict)
					}
				}
				return tombstoneSession(ctx, tx, result, row.OperationID)
			}
		} else if kind == apiv1alpha1.SessionOperation_SESSION_OPERATION_CREATE {
			result.Operation = kind // A new generation may retry preparation with the same pinned inputs.
		}
		data, err := marshalSession(result)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE session SET state = $2, operation = $3, data = $4,
			    operation_id = CASE WHEN $5 THEN NULL ELSE operation_id END,
			    executor_id = NULL, actor_uid = CASE WHEN $7 THEN $6 ELSE actor_uid END
			WHERE id = $1 AND ($6::text = '' OR actor_uid = $6 OR (actor_uid IS NULL AND $7))
		`, sessionID, result.State.String(), result.Operation.String(), data, failedPreparation, actorUID,
			successfulExecution && kind == apiv1alpha1.SessionOperation_SESSION_OPERATION_CREATE)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("runtime actor UID changed: %w", ErrConflict)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("finish Session operation: %w", err)
	}
	return result, nil
}

// GetSessionOperation observes this generation only while it remains current,
// including its deletion tombstone. A superseded or missing generation returns
// ErrConflict. Callers must have authorized the session at admission. It never
// returns historical results or grants permission to issue runtime work.
func (c *Client) GetSessionOperation(ctx context.Context, sessionID string, id uuid.UUID) (*SessionOperation, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT id, user_id, prepared_revision, state, data, operation, context_id,
		    source_checkpoint_id, history_id, operation_id, executor_id FROM session
		WHERE id = $1 AND operation_id = $2
	`, pgx.RowToStructByName[sessionRow], sessionID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("lifecycle generation was superseded: %w", ErrConflict)
	}
	if err != nil {
		return nil, err
	}
	return toSessionOperation(row)
}

// toSessionOperation decodes current session state and its private execution
// identity. A completed generation observes current fields, including renames.
func toSessionOperation(row sessionRow) (*SessionOperation, error) {
	session, err := toSession(row)
	if err != nil {
		return nil, err
	}
	operation := &SessionOperation{Session: session, SourceCheckpointID: row.SourceCheckpointID}
	if row.OperationID != nil {
		operation.ID = *row.OperationID
	}
	if row.ExecutorID != nil {
		operation.ExecutorID = *row.ExecutorID
	}
	return operation, nil
}
