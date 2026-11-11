package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// SessionDispatchCleanup identifies an abandoned delivery's runtime cleanup.
// An unclaimed candidate grants no execution authority. Claim returns a new
// value whose executor fences runtime work until the owner records completion
// or conclusively stops and releases it.
type SessionDispatchCleanup struct {
	Session    *apiv1.Session
	DispatchID uuid.UUID
	ActorUID   string
	ExecutorID uuid.UUID
	boundary   *dispatchCleanupBoundary
}

type dispatchCleanupBoundary struct {
	TaskID  string
	Version int64
	State   string
}

// GetPendingSessionDispatchCleanup observes the next revoked or expired delivery
// after the Session cursor without holding locks during runtime preparation.
// Missing or blocked work returns ErrNotFound. Claim revalidates the generation;
// advancing the cursor lets a worker skip an actor that is not ready to suspend.
func (c *Client) GetPendingSessionDispatchCleanup(ctx context.Context, afterSessionID string) (*SessionDispatchCleanup, error) {
	type candidate struct {
		SessionID  string
		DispatchID uuid.UUID
		ActorUID   string
	}
	row, err := queryOne(ctx, c.db, `
		SELECT s.id::text AS session_id, s.dispatch_id, s.actor_uid
		FROM session s JOIN runtime_instance r USING (id)
		WHERE s.dispatch_id IS NOT NULL
		  AND s.dispatch_cleanup_executor_id IS NULL AND s.actor_uid IS NOT NULL
		  AND ($1::text = '' OR s.id > NULLIF($1::text, '')::uuid)
		  AND r.state = 'RUNTIME_STATE_READY' AND r.operation = 'RUNTIME_OPERATION_NONE'
		  AND s.dispatch_expires_at <= clock_timestamp()
		  AND NOT EXISTS (SELECT 1 FROM session_task t WHERE t.history_id = s.history_id
		      AND t.state NOT IN ('TASK_STATE_COMPLETED', 'TASK_STATE_CANCELED', 'TASK_STATE_FAILED',
		          'TASK_STATE_REJECTED', 'TASK_STATE_INPUT_REQUIRED', 'TASK_STATE_AUTH_REQUIRED'))
		  AND NOT EXISTS (SELECT 1 FROM session_task_event e WHERE e.history_id = s.history_id
		      AND (NOT e.published OR (e.quiescence_pending AND e.quiescence_executor_id IS NOT NULL)))
		  AND NOT EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = s.id AND state = 'CREATING')
		ORDER BY s.id LIMIT 1
	`, pgx.RowToStructByName[candidate], afterSessionID)
	if err != nil {
		return nil, notFoundOr(err)
	}
	session, err := readSession(ctx, c.db, row.SessionID)
	if err != nil {
		return nil, notFoundOr(err)
	}
	value, err := toSession(session)
	if err != nil {
		return nil, err
	}
	return &SessionDispatchCleanup{Session: value, DispatchID: row.DispatchID, ActorUID: row.ActorUID}, nil
}

// ClaimSessionDispatchCleanup grants exclusive runtime authority for this exact
// delivery and actor. Admission and history capture commit together. A stale or
// blocked candidate returns ErrNotFound; issued work is never reclaimed by time.
func (c *Client) ClaimSessionDispatchCleanup(ctx context.Context, candidate *SessionDispatchCleanup) (*SessionDispatchCleanup, error) {
	if candidate == nil || candidate.Session == nil || candidate.DispatchID == uuid.Nil || candidate.ActorUID == "" {
		return nil, ErrFailedPrecondition
	}
	var result *SessionDispatchCleanup
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, candidate.Session.Id)
		if err != nil {
			return notFoundOr(err)
		}
		executor := uuid.New()
		tag, err := tx.Exec(ctx, `
			UPDATE session s SET dispatch_cleanup_executor_id = $4
			FROM runtime_instance r
			WHERE s.id = $1 AND r.id = s.id AND s.dispatch_id = $2 AND s.actor_uid = $3
			  AND s.dispatch_cleanup_executor_id IS NULL
			  AND r.state = 'RUNTIME_STATE_READY' AND r.operation = 'RUNTIME_OPERATION_NONE'
			  AND s.dispatch_expires_at <= clock_timestamp()
			  AND NOT EXISTS (SELECT 1 FROM session_task t WHERE t.history_id = s.history_id
			      AND t.state NOT IN ('TASK_STATE_COMPLETED', 'TASK_STATE_CANCELED', 'TASK_STATE_FAILED',
			          'TASK_STATE_REJECTED', 'TASK_STATE_INPUT_REQUIRED', 'TASK_STATE_AUTH_REQUIRED'))
			  AND NOT EXISTS (SELECT 1 FROM session_task_event e WHERE e.history_id = s.history_id
			      AND (NOT e.published OR (e.quiescence_pending AND e.quiescence_executor_id IS NOT NULL)))
			  AND NOT EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = s.id AND state = 'CREATING')
		`, session.ID, candidate.DispatchID, candidate.ActorUID, executor)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrNotFound
		}
		value, err := toSession(session)
		if err != nil {
			return err
		}
		result = &SessionDispatchCleanup{Session: value, DispatchID: candidate.DispatchID, ActorUID: candidate.ActorUID, ExecutorID: executor}
		boundary, err := queryOne(ctx, tx, `
			SELECT e.task_id, e.sequence AS version, t.state FROM session_task_event e
			JOIN session_task t ON t.history_id = e.history_id AND t.id = e.task_id
			WHERE e.history_id = $1 ORDER BY e.sequence DESC LIMIT 1
		`, pgx.RowToStructByName[dispatchCleanupBoundary], session.HistoryID)
		if err == nil {
			result.boundary = &boundary
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		hasTasks, err := queryOne(ctx, tx, `SELECT EXISTS (SELECT 1 FROM session_task WHERE history_id = $1)`, pgx.RowTo[bool], session.HistoryID)
		if err != nil {
			return err
		}
		if hasTasks {
			return fmt.Errorf("session task history is missing: %w", ErrFailedPrecondition)
		}
		return nil
	})
	return result, err
}

// ReleaseSessionDispatchCleanup releases only the exact claiming owner and
// preserves the expired dispatch for retry or crash recovery. The caller must
// permanently stop runtime mutations with conclusive outcomes before releasing;
// a timeout or transport error does not establish that remote work has ended.
// Missing Sessions return ErrNotFound. Stale, mismatched, or already released
// claims return ErrConflict without changing the current owner's authority.
func (c *Client) ReleaseSessionDispatchCleanup(ctx context.Context, work *SessionDispatchCleanup) error {
	if work == nil || work.Session == nil || work.DispatchID == uuid.Nil || work.ExecutorID == uuid.Nil || work.ActorUID == "" {
		return ErrFailedPrecondition
	}
	return c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, work.Session.Id)
		if err != nil {
			return notFoundOr(err)
		}
		tag, err := tx.Exec(ctx, `
			UPDATE session SET dispatch_cleanup_executor_id = NULL
			WHERE id = $1 AND dispatch_id = $2 AND dispatch_cleanup_executor_id = $3 AND actor_uid = $4
		`, session.ID, work.DispatchID, work.ExecutorID, work.ActorUID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
		return nil
	})
}

// FinishSessionDispatchCleanup records the claimed snapshot without inventing
// history or changing logical Session state. It releases admission atomically
// with snapshot metadata and obsolete task-quiescence work. Completed retries
// do nothing; a newer cleanup generation or owner returns ErrConflict.
func (c *Client) FinishSessionDispatchCleanup(ctx context.Context, work *SessionDispatchCleanup, snapshot *SessionTaskSnapshot) error {
	if work == nil || work.Session == nil || work.DispatchID == uuid.Nil || work.ExecutorID == uuid.Nil {
		return ErrFailedPrecondition
	}
	if snapshot == nil || snapshot.Atespace == "" || snapshot.URI == "" || snapshot.Fidelity == "" {
		return fmt.Errorf("dispatch cleanup requires a runtime snapshot: %w", ErrFailedPrecondition)
	}
	return c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, work.Session.Id)
		if err != nil {
			return notFoundOr(err)
		}
		type claim struct {
			ID       *uuid.UUID
			Executor *uuid.UUID
			ActorUID *string
		}
		current, err := queryOne(ctx, tx, `
			SELECT dispatch_id AS id, dispatch_cleanup_executor_id AS executor, actor_uid
			FROM session WHERE id = $1
		`, pgx.RowToStructByName[claim], session.ID)
		if err != nil {
			return err
		}
		if current.ID == nil {
			return nil
		}
		if *current.ID != work.DispatchID || current.Executor == nil || *current.Executor != work.ExecutorID || current.ActorUID == nil || *current.ActorUID != work.ActorUID {
			return ErrConflict
		}
		if target := work.boundary; target != nil {
			latest, err := queryOne(ctx, tx, `SELECT MAX(sequence) FROM session_task_event WHERE history_id = $1`, pgx.RowTo[int64], session.HistoryID)
			if err != nil {
				return err
			}
			stored, err := readSessionTask(ctx, tx, session.HistoryID, target.TaskID)
			if err != nil {
				return err
			}
			if latest != target.Version || stored.State != target.State {
				return ErrConflict
			}
			if err := saveSessionTaskSnapshot(ctx, tx, session.HistoryID, target.TaskID, target.Version, snapshot); err != nil {
				return err
			}
		}
		if err := execSQL(ctx, tx, `
			UPDATE session_task_event SET quiescence_pending = FALSE
			WHERE history_id = $1 AND quiescence_pending AND quiescence_executor_id IS NULL
		`, session.HistoryID); err != nil {
			return err
		}
		return retireSessionDispatchCleanup(ctx, tx, session.ID)
	})
}

// retireSessionDispatchCleanup removes compensation after execution or an
// authoritative lifecycle outcome takes responsibility. Callers hold the Session
// lock and must have checked admission or established runtime termination.
func retireSessionDispatchCleanup(ctx context.Context, tx pgx.Tx, sessionID uuid.UUID) error {
	return execSQL(ctx, tx, `
		UPDATE session SET dispatch_id = NULL, dispatch_expires_at = NULL, dispatch_cleanup_executor_id = NULL
		WHERE id = $1
	`, sessionID)
}
