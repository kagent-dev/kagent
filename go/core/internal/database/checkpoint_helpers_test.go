package database

import (
	"context"
	"errors"

	"github.com/google/uuid"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// Fixtures admit a bounded attempt and abandon it explicitly. Production callers
// retain this claim while performing the external copy.
func reserveCheckpoint(ctx context.Context, client *Client, checkpoint *apiv1.Checkpoint, owner, request string) (*apiv1.Checkpoint, *SessionTaskSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, RuntimeOperationTimeout)
	defer cancel()
	executor := uuid.New()
	result, snapshot, err := client.ReserveSessionCheckpoint(ctx, checkpoint, owner, request, executor)
	err = errors.Join(err, client.ReleaseCheckpointOperation(context.WithoutCancel(ctx), checkpoint.Id, executor))
	return result, snapshot, err
}

func finalizeCheckpoint(ctx context.Context, client *Client, id, uid, uri, failure string) (*apiv1.Checkpoint, error) {
	ctx, cancel := context.WithTimeout(ctx, RuntimeOperationTimeout)
	defer cancel()
	op, err := client.GetCheckpointOperation(ctx, id, "alice")
	if err != nil {
		return nil, err
	}
	executor := uuid.New()
	op, err = client.ClaimCheckpointOperation(ctx, id, op.Creator, executor)
	if err != nil {
		return nil, err
	}
	return client.FinalizeClaimedSessionCheckpoint(ctx, op.Checkpoint, executor, uid, uri, failure)
}

func deleteCheckpoint(ctx context.Context, client *Client, id, owner string) error {
	ctx, cancel := context.WithTimeout(ctx, RuntimeOperationTimeout)
	defer cancel()
	executor := uuid.New()
	_, err := client.ClaimCheckpointOperation(ctx, id, owner, executor)
	if err != nil {
		return err
	}
	return client.DeleteClaimedSessionCheckpoint(ctx, id, owner, executor)
}

func beginDeleteCheckpoint(ctx context.Context, client *Client, id, owner string) (*SessionTaskSnapshot, string, error) {
	ctx, cancel := context.WithTimeout(ctx, RuntimeOperationTimeout)
	defer cancel()
	executor := uuid.New()
	op, err := client.BeginDeleteSessionCheckpoint(ctx, id, owner, executor)
	if errors.Is(err, ErrConflict) {
		op, err = client.GetCheckpointOperation(ctx, id, owner)
	}
	if err != nil {
		return nil, "", err
	}
	if err = client.ReleaseCheckpointOperation(context.WithoutCancel(ctx), id, executor); err != nil {
		return nil, "", err
	}
	return op.Snapshot, op.TagUID, nil
}
