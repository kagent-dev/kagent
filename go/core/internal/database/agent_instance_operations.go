package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// InstanceOperation identifies one admitted lifecycle operation by ID and Kind.
// Instance is its original input; Result and Failure describe its retained,
// immutable outcome.
// TODO: add bounded pruning of settled outcomes after at least 24 hours;
// pending or uncertain operations must never expire.
type InstanceOperation struct {
	ID                 uuid.UUID
	Kind               apiv1alpha1.AgentInstanceOperation
	SourceCheckpointID *uuid.UUID // Pinned fork intent; nil for ordinary creation.
	Instance           *apiv1alpha1.AgentInstance
	ExecutorID         uuid.UUID // Nonzero means runtime work may have been issued; never retry it.
	Result             *apiv1alpha1.AgentInstance
	Failure            string
}

// BeginAgentInstanceOperation admits or joins one lifecycle operation under the
// instance lock. Joined callers retain the same identity, but must win Claim before
// touching the runtime. Delete may supersede unclaimed work; claimed work blocks
// conflicting operations indefinitely until its executor records completion.
// Callers authorize access. Missing instances return ErrNotFound, except that a
// completed Delete can return its retained result to an already authorized caller.
func (c *Client) BeginAgentInstanceOperation(ctx context.Context, instanceID string, kind apiv1alpha1.AgentInstanceOperation) (*InstanceOperation, error) {
	var operation *InstanceOperation
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockAgentInstance(ctx, tx, instanceID)
		if errors.Is(err, pgx.ErrNoRows) && kind == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE {
			operation, err = readCompletedInstanceOperation(ctx, tx, instanceID, kind)
			return notFoundOr(err)
		}
		if err != nil {
			return notFoundOr(err)
		}
		instance, err := toAgentInstance(row)
		if err != nil {
			return err
		}
		// A repeated creation request observes creation's outcome, even after an
		// opposite lifecycle operation. It cannot adopt or recreate the runtime.
		if kind == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE && instance.State != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING {
			operation, err = readCompletedInstanceOperation(ctx, tx, instanceID, kind)
			return notFoundOr(err)
		}
		pending, err := queryOne(ctx, tx, `
			SELECT id FROM agent_instance_operation WHERE instance_id = $1 AND completed_at IS NULL
		`, pgx.RowTo[uuid.UUID], instanceID)
		if err == nil {
			operation, err = readInstanceOperation(ctx, tx, pending)
			if err != nil {
				return err
			}
			if operation.Kind == kind {
				return nil
			}
			// Only Delete may replace another operation, before runtime work
			// could have been issued. A claimed operation must retain its pins.
			canSupersede := kind == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE && operation.ExecutorID == uuid.Nil
			if !canSupersede {
				return fmt.Errorf("AgentInstance has an unfinished lifecycle operation: %w", ErrConflict)
			}
			if err := execSQL(ctx, tx, `
				UPDATE agent_instance_operation SET failure = 'superseded by deletion', completed_at = NOW()
				WHERE id = $1
			`, pending); err != nil {
				return err
			}
			instance.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var expected, target apiv1alpha1.AgentInstanceState
		switch kind {
		case apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE:
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
		// Resume/Suspend at their target state return the retained outcome;
		// the current state alone is not proof that this operation completed.
		alreadyAtTarget := target != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_UNSPECIFIED && instance.State == target
		if instance.Operation == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED && alreadyAtTarget {
			operation, err = readCompletedInstanceOperation(ctx, tx, instanceID, kind)
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("no retained lifecycle outcome for %s: %w", kind, ErrFailedPrecondition)
			}
			return err
		}
		expectedOperation := apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED
		if kind == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE {
			expectedOperation = kind
		}
		// A reserved creation carries CREATE before lifecycle admission. With
		// no claimed operation remaining, Delete may replace that marker too.
		deletingUnissuedCreation := kind == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE && instance.Operation == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE
		canStart := instance.State == expected && (instance.Operation == expectedOperation || deletingUnissuedCreation)
		if !canStart {
			return fmt.Errorf("AgentInstance cannot start %s from %s with operation %s: %w", kind, instance.State, instance.Operation, ErrConflict)
		}
		instance.Operation = kind
		instance.UpdatedAt = timestamppb.Now()
		input, err := marshalAgentInstance(instance)
		if err != nil {
			return err
		}
		// Share admission's instance lock and the existing checkpoint/task barriers.
		tag, err := tx.Exec(ctx, `
			UPDATE agent_instance SET operation = $2, data = $3 WHERE id = $1
			AND NOT EXISTS (SELECT 1 FROM agent_instance_checkpoint WHERE source_instance_id = $1 AND state = 'CREATING')
			AND ($2::text <> 'AGENT_INSTANCE_OPERATION_SUSPEND' OR NOT EXISTS (
			    SELECT 1 FROM agent_instance_task WHERE history_id = $4
			    AND state NOT IN ('TASK_STATE_COMPLETED', 'TASK_STATE_CANCELED', 'TASK_STATE_FAILED',
			        'TASK_STATE_REJECTED', 'TASK_STATE_INPUT_REQUIRED', 'TASK_STATE_AUTH_REQUIRED')))
		`, instanceID, kind.String(), input, row.HistoryID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("AgentInstance has an active task or checkpoint: %w", ErrConflict)
		}
		operation = &InstanceOperation{ID: uuid.New(), Kind: kind, Instance: instance, SourceCheckpointID: row.SourceCheckpointID}
		return execSQL(ctx, tx, `
			INSERT INTO agent_instance_operation (id, instance_id, kind, input, source_checkpoint_id) VALUES ($1, $2, $3, $4, $5)
		`, operation.ID, instanceID, kind.String(), input, row.SourceCheckpointID)
	})
	if err != nil {
		return nil, fmt.Errorf("begin AgentInstance operation: %w", err)
	}
	return operation, nil
}

// ClaimAgentInstanceOperation grants exactly one executor permission to issue the
// admitted operation. False means the caller must only observe this operation's
// outcome. The claim never expires: loss of a caller or RPC response cannot prove
// that its runtime effects have stopped. Unclaimed operations are safe to retry.
func (c *Client) ClaimAgentInstanceOperation(ctx context.Context, id, executorID uuid.UUID) (bool, error) {
	if executorID == uuid.Nil {
		return false, fmt.Errorf("lifecycle executor ID is required")
	}
	claimed := false
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		operation, err := readInstanceOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if operation.Result != nil || operation.Failure != "" {
			return nil
		}
		if _, err := lockAgentInstance(ctx, tx, operation.Instance.Id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				current, readErr := readInstanceOperation(ctx, tx, id)
				if readErr == nil && (current.Result != nil || current.Failure != "") {
					return nil
				}
			}
			return notFoundOr(err)
		}
		tag, err := tx.Exec(ctx, `
			UPDATE agent_instance_operation SET executor_id = $2
			WHERE id = $1 AND executor_id IS NULL AND completed_at IS NULL
		`, id, executorID)
		claimed = tag.RowsAffected() == 1
		return err
	})
	return claimed, err
}

// FinishAgentInstanceOperation atomically retains the exact result and changes or
// deletes the instance. Only the claiming executor may finish successful work.
// An unclaimed operation may instead fail preparation, releasing its lifecycle
// marker; failures after Claim must remain pending. Stale identities return
// ErrConflict, including a stale completion after a newer operation starts.
func (c *Client) FinishAgentInstanceOperation(ctx context.Context, id, executorID uuid.UUID, authority, failure string) (*apiv1alpha1.AgentInstance, error) {
	var result *apiv1alpha1.AgentInstance
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		operation, err := readInstanceOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		row, err := lockAgentInstance(ctx, tx, operation.Instance.Id)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("lifecycle instance no longer exists: %w", ErrConflict)
		}
		if err != nil {
			return err
		}
		operation, err = readInstanceOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		completed := operation.Result != nil || operation.Failure != ""
		// Preparation may fail only before Claim. After Claim, only a known
		// successful outcome may settle the operation; errors keep it pending.
		failedPreparation := executorID == uuid.Nil && failure != ""
		successfulExecution := executorID != uuid.Nil && failure == ""
		if completed || operation.ExecutorID != executorID || (!failedPreparation && !successfulExecution) {
			return fmt.Errorf("lifecycle operation no longer belongs to this executor: %w", ErrConflict)
		}
		result, err = toAgentInstance(row)
		if err != nil {
			return err
		}
		kind := operation.Kind
		if result.Operation != kind || result.State != operation.Instance.State {
			return fmt.Errorf("lifecycle operation changed: %w", ErrConflict)
		}
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
				result.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_DELETED
				result.PreparedRevision, result.A2AAuthority = "", ""
			}
		} else if kind == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE {
			result.Operation = kind // Creation can retry preparation using its pinned inputs.
		}
		data, err := marshalAgentInstance(result)
		if err != nil {
			return err
		}
		if successfulExecution && kind == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE {
			err = execSQL(ctx, tx, `DELETE FROM agent_instance WHERE id = $1`, row.ID)
		} else {
			err = execSQL(ctx, tx, `UPDATE agent_instance SET state = $2, operation = $3, data = $4 WHERE id = $1`, row.ID, result.State.String(), result.Operation.String(), data)
		}
		if err != nil {
			return err
		}
		if failedPreparation {
			data = nil
		}
		return execSQL(ctx, tx, `
			UPDATE agent_instance_operation SET result = $2, failure = $3, completed_at = NOW() WHERE id = $1
		`, id, data, failure)
	})
	if err != nil {
		return nil, fmt.Errorf("finish AgentInstance operation: %w", err)
	}
	return result, nil
}

// GetAgentInstanceOperation observes exactly one admitted operation, including
// after instance deletion. It never reads a newer operation's result or issues
// runtime work. Callers must already have authorized the instance at admission.
// A missing retained identity returns ErrFailedPrecondition rather than restarting.
func (c *Client) GetAgentInstanceOperation(ctx context.Context, id uuid.UUID) (*InstanceOperation, error) {
	operation, err := readInstanceOperation(ctx, c.db, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("lifecycle outcome is no longer retained: %w", ErrFailedPrecondition)
	}
	return operation, err
}

// readInstanceOperation decodes the admitted input and optional retained outcome.
// The caller owns authorization and must hold the instance lock before mutation.
func readInstanceOperation(ctx context.Context, db dbExecutor, id uuid.UUID) (*InstanceOperation, error) {
	type operationRow struct {
		InstanceID         uuid.UUID
		SourceCheckpointID *uuid.UUID
		Kind               string
		Input              []byte
		ExecutorID         *uuid.UUID
		Result             []byte
		Failure            string
	}
	row, err := queryOne(ctx, db, `SELECT instance_id, source_checkpoint_id, kind, input, executor_id, result, failure FROM agent_instance_operation WHERE id = $1`, pgx.RowToStructByName[operationRow], id)
	if err != nil {
		return nil, err
	}
	operation := &InstanceOperation{ID: id, SourceCheckpointID: row.SourceCheckpointID, Instance: &apiv1alpha1.AgentInstance{}, Failure: row.Failure}
	if err := proto.Unmarshal(row.Input, operation.Instance); err != nil {
		return nil, fmt.Errorf("decode lifecycle input: %w", err)
	}
	value, ok := apiv1alpha1.AgentInstanceOperation_value[row.Kind]
	if !ok {
		return nil, fmt.Errorf("invalid stored lifecycle operation %q", row.Kind)
	}
	operation.Instance.Id = row.InstanceID.String()
	operation.Kind = apiv1alpha1.AgentInstanceOperation(value)
	if row.ExecutorID != nil {
		operation.ExecutorID = *row.ExecutorID
	}
	if row.Result != nil {
		operation.Result = &apiv1alpha1.AgentInstance{}
		if err := proto.Unmarshal(row.Result, operation.Result); err != nil {
			return nil, fmt.Errorf("decode lifecycle result: %w", err)
		}
	}
	return operation, nil
}

// readCompletedInstanceOperation returns the latest successful outcome for a
// requested lifecycle kind. It does not substitute the instance's current state.
func readCompletedInstanceOperation(ctx context.Context, db dbExecutor, instanceID string, kind apiv1alpha1.AgentInstanceOperation) (*InstanceOperation, error) {
	id, err := queryOne(ctx, db, `
		SELECT id FROM agent_instance_operation WHERE instance_id = $1 AND kind = $2 AND result IS NOT NULL
		ORDER BY created_at DESC, id DESC LIMIT 1
	`, pgx.RowTo[uuid.UUID], instanceID, kind.String())
	if err != nil {
		return nil, err
	}
	return readInstanceOperation(ctx, db, id)
}

// hasPendingInstanceOperation reports whether direct lifecycle writes must defer
// to an admitted operation. Mutating callers must hold the instance row lock.
func hasPendingInstanceOperation(ctx context.Context, db dbExecutor, instanceID uuid.UUID) (bool, error) {
	return queryOne(ctx, db, `SELECT EXISTS (SELECT 1 FROM agent_instance_operation WHERE instance_id = $1 AND completed_at IS NULL)`, pgx.RowTo[bool], instanceID)
}
