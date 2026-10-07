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
// not listed: it keeps its pinned revision until it idles out. Admission
// rechecks the Agent under the session lock.
func (c *Client) ListSessionsOfDeletedAgents(ctx context.Context, afterID string, limit int) ([]string, error) {
	return queryMany(ctx, c.db, `
		SELECT s.id::text FROM session s
		JOIN runtime_instance r USING (id)
		JOIN agent_runtime_revision prepared ON prepared.revision = r.prepared_revision
		WHERE (s.deletion_reason = 'agent_deleted'
		    OR (r.state <> 'RUNTIME_STATE_DELETED' AND NOT EXISTS (
		        SELECT 1 FROM agent_definition p
		        WHERE p.namespace = prepared.namespace AND p.agent_name = prepared.agent_name
		          AND p.retired_at IS NULL)))
		  AND ($1::text = '' OR s.id > $1::uuid)
		ORDER BY s.id LIMIT $2
	`, pgx.RowTo[string], afterID, limit)
}

// BeginDeletedAgentSessionDeletion admits the deletion of a session whose Agent
// is gone, or joins the deletion already admitted for it, under the session
// lock. An Agent active again under the name since the listing leaves the
// session alone with ErrConflict; a deleted session answers ErrNotFound. The
// admitted reason survives retries, so a later sweep finishes a deletion whose
// runtime work failed.
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
		gone, err := queryOne(ctx, tx, `
			SELECT COALESCE(deletion_reason, '') = 'agent_deleted' OR NOT EXISTS (
			    SELECT 1 FROM agent_runtime_revision prepared
			    JOIN agent_definition p ON p.namespace = prepared.namespace AND p.agent_name = prepared.agent_name
			    WHERE prepared.revision = $2 AND p.retired_at IS NULL)
			FROM session WHERE id = $1
		`, pgx.RowTo[bool], row.ID, row.PreparedRevision)
		if err != nil {
			return err
		}
		if !gone {
			return fmt.Errorf("the session's Agent is active: %w", ErrConflict)
		}
		operation, err = beginSessionDeletion(ctx, tx, row, sessionDeletionAgentDeleted)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("begin deleted-Agent Session deletion: %w", err)
	}
	return operation, nil
}
