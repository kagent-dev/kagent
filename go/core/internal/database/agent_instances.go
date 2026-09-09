package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func toAgentInstance(row agentInstanceRow) (*apiv1alpha1.AgentInstance, error) {
	instance := &apiv1alpha1.AgentInstance{}
	if err := proto.Unmarshal(row.Data, instance); err != nil {
		return nil, fmt.Errorf("decode AgentInstance %s: %w", row.ID, err)
	}
	state, ok := apiv1alpha1.AgentInstanceState_value["AGENT_INSTANCE_STATE_"+row.State]
	if !ok {
		return nil, fmt.Errorf("decode AgentInstance %s state %q", row.ID, row.State)
	}
	operation := row.Operation
	if operation == "NONE" {
		operation = "UNSPECIFIED"
	}
	operationValue, ok := apiv1alpha1.AgentInstanceOperation_value["AGENT_INSTANCE_OPERATION_"+operation]
	if !ok {
		return nil, fmt.Errorf("decode AgentInstance %s operation %q", row.ID, row.Operation)
	}
	// Columns own identity, authorization, revision retention, query labels and lifecycle.
	// Store updates write the same values to the payload in the same transaction.
	instance.State = apiv1alpha1.AgentInstanceState(state)
	instance.Operation = apiv1alpha1.AgentInstanceOperation(operationValue)
	instance.Id = row.ID.String()
	instance.ContextId = row.ContextID.String()
	instance.Creator = row.UserID
	instance.PreparedRevision = derefStr(row.PreparedRevision)
	instance.Labels = nil
	if len(row.Labels) > 0 {
		if err := json.Unmarshal(row.Labels, &instance.Labels); err != nil {
			return nil, fmt.Errorf("decode AgentInstance labels: %w", err)
		}
	}
	return instance, nil
}

func marshalAgentInstance(instance *apiv1alpha1.AgentInstance) ([]byte, error) {
	data, err := proto.Marshal(instance)
	if err != nil {
		return nil, fmt.Errorf("encode AgentInstance %s: %w", instance.GetId(), err)
	}
	return data, nil
}

func sameAgentInstanceRequest(instance, request *apiv1alpha1.AgentInstance) bool {
	return proto.Equal(instance.GetHarness(), request.GetHarness()) && proto.Equal(instance.GetAgentTemplate(), request.GetAgentTemplate())
}

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

// insertAgentInstance reserves the conversation and pins its prepared revision.
// Callers own the transaction so execution linkage can commit with creation.
func insertAgentInstance(ctx context.Context, db dbExecutor, request *apiv1alpha1.AgentInstance, requestID string) (agentInstanceRow, error) {
	revision, err := queryOne(ctx, db, `
		SELECT r.revision, r.namespace, r.agent_template_name, r.agent_template_uid, r.harness_name, r.harness_uid,
		    r.source_snapshot, r.egress_destinations, r.actor_template_atespace, r.actor_template_name,
		    r.actor_template_uid, r.created_at, r.updated_at, r.agent_card, p.agent_template_labels,
		    clock_timestamp()::timestamptz AS db_time
		FROM agent_template_harness_pair p
		JOIN runtime_revision r ON r.revision = p.latest_successful_revision
		WHERE p.namespace = $1
		  AND p.namespace = $2
		  AND p.agent_template_name = $3
		  AND p.harness_name = $4
		  AND p.retired_at IS NULL
	`,
		pgx.RowToStructByName[instanceRuntimeRevisionRow], request.GetHarness().GetNamespace(),
		request.GetAgentTemplate().GetNamespace(), request.GetAgentTemplate().GetName(),
		request.GetHarness().GetName(),
	)
	if err != nil {
		return agentInstanceRow{}, fmt.Errorf("get latest successful runtime revision: %w", notFoundOr(err))
	}
	labels := map[string]string{}
	if err := json.Unmarshal(revision.AgentTemplateLabels, &labels); err != nil {
		return agentInstanceRow{}, fmt.Errorf("decode AgentTemplate labels: %w", err)
	}
	instance := proto.CloneOf(request)
	contextID, historyID := uuid.New(), uuid.New()
	instance.ContextId = contextID.String()
	instance.PreparedRevision = revision.Revision
	instance.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING
	instance.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE
	instance.Labels = labels
	instance.CreatedAt = timestamppb.New(revision.DBTime)
	instance.UpdatedAt = timestamppb.New(revision.DBTime)
	data, err := marshalAgentInstance(instance)
	if err != nil {
		return agentInstanceRow{}, err
	}
	instanceID := uuid.MustParse(request.GetId())

	if err := execSQL(ctx, db, `
		INSERT INTO a2a_context (id, user_id, context_id) VALUES ($1, $2, $3)
	`, historyID, instance.Creator, contextID); err != nil {
		return agentInstanceRow{}, fmt.Errorf("insert A2A context: %w", err)
	}
	return queryOne(ctx, db, `
		INSERT INTO agent_instance (id, user_id, request_id, context_id, history_id, prepared_revision,
		    source_checkpoint_id, state, operation, labels, data) VALUES ($1, $2, $3, $4, $5, $6, $9::uuid,
		    'CREATING', 'CREATE', $7, $8)
		ON CONFLICT (user_id, request_id) DO NOTHING
		RETURNING id, user_id, request_id, prepared_revision, state, labels, data, operation, context_id,
		    source_checkpoint_id, history_id
	`,
		pgx.RowToStructByName[agentInstanceRow], instanceID, instance.Creator, requestID, contextID, historyID,
		&revision.Revision, revision.AgentTemplateLabels, data, nil,
	)
}

func (c *Client) GetAgentInstanceByID(ctx context.Context, id string) (*apiv1alpha1.AgentInstance, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid AgentInstance ID: %w", err)
	}
	row, err := queryOne(ctx, c.db, `
		SELECT id, user_id, request_id, prepared_revision, state, labels, data, operation, context_id,
		    source_checkpoint_id, history_id FROM agent_instance WHERE id = $1
	`, pgx.RowToStructByName[agentInstanceRow], uid)
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance %s: %w", id, notFoundOr(err))
	}
	return toAgentInstance(row)
}

func (c *Client) GetAgentInstance(ctx context.Context, id, userID string) (*apiv1alpha1.AgentInstance, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT id, user_id, request_id, prepared_revision, state, labels, data, operation, context_id,
		    source_checkpoint_id, history_id FROM agent_instance WHERE id = $1 AND user_id = $2
	`, pgx.RowToStructByName[agentInstanceRow], uuid.MustParse(id), userID)
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance %s: %w", id, notFoundOr(err))
	}
	return toAgentInstance(row)
}

func (c *Client) ListAgentInstances(ctx context.Context, query AgentInstanceQuery) ([]*apiv1alpha1.AgentInstance, error) {
	matchLabels := query.MatchLabels
	if matchLabels == nil {
		matchLabels = map[string]string{}
	}
	labels, err := json.Marshal(matchLabels)
	if err != nil {
		return nil, fmt.Errorf("marshal AgentInstance label selector: %w", err)
	}
	rows, err := queryMany(ctx, c.db, `
		SELECT i.id, i.user_id, i.request_id, i.prepared_revision, i.state, i.labels, i.data, i.operation,
		    i.context_id, i.source_checkpoint_id, i.history_id FROM agent_instance i
		LEFT JOIN runtime_revision r ON r.revision = i.prepared_revision
		WHERE ($1::boolean OR i.user_id = $2)
		  AND (NULLIF($3::text, '') IS NULL OR i.id > NULLIF($3::text, '')::uuid)
		  AND i.labels @> $4::jsonb
		  AND ($5::text = '' OR (r.agent_template_name = $5 AND r.namespace = $6))
		  AND ($7::text = '' OR (r.harness_name = $7 AND r.namespace = $8))
		ORDER BY i.id
		LIMIT $9
	`,
		pgx.RowToStructByName[agentInstanceRow], query.AllUsers, query.UserID, query.AfterID, labels,
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

// UpdateAgentInstanceName serializes with lifecycle updates so neither loses the other's fields.
func (c *Client) UpdateAgentInstanceName(ctx context.Context, id, userID, name string) (*apiv1alpha1.AgentInstance, error) {
	var result *apiv1alpha1.AgentInstance
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockAgentInstance(ctx, tx, uuid.MustParse(id))
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
		row, err = queryOne(ctx, tx, `
			UPDATE agent_instance
			SET data = $1
			WHERE id = $2 AND user_id = $3
			RETURNING id, user_id, request_id, prepared_revision, state, labels, data, operation, context_id,
			    source_checkpoint_id, history_id
		`, pgx.RowToStructByName[agentInstanceRow], data, row.ID, userID)
		if err != nil {
			return err
		}
		result, err = toAgentInstance(row)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("rename AgentInstance %s: %w", id, err)
	}
	return result, nil
}

func (c *Client) MarkAgentInstanceReady(ctx context.Context, id, authority string) (*apiv1alpha1.AgentInstance, error) {
	var result *apiv1alpha1.AgentInstance
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockAgentInstance(ctx, tx, uuid.MustParse(id))
		if err != nil {
			return notFoundOr(err)
		}
		result, err = toAgentInstance(row)
		if err != nil || row.State != "CREATING" || row.Operation != "CREATE" {
			return err
		}
		result.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY
		result.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED
		result.A2AAuthority = authority
		result.Failure = nil
		result.UpdatedAt = timestamppb.Now()
		data, err := marshalAgentInstance(result)
		if err != nil {
			return err
		}
		_, err = queryOne(ctx, tx, `
			UPDATE agent_instance
			SET state = 'READY', operation = 'NONE', data = $2
			WHERE id = $1 AND state = 'CREATING' AND operation = 'CREATE'
			RETURNING id, user_id, request_id, prepared_revision, state, labels, data, operation, context_id,
			    source_checkpoint_id, history_id
		`, pgx.RowToStructByName[agentInstanceRow], row.ID, data)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("mark AgentInstance %s ready: %w", id, err)
	}
	return result, nil
}

func (c *Client) TransitionAgentInstance(
	ctx context.Context,
	instance *apiv1alpha1.AgentInstance,
	expectedState apiv1alpha1.AgentInstanceState,
	expectedOperation apiv1alpha1.AgentInstanceOperation,
) (*apiv1alpha1.AgentInstance, error) {
	var result *apiv1alpha1.AgentInstance
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockAgentInstance(ctx, tx, uuid.MustParse(instance.GetId()))
		if err != nil {
			return notFoundOr(err)
		}
		result, err = toAgentInstance(row)
		if err != nil {
			return err
		}
		if result.State != expectedState || result.Operation != expectedOperation {
			return ErrAgentInstanceConflict
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
		row, err = queryOne(ctx, tx, `
			UPDATE agent_instance
			SET state = $1, operation = $2, data = $3
			WHERE agent_instance.id = $4
			  AND agent_instance.state = $5
			  AND agent_instance.operation = $6
			  AND (
			    $6::text <> 'NONE'
			    OR NOT EXISTS (
			      SELECT 1 FROM agent_instance_checkpoint c
			      WHERE c.source_instance_id = agent_instance.id AND c.state = 'CREATING'
			    )
			  )
			RETURNING id, user_id, request_id, prepared_revision, state, labels, data, operation, context_id,
			    source_checkpoint_id, history_id
		`,
			pgx.RowToStructByName[agentInstanceRow], agentInstanceStateName(next.State),
			agentInstanceOperationName(next.Operation), data, row.ID, agentInstanceStateName(expectedState),
			agentInstanceOperationName(expectedOperation),
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAgentInstanceConflict
		}
		if err != nil {
			return err
		}
		result, err = toAgentInstance(row)
		return err
	})
	if err != nil {
		return result, fmt.Errorf("transition AgentInstance %s: %w", instance.GetId(), err)
	}
	return result, nil
}

func agentInstanceStateName(state apiv1alpha1.AgentInstanceState) string {
	return strings.TrimPrefix(state.String(), "AGENT_INSTANCE_STATE_")
}

func agentInstanceOperationName(operation apiv1alpha1.AgentInstanceOperation) string {
	if operation == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED {
		return "NONE"
	}
	return strings.TrimPrefix(operation.String(), "AGENT_INSTANCE_OPERATION_")
}

func (c *Client) DeleteAgentInstance(ctx context.Context, id string) error {
	if err := execSQL(ctx, c.db, `
		DELETE FROM agent_instance WHERE id = $1
	`, uuid.MustParse(id)); err != nil {
		return fmt.Errorf("delete AgentInstance %s: %w", id, err)
	}
	return nil
}

type a2aContextRow struct {
	ID        uuid.UUID
	UserID    string
	CreatedAt time.Time
	ContextID uuid.UUID
}

type agentInstanceRow struct {
	ID                 uuid.UUID
	UserID             string
	RequestID          string
	PreparedRevision   *string
	State              string
	Labels             []byte
	Data               []byte
	Operation          string
	ContextID          uuid.UUID
	SourceCheckpointID *uuid.UUID
	HistoryID          uuid.UUID
}

func lockAgentInstance(ctx context.Context, db pgx.Tx, id uuid.UUID) (agentInstanceRow, error) {
	return queryOne(ctx, db, `
		SELECT id, user_id, request_id, prepared_revision, state, labels, data, operation, context_id,
		    source_checkpoint_id, history_id FROM agent_instance WHERE id = $1 FOR UPDATE
	`, pgx.RowToStructByName[agentInstanceRow], id)
}

func readAgentInstanceRequest(ctx context.Context, db dbExecutor, userID, requestID string) (agentInstanceRow, error) {
	return queryOne(ctx, db, `
		SELECT id, user_id, request_id, prepared_revision, state, labels, data, operation, context_id,
		    source_checkpoint_id, history_id FROM agent_instance
		WHERE user_id = $1 AND request_id = $2
	`, pgx.RowToStructByName[agentInstanceRow], userID, requestID)
}
