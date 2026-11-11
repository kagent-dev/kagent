package database

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

func TestCheckpointCompletionRejectsExpiryDuringLockWait(t *testing.T) {
	pool := setupTestDB(t)
	client := NewClient(pool, "public")
	scenarios := []struct {
		name     string
		state    apiv1.CheckpointState
		complete func(context.Context, *Client, *CheckpointOperation, uuid.UUID) error
	}{
		{
			name: "creation", state: apiv1.CheckpointState_CHECKPOINT_STATE_CREATING,
			complete: func(ctx context.Context, client *Client, op *CheckpointOperation, executor uuid.UUID) error {
				_, err := client.FinalizeClaimedSessionCheckpoint(ctx, op.Checkpoint, executor, "tag-uid", "s3://tags/retained", "")
				return err
			},
		},
		{
			name: "deletion", state: apiv1.CheckpointState_CHECKPOINT_STATE_DELETING,
			complete: func(ctx context.Context, client *Client, op *CheckpointOperation, executor uuid.UUID) error {
				return client.DeleteClaimedSessionCheckpoint(ctx, op.Checkpoint.Id, op.Creator, executor)
			},
		},
		{
			name: "failed tag collection", state: apiv1.CheckpointState_CHECKPOINT_STATE_FAILED,
			complete: func(ctx context.Context, client *Client, op *CheckpointOperation, executor uuid.UUID) error {
				return client.ClearFailedCheckpointTag(ctx, op.Checkpoint.Id, op.Creator, op.TagUID, executor)
			},
		},
	}
	for _, scenario := range scenarios {
		t.Run("Given live "+scenario.name+", when completion waits past expiry on a locked row, assert that only a replacement can finish", func(t *testing.T) {
			checkpoint, _ := reserveRecoveryCheckpoint(t, client)
			if scenario.state != apiv1.CheckpointState_CHECKPOINT_STATE_CREATING {
				failure := ""
				if scenario.state == apiv1.CheckpointState_CHECKPOINT_STATE_FAILED {
					failure = "copy failed verification"
				}
				_, err := finalizeCheckpoint(t.Context(), client, checkpoint.Id, "tag-uid", "s3://tags/retained", failure)
				require.NoError(t, err)
			}
			claimCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			executor := uuid.New()
			var op *CheckpointOperation
			var err error
			if scenario.state == apiv1.CheckpointState_CHECKPOINT_STATE_DELETING {
				op, err = client.BeginDeleteSessionCheckpoint(claimCtx, checkpoint.Id, "alice", executor)
			} else {
				op, err = client.ClaimCheckpointOperation(claimCtx, checkpoint.Id, "alice", executor)
			}
			require.NoError(t, err)
			require.Equal(t, scenario.state, op.Checkpoint.State)

			blocker, err := pool.Begin(t.Context())
			require.NoError(t, err)
			defer blocker.Rollback(t.Context()) //nolint:errcheck // The successful path commits it first.
			_, err = blocker.Exec(t.Context(), `SELECT id FROM session_checkpoint WHERE id = $1 FOR UPDATE`, checkpoint.Id)
			require.NoError(t, err)
			var blockingPID int32
			require.NoError(t, blocker.QueryRow(t.Context(), `SELECT pg_backend_pid()`).Scan(&blockingPID))
			finishCtx, stop := context.WithTimeout(t.Context(), 5*time.Second)
			defer stop()
			finished := make(chan error, 1)
			go func() { finished <- scenario.complete(finishCtx, client, op, executor) }()
			require.Eventually(t, func() bool {
				var blocked bool
				require.NoError(t, pool.QueryRow(t.Context(), `
					SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))
				`, blockingPID).Scan(&blocked))
				return blocked
			}, time.Second, 5*time.Millisecond, "completion must reach the real row-lock wait before expiry")
			// The blocker never changes the row. PostgreSQL must check the actual
			// lease clock after that lock is released, without relying on a new tuple.
			require.Eventually(t, func() bool {
				var expired bool
				require.NoError(t, pool.QueryRow(t.Context(), `
					SELECT executor_expires_at <= clock_timestamp() FROM session_checkpoint WHERE id = $1
				`, checkpoint.Id).Scan(&expired))
				return expired
			}, 3*time.Second, 10*time.Millisecond, "the database claim must expire during the lock wait")
			require.NoError(t, blocker.Commit(t.Context()))
			require.ErrorIs(t, <-finished, ErrConflict)
			retained, err := client.GetCheckpointOperation(t.Context(), checkpoint.Id, "alice")
			require.NoError(t, err)
			require.True(t, proto.Equal(op.Checkpoint, retained.Checkpoint), "an expired completion must preserve the durable phase")
			require.Equal(t, op.TagUID, retained.TagUID)
			require.Equal(t, op.Snapshot, retained.Snapshot)

			replacementCtx, release := context.WithTimeout(t.Context(), RuntimeOperationTimeout)
			defer release()
			replacement := uuid.New()
			next, err := client.ClaimCheckpointOperation(replacementCtx, checkpoint.Id, "alice", replacement)
			require.NoError(t, err, "recovery must be able to take over the expired operation immediately")
			require.NoError(t, scenario.complete(replacementCtx, client, next, replacement), "the same completion must succeed for a live replacement")
		})
	}
}
