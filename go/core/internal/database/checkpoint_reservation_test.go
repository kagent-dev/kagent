package database

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// Same delayed-COMMIT boundary as TestCheckpointReleaseWaitsForClaimCommit,
// applied to the initial reservation INSERT instead of an existing row UPDATE.
func TestCheckpointReleaseWaitsForReservationCommit(t *testing.T) {
	pool := setupTestDB(t)
	client := NewClient(pool, "public")
	seed, source := reserveRecoveryCheckpoint(t, client)
	_, err := finalizeCheckpoint(t.Context(), client, seed.Id, "seed", "s3://tags/seed", "")
	require.NoError(t, err)

	gate := &checkpointCommitGate{entered: make(chan struct{}), resume: make(chan struct{})}
	config := pool.Config().Copy()
	config.ConnConfig.Tracer = gate
	reservedPool, err := Connect(t.Context(), &PostgresConfig{Pool: config, Schema: "public"})
	require.NoError(t, err)
	t.Cleanup(reservedPool.Close)
	var once sync.Once
	resume := func() { once.Do(func() { close(gate.resume) }) }
	t.Cleanup(resume)
	reserving := NewClient(reservedPool, "public")
	ctx, cancel := context.WithTimeout(t.Context(), RuntimeOperationTimeout)
	defer cancel()
	executor, id, request := uuid.New(), uuid.NewString(), uuid.NewString()
	reserveDone := make(chan error, 1)
	go func() {
		_, _, err := reserving.ReserveSessionCheckpoint(ctx, &apiv1.Checkpoint{Id: id, SessionId: source.Id, HeadTaskId: seed.HeadTaskId}, "alice", request, executor)
		reserveDone <- err
	}()
	select {
	case <-gate.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	releaseDone := make(chan error, 1)
	go func() { releaseDone <- client.ReleaseCheckpointReservation(ctx, source.Id, id, executor) }()
	early := false
	select {
	case err := <-releaseDone:
		require.NoError(t, err)
		early = true
	case <-time.After(100 * time.Millisecond):
	}
	resume()
	require.NoError(t, <-reserveDone)
	if !early {
		require.NoError(t, <-releaseDone)
	}
	replacement := uuid.New()
	op, err := client.ClaimCheckpointOperation(ctx, id, "alice", replacement)
	require.False(t, early, "release passed an invisible INSERT instead of waiting for its commit")
	require.NoError(t, err, "a late reservation commit must not restore a live claim after release returned")
	require.NoError(t, client.ReleaseCheckpointReservation(ctx, source.Id, id, executor))
	_, err = client.ClaimCheckpointOperation(ctx, id, "alice", uuid.New())
	require.ErrorIs(t, err, ErrConflict, "an obsolete reservation release must not revoke the replacement")
	_, err = client.FinalizeClaimedSessionCheckpoint(ctx, op.Checkpoint, replacement, "retained", "s3://tags/retained", "")
	require.NoError(t, err)
	require.NoError(t, client.ReleaseCheckpointReservation(ctx, uuid.NewString(), uuid.NewString(), executor), "a missing source and reservation are harmless")
}
