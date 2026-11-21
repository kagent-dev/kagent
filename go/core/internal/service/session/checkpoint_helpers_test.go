package session

import (
	"context"
	"errors"

	"github.com/google/uuid"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
)

func reserveCheckpoint(ctx context.Context, client *lifecycleTestStore, checkpoint *apiv1.Checkpoint, owner, request string) (*apiv1.Checkpoint, *database.SessionTaskSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancel()
	executor := uuid.New()
	cp, snapshot, err := client.ReserveSessionCheckpoint(ctx, checkpoint, owner, request, executor)
	err = errors.Join(err, client.ReleaseCheckpointOperation(context.WithoutCancel(ctx), checkpoint.Id, executor))
	return cp, snapshot, err
}

func finalizeCheckpoint(ctx context.Context, client *lifecycleTestStore, id, uid, uri, failure string) (*apiv1.Checkpoint, error) {
	ctx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancel()
	executor := uuid.New()
	op, err := client.ClaimCheckpointOperation(ctx, id, "alice", executor)
	if err != nil {
		return nil, err
	}
	return client.FinalizeClaimedSessionCheckpoint(ctx, op.Checkpoint, executor, uid, uri, failure)
}

func beginDeleteCheckpoint(ctx context.Context, client *lifecycleTestStore, id, owner string) (*database.SessionTaskSnapshot, string, error) {
	ctx, cancel := context.WithTimeout(ctx, database.RuntimeOperationTimeout)
	defer cancel()
	executor := uuid.New()
	op, err := client.BeginDeleteSessionCheckpoint(ctx, id, owner, executor)
	if errors.Is(err, database.ErrConflict) {
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
