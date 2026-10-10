package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

// ListSessionsOfDeletedAgents returns the sessions whose Agent is gone — the
// name their prepared revision was rendered for has no active definition —
// and the deletions of that reason still awaiting runtime cleanup, by id after
// afterID, up to limit. A session of an Agent replaced under the same name is
// not listed: it keeps its pinned revision until it idles out. Nor is a fork of
// a checkpoint: a checkpoint retains runnable inputs after its Agent is deleted,
// and a fork, the only session that can be created once the Agent is gone,
// pins its checkpoint and runs on those inputs until it idles out or is deleted;
// the checkpoint and the revision are released after the last fork. Admission
// rechecks both under the session lock.
func (c *Client) ListSessionsOfDeletedAgents(ctx context.Context, afterID string, limit int) ([]string, error) {
	return queryMany(ctx, c.db, `
		SELECT s.id::text FROM session s
		JOIN runtime_instance r USING (id)
		JOIN agent_runtime_revision prepared ON prepared.revision = r.prepared_revision
		WHERE (s.deletion_reason = 'agent_deleted'
		    OR (r.state <> 'RUNTIME_STATE_DELETED' AND NOT EXISTS (
		        SELECT 1 FROM agent_definition p
		        WHERE p.namespace = prepared.namespace AND p.agent_name = prepared.agent_name
		          AND p.retired_at IS NULL)
		      AND s.pinned_checkpoint_id IS NULL))
		  AND ($1::text = '' OR s.id > $1::uuid)
		ORDER BY s.id LIMIT $2
	`, pgx.RowTo[string], afterID, limit)
}

// BeginDeletedAgentSessionDeletion admits the deletion of a session whose Agent
// is gone, or joins the deletion already admitted for it, under the session
// lock. An Agent active again under the name since the listing, or a fork of a
// checkpoint, leaves the session alone with ErrConflict; a turn
// of the session that still runs defers it with ErrFailedPrecondition, so the
// sweep never cuts a turn off and a later sweep deletes the session once the
// turn ended. A deleted session answers ErrNotFound. The admitted reason
// survives retries, so a later sweep finishes a deletion whose runtime work
// failed.
func (c *Client) BeginDeletedAgentSessionDeletion(ctx context.Context, id string) (*SessionOperation, error) {
	var operation *SessionOperation
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockSession(ctx, tx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if row.State == apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED.String() {
			return ErrNotFound
		}
		type eligibility struct {
			Admitted    bool
			AgentActive bool
			Fork        bool
			TurnRunning bool
		}
		current, err := queryOne(ctx, tx, `
			SELECT COALESCE(s.deletion_reason, '') = 'agent_deleted' AS admitted,
			    EXISTS (
			        SELECT 1 FROM agent_runtime_revision prepared
			        JOIN agent_definition p ON p.namespace = prepared.namespace AND p.agent_name = prepared.agent_name
			        WHERE prepared.revision = $2 AND p.retired_at IS NULL) AS agent_active,
			    s.pinned_checkpoint_id IS NOT NULL AS fork,
			    EXISTS (
			        SELECT 1 FROM session_task t WHERE t.history_id = s.history_id
			          AND t.state NOT IN ('TASK_STATE_COMPLETED', 'TASK_STATE_CANCELED', 'TASK_STATE_FAILED',
			              'TASK_STATE_REJECTED', 'TASK_STATE_INPUT_REQUIRED', 'TASK_STATE_AUTH_REQUIRED')) AS turn_running
			FROM session s WHERE s.id = $1
		`, pgx.RowToStructByName[eligibility], row.ID, row.PreparedRevision)
		if err != nil {
			return err
		}
		if !current.Admitted {
			switch {
			case current.AgentActive:
				return fmt.Errorf("the session's Agent is active: %w", ErrConflict)
			case current.Fork:
				return fmt.Errorf("the session is a fork of a checkpoint: %w", ErrConflict)
			case current.TurnRunning:
				return fmt.Errorf("a turn of the session runs: %w", ErrFailedPrecondition)
			}
		}
		operation, err = beginSessionDeletion(ctx, tx, row, sessionDeletionAgentDeleted)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("begin deleted-Agent Session deletion: %w", err)
	}
	return operation, nil
}
