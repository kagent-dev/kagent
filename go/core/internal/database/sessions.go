package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// toSession decodes a session and uses indexed columns for its identity, owner,
// revision, and lifecycle. Malformed payloads or unknown lifecycle values return
// an error.
func toSession(row sessionRow) (*apiv1alpha1.Session, error) {
	session := &apiv1alpha1.Session{}
	if err := proto.Unmarshal(row.Data, session); err != nil {
		return nil, fmt.Errorf("decode Session %s: %w", row.ID, err)
	}
	state, ok := apiv1alpha1.SessionState_value[row.State]
	if !ok {
		return nil, fmt.Errorf("decode Session %s state %q", row.ID, row.State)
	}
	operationValue, ok := apiv1alpha1.SessionOperation_value[row.Operation]
	if !ok {
		return nil, fmt.Errorf("decode Session %s operation %q", row.ID, row.Operation)
	}
	// Columns own identity, authorization, revision retention and lifecycle.
	// Store updates write the same values to the payload in the same transaction.
	session.State = apiv1alpha1.SessionState(state)
	session.Operation = apiv1alpha1.SessionOperation(operationValue)
	session.Id = row.ID.String()
	session.ContextId = row.ContextID.String()
	session.Creator = row.UserID
	session.PreparedRevision = derefStr(row.PreparedRevision)
	return session, nil
}

// marshalSession encodes a session for durable storage, preserving protobuf fields
// unknown to this binary.
func marshalSession(session *apiv1alpha1.Session) ([]byte, error) {
	data, err := proto.Marshal(session)
	if err != nil {
		return nil, fmt.Errorf("encode Session %s: %w", session.GetId(), err)
	}
	return data, nil
}

// sameSessionRequest reports whether two creation requests target the same Agent. Names and other mutable fields do not affect retry identity.
func sameSessionRequest(session, request *apiv1alpha1.Session) bool {
	return proto.Equal(session.GetAgent(), request.GetAgent())
}

// CreateSession atomically reserves a session, its conversation history, and the
// latest successful runtime revision. A repeated creator/requestID returns the existing
// session when the Agent matches, or ErrIdempotencyConflict otherwise. A
// deleted session returns ErrFailedPrecondition and keeps its request ID reserved. The
// boolean reports whether this call created the session; runtime provisioning belongs to
// the caller.
func (c *Client) CreateSession(ctx context.Context, request *apiv1alpha1.Session, requestID string) (*apiv1alpha1.Session, bool, error) {
	existing, err := readSessionRequest(ctx, c.db, request.GetCreator(), requestID)
	if err == nil {
		session, err := toSession(existing)
		if err == nil && (existing.SourceCheckpointID != nil || !sameSessionRequest(session, request)) {
			return nil, false, ErrIdempotencyConflict
		}
		if err == nil && session.State == apiv1alpha1.SessionState_SESSION_STATE_DELETED {
			return nil, false, ErrFailedPrecondition
		}
		return session, false, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("get Session request: %w", err)
	}

	var row sessionRow
	err = c.withTx(ctx, func(tx pgx.Tx) error {
		row, err = insertSession(ctx, tx, request, requestID)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err = readSessionRequest(ctx, c.db, request.GetCreator(), requestID)
		if err != nil {
			return nil, false, fmt.Errorf("get concurrent Session request: %w", err)
		}
		session, err := toSession(existing)
		if err == nil && (existing.SourceCheckpointID != nil || !sameSessionRequest(session, request)) {
			return nil, false, ErrIdempotencyConflict
		}
		if err == nil && session.State == apiv1alpha1.SessionState_SESSION_STATE_DELETED {
			return nil, false, ErrFailedPrecondition
		}
		return session, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("insert Session: %w", err)
	}
	session, err := toSession(row)
	return session, err == nil, err
}

// insertSession reserves a new conversation in CREATING/CREATE and pins the active
// Agent's latest successful revision. Callers must supply a transaction so
// the history and session commit together. A missing prepared target returns ErrNotFound;
// a duplicate creator/requestID returns pgx.ErrNoRows for the caller to resolve.
func insertSession(ctx context.Context, db pgx.Tx, request *apiv1alpha1.Session, requestID string) (sessionRow, error) {
	type preparedRevision struct {
		Revision string
		DBTime   time.Time
	}
	revision, err := queryOne(ctx, db, `
		SELECT r.revision, clock_timestamp() AS db_time
		FROM agent_definition p
		JOIN runtime_revision r ON r.revision = p.latest_successful_revision
  WHERE p.namespace = $1 AND p.agent_name = $2 AND p.retired_at IS NULL
 `, pgx.RowToStructByName[preparedRevision], request.GetAgent().GetNamespace(), request.GetAgent().GetName())
	if err != nil {
		return sessionRow{}, fmt.Errorf("get latest successful runtime revision: %w", notFoundOr(err))
	}
	if _, err := getAvailableRuntimeRevisionForUpdate(ctx, db, revision.Revision); err != nil {
		return sessionRow{}, err
	}
	session := proto.CloneOf(request)
	historyID := uuid.New()
	session.ContextId = session.Id
	session.PreparedRevision = revision.Revision
	session.State = apiv1alpha1.SessionState_SESSION_STATE_CREATING
	session.Operation = apiv1alpha1.SessionOperation_SESSION_OPERATION_CREATE
	session.CreatedAt = timestamppb.New(revision.DBTime)
	session.UpdatedAt = timestamppb.New(revision.DBTime)
	return insertSessionRecords(ctx, db, session, requestID, historyID, nil)
}

// GetSessionByID returns a session without filtering by owner, or ErrNotFound if
// absent or deleted. Callers must authorize access; malformed UUIDs return an error.
func (c *Client) GetSessionByID(ctx context.Context, id string) (*apiv1alpha1.Session, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid Session ID: %w", err)
	}
	row, err := readSession(ctx, c.db, uid.String())
	if err != nil {
		return nil, fmt.Errorf("get Session %s: %w", id, notFoundOr(err))
	}
	return toSession(row)
}

// GetSessionForRuntime binds a verified Substrate actor UID to the
// session created for it. Deleted sessions and obsolete actors cannot access
// retained task data. Actor identity is private and is never caller metadata.
func (c *Client) GetSessionForRuntime(ctx context.Context, id, actorUID string) (*apiv1alpha1.Session, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT id, user_id, prepared_revision, state, data, operation, context_id,
		    source_checkpoint_id, history_id, operation_id, executor_id
		FROM session WHERE id = $1 AND actor_uid = $2
		    AND state <> 'SESSION_STATE_DELETED'
	`, pgx.RowToStructByName[sessionRow], id, actorUID)
	if err != nil {
		return nil, notFoundOr(err)
	}
	return toSession(row)
}

// GetSession returns a session only when it belongs to userID. Missing sessions
// and sessions owned by another user return ErrNotFound. Tombstones are hidden.
func (c *Client) GetSession(ctx context.Context, id, userID string) (*apiv1alpha1.Session, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT id, user_id, prepared_revision, state, data, operation, context_id,
		    source_checkpoint_id, history_id, operation_id, executor_id FROM session WHERE id = $1 AND user_id = $2 AND state <> 'SESSION_STATE_DELETED'
	`, pgx.RowToStructByName[sessionRow], id, userID)
	if err != nil {
		return nil, fmt.Errorf("get Session %s: %w", id, notFoundOr(err))
	}
	return toSession(row)
}

// ListSessions returns up to Limit sessions in ascending ID order after AfterID,
// filtered by optional Agent reference. It restricts results to
// UserID unless AllUsers is set; callers must authorize that broader access.
func (c *Client) ListSessions(ctx context.Context, query SessionQuery) ([]*apiv1alpha1.Session, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT i.id, i.user_id, i.prepared_revision, i.state, i.data, i.operation,
		    i.context_id, i.source_checkpoint_id, i.history_id, i.operation_id, i.executor_id FROM session i
		LEFT JOIN runtime_revision r ON r.revision = i.prepared_revision
		WHERE i.state <> 'SESSION_STATE_DELETED' AND ($1::boolean OR i.user_id = $2)
		  AND (NULLIF($3::text, '') IS NULL OR i.id > NULLIF($3::text, '')::uuid)
		  AND ($4::text = '' OR (r.agent_name = $4 AND r.namespace = $5))
		ORDER BY i.id
		LIMIT $6
	`,
		pgx.RowToStructByName[sessionRow], query.AllUsers, query.UserID, query.AfterID,
		query.Agent.GetName(), query.Agent.GetNamespace(), int32(query.Limit),
	)
	if err != nil {
		return nil, fmt.Errorf("list Sessions: %w", err)
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

// UpdateSessionName renames an owned session while preserving concurrent lifecycle
// changes. Missing sessions and sessions owned by another user return ErrNotFound.
func (c *Client) UpdateSessionName(ctx context.Context, id, userID, name string) (*apiv1alpha1.Session, error) {
	var result *apiv1alpha1.Session
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockSession(ctx, tx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if row.UserID != userID || row.State == apiv1alpha1.SessionState_SESSION_STATE_DELETED.String() {
			return ErrNotFound
		}
		session, err := toSession(row)
		if err != nil {
			return err
		}
		session.Name = name
		session.UpdatedAt = timestamppb.Now()
		data, err := marshalSession(session)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE session
			SET data = $1
			WHERE id = $2 AND user_id = $3
		`, data, row.ID, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrNotFound
		}
		result = session
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("rename Session %s: %w", id, err)
	}
	return result, nil
}

type sessionRow struct {
	ID                 uuid.UUID
	UserID             string
	PreparedRevision   *string
	State              string
	Data               []byte
	Operation          string
	ContextID          uuid.UUID
	SourceCheckpointID *uuid.UUID
	OperationID        *uuid.UUID
	ExecutorID         *uuid.UUID
	HistoryID          uuid.UUID
}

// lockSession returns a session locked against concurrent updates until the
// caller's transaction ends. It does not filter by owner and returns pgx.ErrNoRows if
// absent. Tombstones are included so lifecycle completion can observe deletion.
func lockSession(ctx context.Context, db pgx.Tx, id string) (sessionRow, error) {
	return queryOne(ctx, db, `
		SELECT id, user_id, prepared_revision, state, data, operation, context_id,
		    source_checkpoint_id, history_id, operation_id, executor_id FROM session WHERE id = $1 FOR UPDATE
	`, pgx.RowToStructByName[sessionRow], id)
}

// readSessionRequest looks up a creation request by owner and request ID, returning
// pgx.ErrNoRows if absent. Callers compare the stored request before treating it as an
// idempotent retry.
func readSessionRequest(ctx context.Context, db dbExecutor, userID, requestID string) (sessionRow, error) {
	return queryOne(ctx, db, `
		SELECT id, user_id, prepared_revision, state, data, operation, context_id,
		    source_checkpoint_id, history_id, operation_id, executor_id FROM session
		WHERE user_id = $1 AND request_id = $2
	`, pgx.RowToStructByName[sessionRow], userID, requestID)
}

// readSession reads a session without checking ownership or locking it. Missing
// or deleted sessions return pgx.ErrNoRows; callers authorize access.
func readSession(ctx context.Context, db dbExecutor, id string) (sessionRow, error) {
	return queryOne(ctx, db, `
		SELECT id, user_id, prepared_revision, state, data, operation, context_id,
		    source_checkpoint_id, history_id, operation_id, executor_id FROM session WHERE id = $1 AND state <> 'SESSION_STATE_DELETED'
	`, pgx.RowToStructByName[sessionRow], id)
}

// insertSessionRecords stores a new session and its independent history together.
// Callers supply a transaction and a prepared CREATING/CREATE session; a duplicate
// creator/requestID returns pgx.ErrNoRows so the caller can roll back and resolve it.
func insertSessionRecords(ctx context.Context, db dbExecutor, session *apiv1alpha1.Session, requestID string, historyID uuid.UUID, sourceCheckpointID *uuid.UUID) (sessionRow, error) {
	data, err := marshalSession(session)
	if err != nil {
		return sessionRow{}, err
	}
	if err := execSQL(ctx, db, `
		INSERT INTO a2a_context (id, context_id) VALUES ($1, $2)
	`, historyID, session.ContextId); err != nil {
		return sessionRow{}, fmt.Errorf("insert A2A context: %w", err)
	}
	return queryOne(ctx, db, `
		INSERT INTO session (id, user_id, request_id, context_id, history_id, prepared_revision,
		    source_checkpoint_id, state, operation, data) VALUES ($1, $2, $3, $4, $5, $6, $7::uuid,
		    'SESSION_STATE_CREATING', 'SESSION_OPERATION_CREATE', $8)
		ON CONFLICT (user_id, request_id) DO NOTHING
		RETURNING id, user_id, prepared_revision, state, data, operation, context_id,
		    source_checkpoint_id, history_id, operation_id, executor_id
	`,
		pgx.RowToStructByName[sessionRow], session.Id, session.Creator, requestID, session.ContextId,
		historyID, session.PreparedRevision, sourceCheckpointID, data,
	)
}

// tombstoneSession releases runtime references and revokes shares while
// retaining owner and request identity. Callers hold the session lock and have
// established that no uncertain executor can still act. History remains retained.
func tombstoneSession(ctx context.Context, tx pgx.Tx, session *apiv1alpha1.Session, operationID *uuid.UUID) error {
	session.State = apiv1alpha1.SessionState_SESSION_STATE_DELETED
	session.Operation = apiv1alpha1.SessionOperation_SESSION_OPERATION_UNSPECIFIED
	session.PreparedRevision, session.A2AAuthority = "", ""
	session.UpdatedAt = timestamppb.Now()
	data, err := marshalSession(session)
	if err != nil {
		return err
	}
	if err := execSQL(ctx, tx, `
		UPDATE session SET state = 'SESSION_STATE_DELETED',
		    operation = 'SESSION_OPERATION_UNSPECIFIED', prepared_revision = NULL,
		    data = $2, operation_id = $3, executor_id = NULL WHERE id = $1
	`, session.Id, data, operationID); err != nil {
		return err
	}
	return execSQL(ctx, tx, `DELETE FROM session_share WHERE session_id = $1`, session.Id)
}
