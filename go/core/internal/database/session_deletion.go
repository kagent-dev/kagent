package database

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

type sessionDeletionReason string

const (
	sessionDeletionUserRequested sessionDeletionReason = "user_requested"
	sessionDeletionIdleTimeout   sessionDeletionReason = "idle_timeout"
)

// beginSessionDeletion attaches the reason to ordinary lifecycle admission.
// The caller holds the session lock and has checked any reason-specific policy.
func beginSessionDeletion(ctx context.Context, tx pgx.Tx, row sessionRow, reason sessionDeletionReason) (*SessionOperation, error) {
	operation, err := beginSessionOperation(ctx, tx, row, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	if err != nil {
		return nil, err
	}
	if err := execSQL(ctx, tx, `UPDATE session SET deletion_reason = COALESCE(deletion_reason, $2) WHERE id = $1`, row.ID, string(reason)); err != nil {
		return nil, err
	}
	return operation, nil
}

// finishSessionDeletion releases expired creation receipts. Explicit deletion
// retains its tombstone so the original request cannot recreate compute.
func finishSessionDeletion(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return execSQL(ctx, tx, `
		DELETE FROM runtime_instance r USING session s
		WHERE r.id = $1 AND s.id = r.id AND s.deletion_reason = 'idle_timeout'
	`, id)
}
