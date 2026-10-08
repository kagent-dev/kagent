package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ListIdleSessions returns candidates, including unfinished idle deletions.
// Empty histories and old fork histories may be newer sessions; admission checks
// creation time and rechecks activity under the task/lifecycle lock.
func (c *Client) ListIdleSessions(ctx context.Context, before time.Time, afterID string, limit int) ([]string, error) {
	return queryMany(ctx, c.db, `
		SELECT s.id::text FROM session s JOIN runtime_instance r USING (id)
		LEFT JOIN LATERAL (
		    SELECT created_at FROM session_task_event WHERE history_id = s.history_id
		    ORDER BY sequence DESC LIMIT 1
		) latest ON TRUE
		WHERE (s.deletion_reason = 'idle_timeout'
		    OR ((latest.created_at IS NULL OR latest.created_at <= $1) AND r.state <> 'RUNTIME_STATE_DELETED'))
		  AND ($2::text = '' OR s.id > $2::uuid)
		ORDER BY s.id LIMIT $3
	`, pgx.RowTo[string], before, afterID, limit)
}

// IdleSessionDeletion combines admitted deletion with activity information for
// sweep observability. Idle timing is not part of the lifecycle operation.
type IdleSessionDeletion struct {
	Operation *SessionOperation
	IdleSince time.Time
}

// BeginIdleSessionDeletion admits ordinary deletion only after rechecking idle
// eligibility. Once admitted, its reason survives retries and TTL changes.
func (c *Client) BeginIdleSessionDeletion(ctx context.Context, id string, before time.Time) (*IdleSessionDeletion, error) {
	var deletion *IdleSessionDeletion
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockSession(ctx, tx, id)
		if err != nil {
			return notFoundOr(err)
		}
		type activity struct {
			Reason      sessionDeletionReason
			LastEventAt *time.Time
		}
		latest, err := queryOne(ctx, tx, `
			SELECT COALESCE(deletion_reason, '') AS reason,
			    (SELECT created_at FROM session_task_event WHERE history_id = s.history_id
			     ORDER BY sequence DESC LIMIT 1) AS last_event_at
			FROM session s WHERE id = $1
		`, pgx.RowToStructByName[activity], row.ID)
		if err != nil {
			return err
		}
		session, err := toSession(row)
		if err != nil {
			return err
		}
		idleSince := session.CreatedAt.AsTime()
		// Forks retain original event times but receive a full idle lifetime.
		if latest.LastEventAt != nil && latest.LastEventAt.After(idleSince) {
			idleSince = *latest.LastEventAt
		}
		if latest.Reason != sessionDeletionIdleTimeout {
			if err := requireIdleSession(ctx, tx, row, idleSince, before); err != nil {
				return err
			}
		}
		operation, err := beginSessionDeletion(ctx, tx, row, sessionDeletionIdleTimeout)
		if err != nil {
			return err
		}
		deletion = &IdleSessionDeletion{Operation: operation, IdleSince: idleSince}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("begin idle Session deletion: %w", err)
	}
	return deletion, nil
}

// requireIdleSession runs under the session lock, before lifecycle admission
// checks dispatch, native cleanup, quiescence, and checkpoint creation.
func requireIdleSession(ctx context.Context, tx pgx.Tx, row sessionRow, idleSince, before time.Time) error {
	if idleSince.After(before) || row.State == "RUNTIME_STATE_DELETED" || row.State == "RUNTIME_STATE_DELETING" || row.Operation != "RUNTIME_OPERATION_NONE" {
		return ErrConflict
	}
	busy, err := queryOne(ctx, tx, `
		SELECT EXISTS (SELECT 1 FROM session_task WHERE history_id = $1
		    AND state NOT IN ('TASK_STATE_COMPLETED', 'TASK_STATE_CANCELED', 'TASK_STATE_FAILED', 'TASK_STATE_REJECTED'))
	`, pgx.RowTo[bool], row.HistoryID)
	if err != nil {
		return err
	}
	if busy {
		return ErrConflict
	}
	return nil
}
