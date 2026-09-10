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

// toAgentInstance decodes an instance and uses indexed columns for its identity, owner,
// revision, and lifecycle. Malformed payloads or unknown lifecycle values return
// an error.
func toAgentInstance(row agentInstanceRow) (*apiv1alpha1.AgentInstance, error) {
	instance := &apiv1alpha1.AgentInstance{}
	if err := proto.Unmarshal(row.Data, instance); err != nil {
		return nil, fmt.Errorf("decode AgentInstance %s: %w", row.ID, err)
	}
	state, ok := apiv1alpha1.AgentInstanceState_value[row.State]
	if !ok {
		return nil, fmt.Errorf("decode AgentInstance %s state %q", row.ID, row.State)
	}
	operationValue, ok := apiv1alpha1.AgentInstanceOperation_value[row.Operation]
	if !ok {
		return nil, fmt.Errorf("decode AgentInstance %s operation %q", row.ID, row.Operation)
	}
	// Columns own identity, authorization, revision retention and lifecycle.
	// Store updates write the same values to the payload in the same transaction.
	instance.State = apiv1alpha1.AgentInstanceState(state)
	instance.Operation = apiv1alpha1.AgentInstanceOperation(operationValue)
	instance.Id = row.ID.String()
	instance.ContextId = row.ContextID.String()
	instance.Creator = row.UserID
	instance.PreparedRevision = derefStr(row.PreparedRevision)
	return instance, nil
}

// marshalAgentInstance encodes an instance for durable storage, preserving protobuf fields
// unknown to this binary.
func marshalAgentInstance(instance *apiv1alpha1.AgentInstance) ([]byte, error) {
	data, err := proto.Marshal(instance)
	if err != nil {
		return nil, fmt.Errorf("encode AgentInstance %s: %w", instance.GetId(), err)
	}
	return data, nil
}

// sameAgentInstanceRequest reports whether two creation requests target the same harness
// and agent template. Names and other mutable fields do not affect retry identity.
func sameAgentInstanceRequest(instance, request *apiv1alpha1.AgentInstance) bool {
	return proto.Equal(instance.GetHarness(), request.GetHarness()) && proto.Equal(instance.GetAgentTemplate(), request.GetAgentTemplate())
}

// CreateAgentInstance atomically reserves an instance, its conversation history, and the
// latest successful runtime revision. A repeated creator/requestID returns the existing
// instance when the harness and template match, or ErrIdempotencyConflict otherwise. The
// boolean reports whether this call created the instance; runtime provisioning belongs to
// the caller.
func (c *Client) CreateAgentInstance(ctx context.Context, request *apiv1alpha1.AgentInstance, requestID string) (*apiv1alpha1.AgentInstance, bool, error) {
	existing, err := readAgentInstanceRequest(ctx, c.db, request.GetCreator(), requestID)
	if err == nil {
		instance, err := toAgentInstance(existing)
		if err == nil && !sameAgentInstanceRequest(instance, request) {
			return nil, false, ErrIdempotencyConflict
		}
		return instance, false, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("get AgentInstance request: %w", err)
	}

	var row agentInstanceRow
	err = c.withTx(ctx, func(tx pgx.Tx) error {
		row, err = insertAgentInstance(ctx, tx, request, requestID)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, err = readAgentInstanceRequest(ctx, c.db, request.GetCreator(), requestID)
		if err != nil {
			return nil, false, fmt.Errorf("get concurrent AgentInstance request: %w", err)
		}
		instance, err := toAgentInstance(existing)
		if err == nil && !sameAgentInstanceRequest(instance, request) {
			return nil, false, ErrIdempotencyConflict
		}
		return instance, false, err
	}
	if err != nil {
		return nil, false, fmt.Errorf("insert AgentInstance: %w", err)
	}
	instance, err := toAgentInstance(row)
	return instance, err == nil, err
}

// insertAgentInstance reserves a new conversation in CREATING/CREATE and pins the active
// template/harness pair's latest successful revision. Callers must supply a transaction so
// the history and instance commit together. A missing prepared target returns ErrNotFound;
// a duplicate creator/requestID returns pgx.ErrNoRows for the caller to resolve.
func insertAgentInstance(ctx context.Context, db pgx.Tx, request *apiv1alpha1.AgentInstance, requestID string) (agentInstanceRow, error) {
	type preparedRevision struct {
		Revision string
		DBTime   time.Time
	}
	revision, err := queryOne(ctx, db, `
		SELECT r.revision, clock_timestamp() AS db_time
		FROM agent_template_harness_pair p
		JOIN runtime_revision r ON r.revision = p.latest_successful_revision
		WHERE p.namespace = $1
		  AND p.namespace = $2
		  AND p.agent_template_name = $3
		  AND p.harness_name = $4
		  AND p.retired_at IS NULL
	`,
		pgx.RowToStructByName[preparedRevision], request.GetHarness().GetNamespace(),
		request.GetAgentTemplate().GetNamespace(), request.GetAgentTemplate().GetName(),
		request.GetHarness().GetName(),
	)
	if err != nil {
		return agentInstanceRow{}, fmt.Errorf("get latest successful runtime revision: %w", notFoundOr(err))
	}
	if _, err := getAvailableRuntimeRevisionForUpdate(ctx, db, revision.Revision); err != nil {
		return agentInstanceRow{}, err
	}
	instance := proto.CloneOf(request)
	contextID, historyID := uuid.New(), uuid.New()
	instance.ContextId = contextID.String()
	instance.PreparedRevision = revision.Revision
	instance.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING
	instance.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE
	instance.CreatedAt = timestamppb.New(revision.DBTime)
	instance.UpdatedAt = timestamppb.New(revision.DBTime)
	return insertAgentInstanceRecords(ctx, db, instance, requestID, historyID, nil)
}

// GetAgentInstanceByID returns an instance without filtering by owner, or ErrNotFound if
// absent. Callers must authorize access; malformed UUIDs return an error.
func (c *Client) GetAgentInstanceByID(ctx context.Context, id string) (*apiv1alpha1.AgentInstance, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid AgentInstance ID: %w", err)
	}
	row, err := readAgentInstance(ctx, c.db, uid.String())
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance %s: %w", id, notFoundOr(err))
	}
	return toAgentInstance(row)
}

// GetAgentInstance returns an instance only when it belongs to userID. Missing instances
// and instances owned by another user return ErrNotFound.
func (c *Client) GetAgentInstance(ctx context.Context, id, userID string) (*apiv1alpha1.AgentInstance, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT id, user_id, prepared_revision, state, data, operation, context_id,
		    source_checkpoint_id, history_id FROM agent_instance WHERE id = $1 AND user_id = $2
	`, pgx.RowToStructByName[agentInstanceRow], id, userID)
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance %s: %w", id, notFoundOr(err))
	}
	return toAgentInstance(row)
}

// ListAgentInstances returns up to Limit instances in ascending ID order after AfterID,
// filtered by optional template/harness references. It restricts results to
// UserID unless AllUsers is set; callers must authorize that broader access.
func (c *Client) ListAgentInstances(ctx context.Context, query AgentInstanceQuery) ([]*apiv1alpha1.AgentInstance, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT i.id, i.user_id, i.prepared_revision, i.state, i.data, i.operation,
		    i.context_id, i.source_checkpoint_id, i.history_id FROM agent_instance i
		LEFT JOIN runtime_revision r ON r.revision = i.prepared_revision
		WHERE ($1::boolean OR i.user_id = $2)
		  AND (NULLIF($3::text, '') IS NULL OR i.id > NULLIF($3::text, '')::uuid)
		  AND ($4::text = '' OR (r.agent_template_name = $4 AND r.namespace = $5))
		  AND ($6::text = '' OR (r.harness_name = $6 AND r.namespace = $7))
		ORDER BY i.id
		LIMIT $8
	`,
		pgx.RowToStructByName[agentInstanceRow], query.AllUsers, query.UserID, query.AfterID,
		query.AgentTemplate.GetName(), query.AgentTemplate.GetNamespace(), query.Harness.GetName(),
		query.Harness.GetNamespace(), int32(query.Limit),
	)
	if err != nil {
		return nil, fmt.Errorf("list AgentInstances: %w", err)
	}
	result := make([]*apiv1alpha1.AgentInstance, 0, len(rows))
	for _, row := range rows {
		instance, err := toAgentInstance(row)
		if err != nil {
			return nil, err
		}
		result = append(result, instance)
	}
	return result, nil
}

// UpdateAgentInstanceName renames an owned instance while preserving concurrent lifecycle
// changes. Missing instances and instances owned by another user return ErrNotFound.
func (c *Client) UpdateAgentInstanceName(ctx context.Context, id, userID, name string) (*apiv1alpha1.AgentInstance, error) {
	var result *apiv1alpha1.AgentInstance
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockAgentInstance(ctx, tx, id)
		if err != nil {
			return notFoundOr(err)
		}
		if row.UserID != userID {
			return ErrNotFound
		}
		instance, err := toAgentInstance(row)
		if err != nil {
			return err
		}
		instance.Name = name
		instance.UpdatedAt = timestamppb.Now()
		data, err := marshalAgentInstance(instance)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE agent_instance
			SET data = $1
			WHERE id = $2 AND user_id = $3
		`, data, row.ID, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrNotFound
		}
		result = instance
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("rename AgentInstance %s: %w", id, err)
	}
	return result, nil
}

// TransitionAgentInstance changes lifecycle fields only if the stored state and operation
// match the expected values. A mismatch, or a creating checkpoint when starting a new
// operation, returns ErrConflict. It preserves other instance fields; callers
// choose a valid transition and authorize it.
func (c *Client) TransitionAgentInstance(
	ctx context.Context,
	instance *apiv1alpha1.AgentInstance,
	expectedState apiv1alpha1.AgentInstanceState,
	expectedOperation apiv1alpha1.AgentInstanceOperation,
) (*apiv1alpha1.AgentInstance, error) {
	var result *apiv1alpha1.AgentInstance
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockAgentInstance(ctx, tx, instance.GetId())
		if err != nil {
			return notFoundOr(err)
		}
		result, err = toAgentInstance(row)
		if err != nil {
			return err
		}
		if result.State != expectedState || result.Operation != expectedOperation {
			return fmt.Errorf("AgentInstance lifecycle state or operation changed: %w", ErrConflict)
		}
		// Only lifecycle fields belong to this operation. Keep concurrent renames,
		// immutable indexed fields and unknown protobuf fields from the locked row.
		next := proto.Clone(result).(*apiv1alpha1.AgentInstance)
		next.State, next.Operation = instance.State, instance.Operation
		next.A2AAuthority = instance.A2AAuthority
		next.Failure = instance.Failure
		next.UpdatedAt = timestamppb.Now()
		data, err := marshalAgentInstance(next)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE agent_instance
			SET state = $1, operation = $2, data = $3
			WHERE agent_instance.id = $4
			  AND agent_instance.state = $5
			  AND agent_instance.operation = $6
			  AND (
			    $6::text <> 'AGENT_INSTANCE_OPERATION_UNSPECIFIED'
			    OR NOT EXISTS (
			      SELECT 1 FROM agent_instance_checkpoint c
			      WHERE c.source_instance_id = agent_instance.id AND c.state = 'CREATING'
			    )
			  )
		`,
			next.State.String(),
			next.Operation.String(), data, row.ID, expectedState.String(),
			expectedOperation.String(),
		)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("AgentInstance %s has a checkpoint being created: %w", instance.GetId(), ErrConflict)
		}
		result = next
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("transition AgentInstance %s: %w", instance.GetId(), err)
	}
	return result, nil
}

// DeleteAgentInstance removes an instance and its shares while retaining conversation
// history and checkpoints. A missing instance is a no-op. Callers authorize deletion and
// perform runtime cleanup separately.
func (c *Client) DeleteAgentInstance(ctx context.Context, id string) error {
	if err := execSQL(ctx, c.db, `
		DELETE FROM agent_instance WHERE id = $1
	`, id); err != nil {
		return fmt.Errorf("delete AgentInstance %s: %w", id, err)
	}
	return nil
}

type agentInstanceRow struct {
	ID                 uuid.UUID
	UserID             string
	PreparedRevision   *string
	State              string
	Data               []byte
	Operation          string
	ContextID          uuid.UUID
	SourceCheckpointID *uuid.UUID
	HistoryID          uuid.UUID
}

// lockAgentInstance returns an instance locked against concurrent updates until the
// caller's transaction ends. It does not filter by owner and returns pgx.ErrNoRows if
// absent.
func lockAgentInstance(ctx context.Context, db pgx.Tx, id string) (agentInstanceRow, error) {
	return queryOne(ctx, db, `
		SELECT id, user_id, prepared_revision, state, data, operation, context_id,
		    source_checkpoint_id, history_id FROM agent_instance WHERE id = $1 FOR UPDATE
	`, pgx.RowToStructByName[agentInstanceRow], id)
}

// readAgentInstanceRequest looks up a creation request by owner and request ID, returning
// pgx.ErrNoRows if absent. Callers compare the stored request before treating it as an
// idempotent retry.
func readAgentInstanceRequest(ctx context.Context, db dbExecutor, userID, requestID string) (agentInstanceRow, error) {
	return queryOne(ctx, db, `
		SELECT id, user_id, prepared_revision, state, data, operation, context_id,
		    source_checkpoint_id, history_id FROM agent_instance
		WHERE user_id = $1 AND request_id = $2
	`, pgx.RowToStructByName[agentInstanceRow], userID, requestID)
}

// readAgentInstance reads an instance without checking ownership or locking it. Missing
// instances return pgx.ErrNoRows; callers authorize access.
func readAgentInstance(ctx context.Context, db dbExecutor, id string) (agentInstanceRow, error) {
	return queryOne(ctx, db, `
		SELECT id, user_id, prepared_revision, state, data, operation, context_id,
		    source_checkpoint_id, history_id FROM agent_instance WHERE id = $1
	`, pgx.RowToStructByName[agentInstanceRow], id)
}

// insertAgentInstanceRecords stores a new instance and its independent history together.
// Callers supply a transaction and a prepared CREATING/CREATE instance; a duplicate
// creator/requestID returns pgx.ErrNoRows so the caller can roll back and resolve it.
// A fork records immutable parent history/cutoff metadata separately from the instance's
// source checkpoint, so ancestry does not depend on checkpoint rows or instance lifetime.
func insertAgentInstanceRecords(ctx context.Context, db dbExecutor, instance *apiv1alpha1.AgentInstance, requestID string, historyID uuid.UUID, source *agentInstanceCheckpointRow) (agentInstanceRow, error) {
	data, err := marshalAgentInstance(instance)
	if err != nil {
		return agentInstanceRow{}, err
	}
	var sourceCheckpointID, parentHistoryID *uuid.UUID
	var parentHistorySequence *int64
	if source != nil {
		sourceCheckpointID = &source.ID
		parentHistoryID = &source.SourceHistoryID
		parentHistorySequence = &source.HistorySequence
	}
	if err := execSQL(ctx, db, `
		INSERT INTO agent_history (id, user_id, context_id, parent_history_id, parent_history_sequence)
		VALUES ($1, $2, $3, $4, $5)
	`, historyID, instance.Creator, instance.ContextId, parentHistoryID, parentHistorySequence); err != nil {
		return agentInstanceRow{}, fmt.Errorf("insert agent history: %w", err)
	}
	return queryOne(ctx, db, `
		INSERT INTO agent_instance (id, user_id, request_id, context_id, history_id, prepared_revision,
		    source_checkpoint_id, state, operation, data) VALUES ($1, $2, $3, $4, $5, $6, $7::uuid,
		    'AGENT_INSTANCE_STATE_CREATING', 'AGENT_INSTANCE_OPERATION_CREATE', $8)
		ON CONFLICT (user_id, request_id) DO NOTHING
		RETURNING id, user_id, prepared_revision, state, data, operation, context_id,
		    source_checkpoint_id, history_id
	`,
		pgx.RowToStructByName[agentInstanceRow], instance.Id, instance.Creator, requestID, instance.ContextId,
		historyID, instance.PreparedRevision, sourceCheckpointID, data,
	)
}
