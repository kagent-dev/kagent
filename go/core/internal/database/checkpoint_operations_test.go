package database

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

type checkpointCommitGate struct {
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (g *checkpointCommitGate) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.EqualFold(data.SQL, "commit") {
		g.once.Do(func() {
			close(g.entered)
			select {
			case <-g.resume:
			case <-ctx.Done():
			}
		})
	}
	return ctx
}

func (*checkpointCommitGate) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestCheckpointReleaseWaitsForClaimCommit(t *testing.T) {
	pool := setupTestDB(t)
	checkpoint, _ := reserveRecoveryCheckpoint(t, NewClient(pool, "public"))
	gate := &checkpointCommitGate{entered: make(chan struct{}), resume: make(chan struct{})}
	config := pool.Config().Copy()
	config.ConnConfig.Tracer = gate
	claimedPool, err := Connect(t.Context(), &PostgresConfig{Pool: config, Schema: "public"})
	require.NoError(t, err)
	t.Cleanup(claimedPool.Close)
	var unblock sync.Once
	resume := func() { unblock.Do(func() { close(gate.resume) }) }
	t.Cleanup(resume)
	client := NewClient(claimedPool, "public")
	ctx, cancel := context.WithTimeout(t.Context(), RuntimeOperationTimeout)
	defer cancel()
	executor := uuid.New()
	claimDone := make(chan error, 1)
	go func() {
		_, err := client.ClaimCheckpointOperation(ctx, checkpoint.Id, "alice", executor)
		claimDone <- err
	}()
	select {
	case <-gate.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	releaseDone := make(chan error, 1)
	go func() { releaseDone <- client.ReleaseCheckpointOperation(ctx, checkpoint.Id, executor) }()
	select {
	case err := <-releaseDone:
		t.Fatalf("release passed an uncommitted claim instead of waiting: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	resume()
	require.NoError(t, <-claimDone)
	require.NoError(t, <-releaseDone)
	_, err = client.ClaimCheckpointOperation(ctx, checkpoint.Id, "alice", uuid.New())
	require.NoError(t, err, "release must retire the claim after its delayed commit")
}

func reserveRecoveryCheckpoint(t *testing.T, client *Client) (*apiv1.Checkpoint, *apiv1.Session) {
	t.Helper()
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	_, err = markSessionReady(ctx, client, session.Id, "runtime")
	require.NoError(t, err)
	task := newSessionTask(uuid.NewString(), uuid.NewString())
	task.ContextID = session.ContextId
	task.Status.State = a2a.TaskStateCompleted
	require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task,
		&SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/source", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}))
	checkpoint, _, err := reserveCheckpoint(ctx, client,
		&apiv1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID)}, "alice", uuid.NewString())
	require.NoError(t, err)
	return checkpoint, session
}

func TestCheckpointClaimsFenceCompletionAndDeletion(t *testing.T) {
	pool := setupTestDB(t)
	config := pool.Config().Copy()
	config.ConnConfig.Tracer = &checkpointClaimTransit{}
	claimPool, err := Connect(t.Context(), &PostgresConfig{Pool: config, Schema: "public"})
	require.NoError(t, err)
	t.Cleanup(claimPool.Close)
	client := NewClient(claimPool, "public")
	checkpoint, _ := reserveRecoveryCheckpoint(t, client)
	first := uuid.New()
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	_, err = client.ClaimCheckpointOperation(ctx, checkpoint.Id, "alice", first)
	require.NoError(t, err)
	nextCtx, stop := context.WithTimeout(t.Context(), RuntimeOperationTimeout)
	defer stop()
	_, err = client.ClaimCheckpointOperation(nextCtx, checkpoint.Id, "alice", uuid.New())
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.ClaimCheckpointOperation(nextCtx, checkpoint.Id, "mallory", uuid.New())
	require.ErrorIs(t, err, ErrConflict)
	page, err := client.ListPendingCheckpoints(t.Context(), "", 100)
	require.NoError(t, err)
	require.Empty(t, page, "live claims are not sweep work")
	<-ctx.Done()
	second := uuid.New()
	// A client deadline does not include transit before PostgreSQL starts its
	// lease clock. Observe actual takeover eligibility instead of guessing it.
	var op *CheckpointOperation
	require.Eventually(t, func() bool {
		op, err = client.ClaimCheckpointOperation(nextCtx, checkpoint.Id, "alice", second)
		return !errors.Is(err, ErrConflict)
	}, 5*time.Second, 10*time.Millisecond, "expired claim must become eligible for takeover")
	require.NoError(t, err)
	require.NoError(t, client.ReleaseCheckpointOperation(t.Context(), checkpoint.Id, first))
	_, err = client.FinalizeClaimedSessionCheckpoint(nextCtx, op.Checkpoint, first, "old", "s3://tags/old", "")
	require.ErrorIs(t, err, ErrConflict)
	ready, err := client.FinalizeClaimedSessionCheckpoint(nextCtx, op.Checkpoint, second, "retained", "s3://tags/retained", "")
	require.NoError(t, err)
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_READY, ready.State)
	// Completion must clear both columns in the same write, before any release.
	claim, err := queryOne(t.Context(), client.db, `SELECT executor_id IS NULL AND executor_expires_at IS NULL FROM session_checkpoint WHERE id = $1`, pgx.RowTo[bool], checkpoint.Id)
	require.NoError(t, err)
	require.True(t, claim)
	deletion := uuid.New()
	deleting, err := client.BeginDeleteSessionCheckpoint(nextCtx, checkpoint.Id, "alice", deletion)
	require.NoError(t, err)
	require.Equal(t, "retained", deleting.TagUID)
	require.ErrorIs(t, client.DeleteClaimedSessionCheckpoint(nextCtx, checkpoint.Id, "alice", second), ErrConflict)
	require.NoError(t, client.DeleteClaimedSessionCheckpoint(nextCtx, checkpoint.Id, "alice", deletion))
}

// Delay the first claim after its duration was calculated, so the database lease
// deterministically outlives the client timer. Stored timestamps stay untouched.
type checkpointClaimTransit struct{ once sync.Once }

func (g *checkpointClaimTransit) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "UPDATE session_checkpoint SET executor_id") {
		g.once.Do(func() { time.Sleep(100 * time.Millisecond) })
	}
	return ctx
}

func (*checkpointClaimTransit) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestLegacyUnversionedCheckpointCannotBeClaimed(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	checkpoint, _ := reserveRecoveryCheckpoint(t, client)
	// Prepare a legacy fixture with no claim: the migration preserves such
	// rows, but new Reserve must always insert a non-NULL bounded executor.
	_, err := client.db.Exec(t.Context(), `UPDATE session_checkpoint SET executor_id = NULL, executor_expires_at = NULL WHERE id = $1`, checkpoint.Id)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), RuntimeOperationTimeout)
	defer cancel()
	_, err = client.ClaimCheckpointOperation(ctx, checkpoint.Id, "alice", uuid.New())
	require.ErrorIs(t, err, ErrConflict, "unknown legacy v4 rows cannot be age-fenced")
	_, err = client.GetCheckpointOperation(ctx, checkpoint.Id, "mallory")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestLegacyCheckpointGraceAndExpiredClaims(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	for _, age := range []time.Duration{0, 3 * time.Minute} {
		checkpoint, _ := reserveRecoveryCheckpoint(t, client)
		id, err := uuid.NewV7()
		require.NoError(t, err)
		binary.BigEndian.PutUint64(id[:8], uint64(time.Now().Add(-age).UnixMilli())<<16)
		id[6] = 0x70
		value := proto.CloneOf(checkpoint)
		value.Id = id.String()
		data, err := proto.Marshal(value)
		require.NoError(t, err)
		// A legacy reservation is intentionally constructed as migration input.
		_, err = client.db.Exec(t.Context(), `UPDATE session_checkpoint SET id = $2, data = $3, executor_id = NULL, executor_expires_at = NULL WHERE id = $1`, checkpoint.Id, id, data)
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(t.Context(), RuntimeOperationTimeout)
		executor := uuid.New()
		page, err := client.ListPendingCheckpoints(ctx, "", 100)
		require.NoError(t, err)
		op, err := client.ClaimCheckpointOperation(ctx, id.String(), "alice", executor)
		if age == 0 {
			require.ErrorIs(t, err, ErrConflict)
			require.Empty(t, page)
		} else {
			require.NoError(t, err)
			require.Len(t, page, 1)
			failed, err := client.FinalizeClaimedSessionCheckpoint(ctx, op.Checkpoint, executor, "partial-tag", "", "copy failed")
			require.NoError(t, err)
			require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_FAILED, failed.State)
			retained, err := client.GetCheckpointOperation(ctx, id.String(), "alice")
			require.NoError(t, err)
			require.Equal(t, "partial-tag", retained.TagUID)
			require.Equal(t, "s3://snapshots/source", retained.Snapshot.URI)
		}
		cancel()
	}
}
