package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

const (
	runtimeKindAgent   = "agent"
	runtimeKindSandbox = "sandbox"
)

// RuntimeOperation carries a typed resource and its current execution authority.
// Completion retains the generation for observers. Executor IDs record that
// work may have been issued even after an individual attempt ends.
type RuntimeOperation[T any] struct {
	ID         uuid.UUID
	ExecutorID uuid.UUID
	Instance   T
}

// runtimeInstanceRow is the common relational part of both resource projections.
// Resource-specific rows embed it and add only their own columns.
type runtimeInstanceRow struct {
	ID               uuid.UUID
	UserID           string
	PreparedRevision *string
	State            string
	Operation        string
	OperationID      *uuid.UUID
	ExecutorID       *uuid.UUID
}

func (r runtimeInstanceRow) lifecycle() (apiv1alpha1.RuntimeState, apiv1alpha1.RuntimeOperation, error) {
	state, ok := apiv1alpha1.RuntimeState_value[r.State]
	if !ok || state == int32(apiv1alpha1.RuntimeState_RUNTIME_STATE_UNSPECIFIED) {
		return 0, 0, fmt.Errorf("decode runtime %s state %q", r.ID, r.State)
	}
	operation, ok := apiv1alpha1.RuntimeOperation_value[r.Operation]
	if !ok {
		return 0, 0, fmt.Errorf("decode runtime %s operation %q", r.ID, r.Operation)
	}
	return apiv1alpha1.RuntimeState(state), apiv1alpha1.RuntimeOperation(operation), nil
}

func runtimeOperationFor[T any](row runtimeInstanceRow, instance T) RuntimeOperation[T] {
	result := RuntimeOperation[T]{Instance: instance}
	if row.OperationID != nil {
		result.ID = *row.OperationID
	}
	if row.ExecutorID != nil {
		result.ExecutorID = *row.ExecutorID
	}
	return result
}

// runtimeOperationNeeded computes admission using only common lifecycle state.
// A caller that receives true must check its own admission rules and persist the
// generation in the same transaction, while holding the runtime row lock.
func runtimeOperationNeeded(row runtimeInstanceRow, requested apiv1alpha1.RuntimeOperation) (bool, error) {
	state, operation, err := row.lifecycle()
	if err != nil {
		return false, err
	}
	if requested < apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE || requested > apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE {
		return false, fmt.Errorf("invalid runtime operation %s: %w", requested, ErrFailedPrecondition)
	}
	if state == apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED {
		if requested == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE {
			return false, nil
		}
		return false, ErrNotFound
	}
	if row.OperationID != nil && operation != apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		if operation == requested {
			return false, nil
		}
		if requested != apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE || row.ExecutorID != nil {
			return false, ErrConflict
		}
		operation = apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE
	}
	var expected, target apiv1alpha1.RuntimeState
	expectedOperation := apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE
	switch requested {
	case apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE:
		if state != apiv1alpha1.RuntimeState_RUNTIME_STATE_CREATING && operation == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE {
			return false, nil
		}
		expected, expectedOperation = apiv1alpha1.RuntimeState_RUNTIME_STATE_CREATING, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE
	case apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME:
		expected, target = apiv1alpha1.RuntimeState_RUNTIME_STATE_SUSPENDED, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY
	case apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND:
		expected, target = apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, apiv1alpha1.RuntimeState_RUNTIME_STATE_SUSPENDED
	case apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE:
		expected = state
	}
	if target != apiv1alpha1.RuntimeState_RUNTIME_STATE_UNSPECIFIED && state == target && operation == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		return false, nil
	}
	deletingUnissuedCreation := requested == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE && operation == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE
	if state != expected || (operation != expectedOperation && !deletingUnissuedCreation) {
		return false, ErrConflict
	}
	return true, nil
}

func beginRuntimeOperation(ctx context.Context, tx pgx.Tx, row runtimeInstanceRow, kind string, operation apiv1alpha1.RuntimeOperation) (runtimeInstanceRow, error) {
	row.Operation, row.OperationID, row.ExecutorID = operation.String(), new(uuid.New()), nil
	err := saveRuntimeLifecycle(ctx, tx, row, kind)
	return row, err
}

// RuntimeOperationTimeout bounds one inline attempt and its execution claim.
// A client can retry an abandoned attempt after this bound without a sweeper.
const RuntimeOperationTimeout = 2 * time.Minute

func (c *Client) claimRuntimeOperation(ctx context.Context, id, kind string, operationID, executorID uuid.UUID) (bool, error) {
	if operationID == uuid.Nil || executorID == uuid.Nil {
		return false, ErrFailedPrecondition
	}
	lifetime := RuntimeOperationTimeout
	if deadline, ok := ctx.Deadline(); ok {
		lifetime = min(lifetime, time.Until(deadline))
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	tag, err := c.db.Exec(ctx, `
		UPDATE runtime_instance SET executor_id = $4,
		    executor_expires_at = clock_timestamp() + $5 * interval '1 millisecond'
		WHERE id = $1 AND kind = $2 AND operation_id = $3
		    AND (executor_id IS NULL OR executor_expires_at <= clock_timestamp())
		    AND operation <> 'RUNTIME_OPERATION_NONE' AND state <> 'RUNTIME_STATE_DELETED'
	`, id, kind, operationID, executorID, lifetime.Milliseconds()+1)
	return tag.RowsAffected() == 1, err
}

// ReleaseRuntimeOperation ends an attempt, not the durable operation. Keeping
// executor_id prevents uncertain issued work from being discarded as preparation.
// A stale attempt cannot release a retry's claim or a newer operation.
func (c *Client) ReleaseRuntimeOperation(ctx context.Context, id string, operationID, executorID uuid.UUID) error {
	_, err := c.db.Exec(ctx, `
		UPDATE runtime_instance SET executor_expires_at = clock_timestamp()
		WHERE id = $1 AND operation_id = $2 AND executor_id = $3
		    AND operation <> 'RUNTIME_OPERATION_NONE'
	`, id, operationID, executorID)
	return err
}

// completeRuntimeOperation accepts either claimed success or unclaimed preparation
// failure. No failure, timeout, or disconnect can release possibly issued work.
func completeRuntimeOperation(row runtimeInstanceRow, operationID, executorID uuid.UUID, failure string) (runtimeInstanceRow, error) {
	_, operation, err := row.lifecycle()
	if err != nil {
		return row, err
	}
	if operationID == uuid.Nil || row.OperationID == nil || *row.OperationID != operationID || operation == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		return row, ErrConflict
	}
	failedPreparation := executorID == uuid.Nil && failure != ""
	if failedPreparation {
		if row.ExecutorID != nil {
			return row, ErrConflict
		}
		row.OperationID = nil
		if operation != apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE {
			row.Operation = apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE.String()
		}
	} else {
		if executorID == uuid.Nil || row.ExecutorID == nil || *row.ExecutorID != executorID || failure != "" {
			return row, ErrConflict
		}
		switch operation {
		case apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME:
			row.State = apiv1alpha1.RuntimeState_RUNTIME_STATE_READY.String()
		case apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND:
			row.State = apiv1alpha1.RuntimeState_RUNTIME_STATE_SUSPENDED.String()
		case apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE:
			row.State, row.PreparedRevision = apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED.String(), nil
		}
		row.Operation = apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE.String()
	}
	row.ExecutorID = nil
	return row, nil
}

// saveRuntimeLifecycle updates only common lifecycle fields. Extension writes
// and resource payloads are written under the same row lock and transaction,
// including deletion cleanup.
func saveRuntimeLifecycle(ctx context.Context, tx pgx.Tx, row runtimeInstanceRow, kind string) error {
	tag, err := tx.Exec(ctx, `
		UPDATE runtime_instance SET state = $3, operation = $4, prepared_revision = $5,
		    operation_id = $6, executor_id = $7,
		    executor_expires_at = CASE WHEN executor_id IS DISTINCT FROM $7 THEN NULL ELSE executor_expires_at END
		WHERE id = $1 AND kind = $2
	`, row.ID, kind, row.State, row.Operation, row.PreparedRevision, row.OperationID, row.ExecutorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}
