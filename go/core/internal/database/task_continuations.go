package database

import (
	"context"
	"errors"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TaskContinuation is a durable dependency of an active parent on one native input.
type TaskContinuation struct {
	ID              uuid.UUID
	ParentSessionID string
	ParentTaskID    string
	ChildSessionID  string
	ChildMessageID  string
	ChildTaskID     *string
	ClaimToken      uuid.UUID
}

// RegisterTaskContinuation records intent before dispatch. Only a working parent
// may delegate, and an ancestor cannot be invoked as its own descendant.
func (c *Client) RegisterTaskContinuation(ctx context.Context, parentID, childID, messageID string) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		parent, err := lockSession(ctx, tx, parentID)
		if err != nil {
			return notFoundOr(err)
		}
		cycle, err := queryOne(ctx, tx, `
     WITH RECURSIVE ancestors(id) AS (
       SELECT $1::uuid UNION
       SELECT d.parent_session_id FROM task_continuation d JOIN ancestors a ON d.child_session_id = a.id
       WHERE NOT d.delivered AND NOT d.abandoned
     ) SELECT EXISTS (SELECT 1 FROM ancestors WHERE id = $2::uuid)
   `, pgx.RowTo[bool], parentID, childID)
		if err != nil {
			return err
		}
		if cycle {
			return ErrFailedPrecondition
		}
		taskID, err := queryOne(ctx, tx, `
     SELECT id FROM session_task WHERE history_id = $1 AND state = 'TASK_STATE_WORKING'
   `, pgx.RowTo[string], parent.HistoryID)
		if err != nil {
			return notFoundOr(err)
		}
		return execSQL(ctx, tx, `
     INSERT INTO task_continuation(id, parent_session_id, parent_task_id, child_session_id, child_message_id)
     VALUES ($1, $2, $3, $4, $5) ON CONFLICT (parent_task_id, child_session_id, child_message_id) DO NOTHING
   `, uuid.New(), parentID, taskID, childID, messageID)
	})
}

// bindTaskContinuations publishes the waiting boundary in the task's own transaction.
// A child that completes synchronously never causes an extra parent invocation.
func bindTaskContinuations(ctx context.Context, tx pgx.Tx, sessionID string, historyID uuid.UUID, taskID string, state a2a.TaskState) error {
	return execSQL(ctx, tx, `
   UPDATE task_continuation d SET child_task_id = $3,
     awaited = d.awaited OR $4::boolean OR EXISTS (
       SELECT 1 FROM task_continuation nested WHERE nested.parent_session_id = $1
         AND nested.parent_task_id = $3 AND nested.awaited AND NOT nested.delivered AND NOT nested.abandoned)
   WHERE d.child_session_id = $1 AND EXISTS (
     SELECT 1 FROM session_task_event e WHERE e.history_id = $2 AND e.task_id = $3
       AND e.message_id = d.child_message_id)
 `, sessionID, historyID, taskID, state == a2a.TaskStateInputRequired || state == a2a.TaskStateAuthRequired)
}

// ClaimTaskContinuations leases ready dependencies. A parent waits for the whole
// child session, including continuations of deeper delegations and input dispatches.
func (c *Client) ClaimTaskContinuations(ctx context.Context, limit int) ([]TaskContinuation, error) {
	return c.withContinuationClaims(ctx, limit)
}

// withContinuationClaims fences concurrent replicas without holding network calls in a transaction.
func (c *Client) withContinuationClaims(ctx context.Context, limit int) ([]TaskContinuation, error) {
	var claimed []TaskContinuation
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		if err := execSQL(ctx, tx, `
      UPDATE task_continuation d SET delivered = TRUE
      FROM session_record p, session_record c, session_task pt
      WHERE d.parent_session_id = p.id AND d.child_session_id = c.id
        AND pt.history_id = p.history_id AND pt.id = d.parent_task_id
        AND pt.state IN ('TASK_STATE_COMPLETED', 'TASK_STATE_FAILED', 'TASK_STATE_REJECTED', 'TASK_STATE_CANCELED')
        AND NOT d.awaited AND NOT d.delivered AND NOT d.abandoned AND d.child_task_id IS NULL
        AND NOT EXISTS (SELECT 1 FROM session s WHERE s.id = c.id
          AND s.dispatch_id IS NOT NULL AND s.dispatch_expires_at > clock_timestamp())
        AND NOT EXISTS (SELECT 1 FROM session_task_event e
          WHERE e.history_id = c.history_id AND e.message_id = d.child_message_id)
    `); err != nil {
			return err
		}
		if err := execSQL(ctx, tx, `
      UPDATE task_continuation d SET delivered = TRUE
      FROM session_record c, session_task t
      WHERE NOT d.awaited AND NOT d.delivered AND NOT d.abandoned
        AND c.id = d.child_session_id AND t.history_id = c.history_id AND t.id = d.child_task_id
        AND t.state IN ('TASK_STATE_COMPLETED', 'TASK_STATE_FAILED', 'TASK_STATE_REJECTED', 'TASK_STATE_CANCELED')
    `); err != nil {
			return err
		}
		if err := execSQL(ctx, tx, `
     WITH RECURSIVE lineage(id, session_id, task_id) AS (
       SELECT id, child_session_id, child_task_id FROM task_continuation WHERE NOT delivered AND NOT abandoned
       UNION SELECT l.id, l.session_id, n.resume_task_id FROM lineage l
         JOIN task_continuation n ON n.parent_session_id = l.session_id AND n.parent_task_id = l.task_id
         WHERE n.delivered AND n.resume_task_id IS NOT NULL
     ) UPDATE task_continuation d SET abandoned = TRUE, claim_token = NULL, lease_until = NULL
     FROM session_record p, session_record c, session_task pt, session_task ct
     WHERE NOT d.delivered AND NOT d.abandoned
       AND p.id = d.parent_session_id AND c.id = d.child_session_id
       AND pt.history_id = p.history_id AND pt.id = d.parent_task_id
       AND ct.history_id = c.history_id AND ct.id = d.child_task_id
       AND (pt.state IN ('TASK_STATE_CANCELED', 'TASK_STATE_FAILED', 'TASK_STATE_REJECTED')
         OR ct.state = 'TASK_STATE_CANCELED'
         OR p.state = 'RUNTIME_STATE_DELETED' OR c.state = 'RUNTIME_STATE_DELETED'
         OR EXISTS (SELECT 1 FROM task_continuation nested
           WHERE nested.parent_session_id = d.child_session_id AND nested.abandoned
             AND nested.parent_task_id IN (SELECT task_id FROM lineage WHERE id = d.id)))
   `); err != nil {
			return err
		}
		var err error
		claimed, err = queryMany(ctx, tx, `
     WITH RECURSIVE lineage(id, session_id, task_id) AS (
       SELECT id, child_session_id, child_task_id FROM task_continuation WHERE NOT delivered AND NOT abandoned
       UNION SELECT l.id, l.session_id, n.resume_task_id FROM lineage l
         JOIN task_continuation n ON n.parent_session_id = l.session_id AND n.parent_task_id = l.task_id
         WHERE n.delivered AND n.resume_task_id IS NOT NULL
     ), ready AS (
       SELECT d.id FROM task_continuation d
       JOIN session_record p ON p.id = d.parent_session_id
       JOIN session_record c ON c.id = d.child_session_id
       JOIN session_task pt ON pt.history_id = p.history_id AND pt.id = d.parent_task_id
       JOIN session_task ct ON ct.history_id = c.history_id AND ct.id = d.child_task_id
       WHERE d.awaited AND NOT d.delivered AND NOT d.abandoned
         AND (d.claim_token IS NULL OR d.lease_until < clock_timestamp())
         AND p.state = 'RUNTIME_STATE_READY' AND p.operation = 'RUNTIME_OPERATION_NONE'
         AND pt.state = 'TASK_STATE_COMPLETED'
         AND ct.state IN ('TASK_STATE_COMPLETED', 'TASK_STATE_FAILED', 'TASK_STATE_REJECTED')
         AND NOT EXISTS (SELECT 1 FROM session_task t WHERE t.history_id IN (p.history_id, c.history_id)
           AND t.state NOT IN ('TASK_STATE_COMPLETED', 'TASK_STATE_FAILED', 'TASK_STATE_REJECTED', 'TASK_STATE_CANCELED'))
         AND NOT EXISTS (SELECT 1 FROM session s WHERE s.id IN (p.id, c.id)
           AND s.dispatch_id IS NOT NULL AND s.dispatch_expires_at > clock_timestamp())
         AND NOT EXISTS (SELECT 1 FROM task_continuation nested
           WHERE nested.parent_session_id = d.child_session_id AND NOT nested.delivered AND NOT nested.abandoned
             AND nested.parent_task_id IN (SELECT task_id FROM lineage WHERE id = d.id))
         AND NOT EXISTS (SELECT 1 FROM task_continuation nested
           WHERE nested.parent_session_id = d.child_session_id AND nested.abandoned
             AND nested.parent_task_id IN (SELECT task_id FROM lineage WHERE id = d.id))
         AND NOT EXISTS (SELECT 1 FROM task_continuation sibling
           JOIN session_record sc ON sc.id = sibling.child_session_id
           JOIN session_task st ON st.history_id = sc.history_id AND st.id = sibling.child_task_id
           WHERE sibling.parent_task_id = d.parent_task_id AND NOT sibling.delivered AND NOT sibling.abandoned
             AND st.state NOT IN ('TASK_STATE_COMPLETED', 'TASK_STATE_FAILED', 'TASK_STATE_REJECTED', 'TASK_STATE_CANCELED'))
       ORDER BY d.id LIMIT $1 FOR UPDATE OF d SKIP LOCKED
     ) UPDATE task_continuation d SET claim_token = gen_random_uuid(), lease_until = clock_timestamp() + INTERVAL '30 seconds'
       FROM ready WHERE d.id = ready.id
       RETURNING d.id, d.parent_session_id::text, d.parent_task_id, d.child_session_id::text,
         d.child_message_id, d.child_task_id, d.claim_token
   `, pgx.RowToStructByName[TaskContinuation], int32(limit))
		return err
	})
	return claimed, err
}

// CancelTaskContinuations fences a task's pending resumes, including a dispatch
// reserved before cancellation but not yet persisted by the runtime.
func (c *Client) CancelTaskContinuations(ctx context.Context, sessionID, taskID string) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return notFoundOr(err)
		}
		tag, err := tx.Exec(ctx, `
     WITH RECURSIVE descendants(task_id) AS (
       SELECT parent_task_id FROM task_continuation WHERE parent_session_id = $1 AND parent_task_id = $2
       UNION SELECT child.id FROM descendants previous
         JOIN task_continuation d ON d.parent_task_id = previous.task_id
         CROSS JOIN LATERAL (VALUES (d.child_task_id), (d.resume_task_id)) child(id)
         WHERE child.id IS NOT NULL
     ) UPDATE task_continuation SET abandoned = TRUE, claim_token = NULL, lease_until = NULL
       WHERE parent_task_id IN (SELECT task_id FROM descendants) AND NOT delivered AND NOT abandoned
   `, session.ID, taskID)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return execSQL(ctx, tx, `UPDATE session SET dispatch_id = NULL, dispatch_expires_at = NULL WHERE id = $1`, session.ID)
	})
}

// CompleteTaskContinuation releases the lease after a send, or schedules a retry.
func (c *Client) CompleteTaskContinuation(ctx context.Context, value TaskContinuation, resumeTaskID string) error {
	tag, err := c.db.Exec(ctx, `
   UPDATE task_continuation SET delivered = $3, resume_task_id = NULLIF($4, ''),
     claim_token = CASE WHEN $3 THEN NULL ELSE claim_token END,
     lease_until = CASE WHEN $3 THEN NULL ELSE clock_timestamp() + INTERVAL '2 seconds' END
   WHERE id = $1 AND claim_token = $2 AND NOT abandoned
 `, value.ID, value.ClaimToken, resumeTaskID != "", resumeTaskID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

// LatestContinuationTask follows only the delegated task's native resumes.
func (c *Client) LatestContinuationTask(ctx context.Context, sessionID, taskID string) (*a2a.Task, error) {
	row, err := queryOne(ctx, c.db, `
   WITH RECURSIVE chain(id) AS (
     SELECT $2::text UNION
     SELECT d.resume_task_id FROM task_continuation d JOIN chain previous ON d.parent_task_id = previous.id
       WHERE d.parent_session_id = $1 AND d.delivered AND d.resume_task_id IS NOT NULL
   ) SELECT t.data FROM session_task t JOIN session_record s ON s.history_id = t.history_id
   WHERE s.id = $1 AND t.id IN (SELECT id FROM chain) ORDER BY t.position DESC LIMIT 1
 `, pgx.RowTo[[]byte], sessionID, taskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return unmarshalSessionTask(row)
}
