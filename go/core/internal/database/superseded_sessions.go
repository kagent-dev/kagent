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

// ListSessionsOnSupersededRevisions returns READY sessions whose runtime is
// settled — no lifecycle operation, no dispatch reserved, no turn running and
// no idle boundary claimed — and whose prepared revision is no longer their
// agent's current one, by id after afterID, up to limit. The current revision
// is the latest successful revision of the agent the prepared revision was
// rendered for; a session of a retired agent, or of an agent without a current
// revision, has nowhere to move and is left out, as is one whose current
// revision lives in another atespace. Callers authorize access.
func (c *Client) ListSessionsOnSupersededRevisions(ctx context.Context, afterID string, limit int) ([]*apiv1alpha1.Session, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT i.id, i.user_id, i.prepared_revision, i.state, i.data, i.operation, i.context_id,
		    i.source_checkpoint_id, i.history_id, i.operation_id, i.executor_id
		FROM session_record i
		JOIN agent_runtime_revision prepared ON prepared.revision = i.prepared_revision
		JOIN agent_definition p
		  ON p.namespace = prepared.namespace
		 AND p.agent_uid = prepared.agent_uid
		 AND p.retired_at IS NULL
		JOIN agent_runtime_revision current ON current.revision = p.latest_successful_revision AND current.deleted_at IS NULL
		WHERE i.state = 'RUNTIME_STATE_READY'
		  AND i.operation = 'RUNTIME_OPERATION_NONE'
		  AND (i.dispatch_expires_at IS NULL OR i.dispatch_expires_at <= clock_timestamp())
		  AND current.revision <> prepared.revision
		  AND current.actor_template_atespace = prepared.actor_template_atespace
		  AND NOT EXISTS (SELECT 1 FROM session_task WHERE history_id = i.history_id
		      AND state IN ('TASK_STATE_SUBMITTED', 'TASK_STATE_WORKING'))
		  AND NOT EXISTS (SELECT 1 FROM session_task_event WHERE history_id = i.history_id
		      AND (NOT published OR (quiescence_pending AND quiescence_executor_id IS NOT NULL)))
		  AND ($1 = '' OR i.id > $1::uuid)
		ORDER BY i.id
		LIMIT $2
	`, pgx.RowToStructByName[sessionRow], afterID, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("list Sessions on superseded revisions: %w", err)
	}
	result := make([]*apiv1alpha1.Session, 0, len(rows))
	for _, row := range rows {
		session, err := toSession(row)
		if err != nil {
			return nil, err
		}
		result = append(result, session)
	}
	return result, nil
}

// RepointSessionOperation records that the claiming executor of a resume moved
// the session's Actor onto revision, the agent's current one.
func (c *Client) RepointSessionOperation(ctx context.Context, sessionID string, id, executorID uuid.UUID, revision string) error {
	if id == uuid.Nil || executorID == uuid.Nil {
		return fmt.Errorf("lifecycle generation and executor IDs are required")
	}
	_, err := c.repointSession(ctx, sessionID, revision, func(operation *SessionOperation) bool {
		return operation.ID == id && operation.ExecutorID == executorID &&
			operation.Instance.Operation == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME
	})
	return err
}

// RepointSession records that the quiesced Actor of a READY session moved from
// revision from onto revision to, the agent's current one. A session another
// caller has recorded on to meanwhile is returned as it is: the gateway's
// repoint before a turn and the sweep of superseded revisions may move the
// same Actor. It answers ErrConflict, with the session as it is, when the
// session is no longer READY on from or to without a lifecycle operation: an
// operation that claimed it meanwhile wins.
func (c *Client) RepointSession(ctx context.Context, sessionID, from, to string) (*apiv1alpha1.Session, error) {
	return c.repointSession(ctx, sessionID, to, func(operation *SessionOperation) bool {
		session := operation.Instance
		return (session.PreparedRevision == from || session.PreparedRevision == to) &&
			session.State == apiv1alpha1.RuntimeState_RUNTIME_STATE_READY &&
			session.Operation == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE
	})
}

// repointSession moves the session's prepared revision to revision when owned
// accepts the locked session; a session already on revision is left as it is.
// The revision row is locked so the runtime revision GC cannot collect it
// concurrently; the superseded revision is released with the same update. A
// refused repoint returns the session as it is with ErrConflict.
func (c *Client) repointSession(ctx context.Context, sessionID, revision string, owned func(*SessionOperation) bool) (*apiv1alpha1.Session, error) {
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
		result = operation.Instance
		if !owned(operation) {
			return fmt.Errorf("session changed before its repoint was recorded: %w", ErrConflict)
		}
		if result.PreparedRevision == revision {
			return nil
		}
		if _, err := getAvailableRuntimeRevisionForUpdate(ctx, tx, revision); err != nil {
			return err
		}
		result.PreparedRevision = revision
		result.UpdatedAt = timestamppb.Now()
		data, err := marshalSession(result)
		if err != nil {
			return err
		}
		if err := execSQL(ctx, tx, `UPDATE runtime_instance SET prepared_revision = $2 WHERE id = $1 AND kind = 'agent'`, sessionID, revision); err != nil {
			return err
		}
		return execSQL(ctx, tx, `UPDATE session SET data = $2 WHERE id = $1`, sessionID, data)
	})
	if errors.Is(err, ErrConflict) {
		return result, fmt.Errorf("repoint Session %s to revision %s: %w", sessionID, revision, err)
	}
	if err != nil {
		return nil, fmt.Errorf("repoint Session %s to revision %s: %w", sessionID, revision, err)
	}
	return result, nil
}
