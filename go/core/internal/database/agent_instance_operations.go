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

// InstanceOperation is the current lifecycle generation on an instance. Once
// Instance.Operation is unspecified, callers may return the current instance.
// ExecutorID marks possibly issued runtime work and never expires. The value is
// an observation, not permission to execute: callers must win Claim first.
type InstanceOperation struct {
	ID                 uuid.UUID
	Instance           *apiv1alpha1.AgentInstance
	SourceCheckpointID *uuid.UUID
	ExecutorID         uuid.UUID
}

// BeginAgentInstanceOperation admits or joins current lifecycle work under the
// instance lock. Creation retries and already-at-target requests return current
// state. Delete alone may supersede unclaimed work; uncertain work blocks it.
// Callers authorize access. Deleted instances return ErrNotFound, except Delete
// can observe the tombstone for a caller already authorized before deletion.
func (c *Client) BeginAgentInstanceOperation(ctx context.Context, instanceID string, kind apiv1alpha1.AgentInstanceOperation) (*InstanceOperation, error) {
	var operation *InstanceOperation
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockAgentInstance(ctx, tx, instanceID)
		if err != nil {
			return notFoundOr(err)
		}
		operation, err = toInstanceOperation(row)
		if err != nil {
			return err
		}
		instance := operation.Instance
		if instance.State == apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_DELETED {
			if kind == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE {
				return nil
			}
			return ErrNotFound
		}
		pending := row.OperationID != nil && instance.Operation != apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED
		if pending {
			if instance.Operation == kind {
				return nil
			}
			canSupersede := kind == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE && row.ExecutorID == nil
			if !canSupersede {
				return fmt.Errorf("AgentInstance has an unfinished lifecycle operation: %w", ErrConflict)
			}
			instance.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED
		}
		var expected, target apiv1alpha1.AgentInstanceState
		switch kind {
		case apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE:
			// Request deduplication reserves identity, not a historical READY response.
			if instance.State != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING && instance.Operation == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED {
				return nil
			}
			expected = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING
		case apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_RESUME:
			expected, target = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY
		case apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND:
			expected, target = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED
		case apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE:
			expected = instance.State
		default:
			return fmt.Errorf("invalid lifecycle operation %s", kind)
		}
		alreadyAtTarget := target != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_UNSPECIFIED && instance.State == target
		if alreadyAtTarget && instance.Operation == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED {
			return nil
		}
		expectedOperation := apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED
		if kind == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE {
			expectedOperation = kind
		}
		deletingUnissuedCreation := kind == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE && instance.Operation == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE
		canStart := instance.State == expected && (instance.Operation == expectedOperation || deletingUnissuedCreation)
		if !canStart {
			return fmt.Errorf("AgentInstance cannot start %s from %s with operation %s: %w", kind, instance.State, instance.Operation, ErrConflict)
		}
		instance.Operation, instance.UpdatedAt = kind, timestamppb.Now()
		data, err := marshalAgentInstance(instance)
		if err != nil {
			return err
		}
		operation.ID, operation.ExecutorID = uuid.New(), uuid.Nil
		tag, err := tx.Exec(ctx, `
			UPDATE agent_instance SET operation = $2, data = $3, operation_id = $5, executor_id = NULL WHERE id = $1
			AND NOT EXISTS (SELECT 1 FROM agent_instance_checkpoint WHERE source_instance_id = $1 AND state = 'CREATING')
			AND NOT EXISTS (
			    SELECT 1 FROM agent_instance_task WHERE history_id = $4
			    AND turn_phase IS NOT NULL AND turn_phase <> 'SETTLED')
			AND ($2::text <> 'AGENT_INSTANCE_OPERATION_SUSPEND' OR NOT EXISTS (
			    SELECT 1 FROM agent_instance_task WHERE history_id = $4
			    AND state NOT IN ('TASK_STATE_COMPLETED', 'TASK_STATE_CANCELED', 'TASK_STATE_FAILED',
			        'TASK_STATE_REJECTED', 'TASK_STATE_INPUT_REQUIRED', 'TASK_STATE_AUTH_REQUIRED')))
		`, instanceID, kind.String(), data, row.HistoryID, operation.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("AgentInstance has an active task or checkpoint: %w", ErrConflict)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("begin AgentInstance operation: %w", err)
	}
	return operation, nil
}

// ClaimAgentInstanceOperation grants one executor permission to issue this
// generation's runtime work. False means observe only. A stale generation cannot
// claim, even when a later operation has the same kind and state. Claims never
// expire: losing an RPC response does not prove that its effects have stopped.
func (c *Client) ClaimAgentInstanceOperation(ctx context.Context, instanceID string, id, executorID uuid.UUID) (bool, error) {
	if id == uuid.Nil || executorID == uuid.Nil {
		return false, fmt.Errorf("lifecycle generation and executor IDs are required")
	}
	tag, err := c.db.Exec(ctx, `
		UPDATE agent_instance i SET executor_id = $3
		WHERE i.id = $1 AND operation_id = $2 AND executor_id IS NULL
		  AND operation <> 'AGENT_INSTANCE_OPERATION_UNSPECIFIED'
		  AND state <> 'AGENT_INSTANCE_STATE_DELETED'
		  AND NOT EXISTS (
		      SELECT 1 FROM agent_instance_task t WHERE t.history_id = i.history_id
		      AND t.turn_phase IS NOT NULL AND t.turn_phase <> 'SETTLED')
	`, instanceID, id, executorID)
	return tag.RowsAffected() == 1, err
}

// FinishAgentInstanceOperation publishes known success only for the claiming
// executor. A nonempty failure releases only unclaimed preparation and invalidates
// its generation. Uncertain issued work must remain pending. Stale completion or
// release returns ErrConflict. Deletion retains a tombstone and revokes shares;
// PostgreSQL releases its resource pins atomically with the state change.
func (c *Client) FinishAgentInstanceOperation(ctx context.Context, instanceID string, id, executorID uuid.UUID, authority, failure string) (*apiv1alpha1.AgentInstance, error) {
	var result *apiv1alpha1.AgentInstance
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockAgentInstance(ctx, tx, instanceID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		operation, err := toInstanceOperation(row)
		if err != nil {
			return err
		}
		result = operation.Instance
		failedPreparation := executorID == uuid.Nil && failure != ""
		successfulExecution := executorID != uuid.Nil && failure == ""
		if id == uuid.Nil || operation.ID != id || result.Operation == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED || operation.ExecutorID != executorID || (!failedPreparation && !successfulExecution) {
			return fmt.Errorf("lifecycle operation no longer belongs to this executor: %w", ErrConflict)
		}
		kind := result.Operation
		result.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED
		result.UpdatedAt = timestamppb.Now()
		if successfulExecution {
			switch kind {
			case apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE:
				if authority == "" {
					return fmt.Errorf("created AgentInstance requires runtime authority")
				}
				result.State, result.A2AAuthority, result.Failure = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY, authority, nil
			case apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_RESUME:
				result.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY
			case apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND:
				result.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED
			case apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE:
				return tombstoneAgentInstance(ctx, tx, result, row.OperationID)
			}
		} else if kind == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE {
			result.Operation = kind // A new generation may retry preparation with the same pinned inputs.
		}
		data, err := marshalAgentInstance(result)
		if err != nil {
			return err
		}
		return execSQL(ctx, tx, `
			UPDATE agent_instance SET state = $2, operation = $3, data = $4,
			    operation_id = CASE WHEN $5 THEN NULL ELSE operation_id END,
			    executor_id = NULL
			WHERE id = $1
		`, instanceID, result.State.String(), result.Operation.String(), data, failedPreparation)
	})
	if err != nil {
		return nil, fmt.Errorf("finish AgentInstance operation: %w", err)
	}
	return result, nil
}

// GetAgentInstanceOperation observes this generation only while it remains current,
// including its deletion tombstone. A superseded or missing generation returns
// ErrConflict. Callers must have authorized the instance at admission. It never
// returns historical results or grants permission to issue runtime work.
func (c *Client) GetAgentInstanceOperation(ctx context.Context, instanceID string, id uuid.UUID) (*InstanceOperation, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT id, user_id, prepared_revision, state, data, operation, context_id,
		    source_checkpoint_id, history_id, operation_id, executor_id FROM agent_instance
		WHERE id = $1 AND operation_id = $2
	`, pgx.RowToStructByName[agentInstanceRow], instanceID, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("lifecycle generation was superseded: %w", ErrConflict)
	}
	if err != nil {
		return nil, err
	}
	return toInstanceOperation(row)
}

// toInstanceOperation decodes current instance state and its private execution
// identity. A completed generation observes current fields, including renames.
func toInstanceOperation(row agentInstanceRow) (*InstanceOperation, error) {
	instance, err := toAgentInstance(row)
	if err != nil {
		return nil, err
	}
	operation := &InstanceOperation{Instance: instance, SourceCheckpointID: row.SourceCheckpointID}
	if row.OperationID != nil {
		operation.ID = *row.OperationID
	}
	if row.ExecutorID != nil {
		operation.ExecutorID = *row.ExecutorID
	}
	return operation, nil
}
