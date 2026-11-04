package database

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

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

func TestCheckpointExecutorFencesStaleTransitions(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	checkpoint, _ := reserveRecoveryCheckpoint(t, client)
	first := uuid.New()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err := client.ClaimCheckpointOperation(ctx, checkpoint.Id, "alice", first)
	require.NoError(t, err)
	otherCtx, cancelOther := context.WithTimeout(t.Context(), RuntimeOperationTimeout)
	defer cancelOther()
	_, err = client.ClaimCheckpointOperation(otherCtx, checkpoint.Id, "alice", uuid.New())
	require.ErrorIs(t, err, ErrConflict, "a live attempt must exclude another worker")
	_, err = client.ClaimCheckpointOperation(otherCtx, checkpoint.Id, "mallory", uuid.New())
	require.ErrorIs(t, err, ErrConflict)
	<-ctx.Done()

	second := uuid.New()
	require.Eventually(t, func() bool {
		_, err = client.ClaimCheckpointOperation(otherCtx, checkpoint.Id, "alice", second)
		return err == nil
	}, time.Second, 10*time.Millisecond)
	// A late release and late writes must not revoke or complete the replacement.
	require.NoError(t, client.ReleaseCheckpointOperation(t.Context(), checkpoint.Id, first))
	_, err = client.FinalizeClaimedSessionCheckpoint(t.Context(), checkpoint.Id, first, "old-tag", "s3://tags/old", "")
	require.ErrorIs(t, err, ErrConflict)
	require.ErrorIs(t, client.BeginCheckpointCleanup(t.Context(), checkpoint.Id, first, "old-tag", "old failure"), ErrConflict)
	_, err = client.FinalizeSessionCheckpoint(t.Context(), checkpoint.Id, "unclaimed", "s3://tags/unclaimed", "")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.ClaimCheckpointOperation(otherCtx, checkpoint.Id, "alice", uuid.New())
	require.ErrorIs(t, err, ErrConflict, "the replacement must still own the attempt")
	_, err = client.FinalizeClaimedSessionCheckpoint(otherCtx, checkpoint.Id, second, "new-tag", "s3://tags/new", "")
	require.NoError(t, err)
	require.NoError(t, client.ReleaseCheckpointOperation(t.Context(), checkpoint.Id, second))

	_, _, err = client.BeginDeleteSessionCheckpoint(t.Context(), checkpoint.Id, "alice")
	require.NoError(t, err)
	deleting := uuid.New()
	_, err = client.ClaimCheckpointOperation(otherCtx, checkpoint.Id, "alice", deleting)
	require.NoError(t, err)
	require.ErrorIs(t, client.DeleteClaimedSessionCheckpoint(t.Context(), checkpoint.Id, "alice", second), ErrConflict)
	retained, err := client.GetCheckpointOperation(t.Context(), checkpoint.Id, "alice")
	require.NoError(t, err)
	require.Equal(t, "new-tag", retained.TagUID)
	require.NoError(t, client.DeleteClaimedSessionCheckpoint(otherCtx, checkpoint.Id, "alice", deleting))
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
	checkpoint, _, err := client.ReserveSessionCheckpoint(ctx,
		&apiv1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID)}, "alice", uuid.NewString())
	require.NoError(t, err)
	return checkpoint, session
}

func TestCheckpointCleanupSurvivesRestartAndFencesPublication(t *testing.T) {
	pool := setupTestDB(t)
	client := NewClient(pool, "public")
	checkpoint, session := reserveRecoveryCheckpoint(t, client)
	ctx := t.Context()
	require.NoError(t, client.BeginCheckpointCleanup(ctx, checkpoint.Id, uuid.Nil, "tag-uid", "copy interrupted"))
	client = NewClient(pool, "public")
	op, err := client.GetCheckpointOperation(ctx, checkpoint.Id, "alice")
	require.NoError(t, err)
	require.Equal(t, "tag-uid", op.TagUID)
	require.Equal(t, "copy interrupted", op.Checkpoint.Failure.Message)
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_CREATING, op.Checkpoint.State)
	_, err = client.GetCheckpointOperation(ctx, checkpoint.Id, "mallory")
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.GetSessionCheckpoint(ctx, checkpoint.Id, "alice")
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, deleteSession(ctx, client, session.Id), ErrConflict)
	_, _, err = client.ForkSession(ctx, checkpoint.Id, "alice", uuid.NewString(), uuid.NewString())
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.FinalizeSessionCheckpoint(ctx, checkpoint.Id, "tag-uid", "s3://tags/copy", "")
	require.ErrorIs(t, err, ErrConflict, "cleanup must not race a stale successful copy into READY")
	require.ErrorIs(t, client.BeginCheckpointCleanup(ctx, checkpoint.Id, uuid.Nil, "replacement", "copy interrupted"), ErrConflict)
	require.NoError(t, client.BeginCheckpointCleanup(ctx, checkpoint.Id, uuid.Nil, "tag-uid", "copy interrupted"))
	_, err = client.FinalizeSessionCheckpoint(ctx, checkpoint.Id, "", "", "different failure")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.FinalizeSessionCheckpoint(ctx, checkpoint.Id, "", "", "copy interrupted")
	require.NoError(t, err)
	// Only completion of compensation releases source admission.
	next := newSessionTask(uuid.NewString(), uuid.NewString())
	next.ContextID = session.ContextId
	_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("after-cleanup"), next, "")
	require.NoError(t, err)
}

func TestPendingCheckpointsPageBothPhasesAndRetainInputs(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	creating, _ := reserveRecoveryCheckpoint(t, client)
	deleting, _ := reserveRecoveryCheckpoint(t, client)
	failed, _ := reserveRecoveryCheckpoint(t, client)
	ctx := t.Context()
	_, err := client.FinalizeSessionCheckpoint(ctx, deleting.Id, "retained-uid", "s3://tags/retained", "")
	require.NoError(t, err)
	_, _, err = client.BeginDeleteSessionCheckpoint(ctx, deleting.Id, "alice")
	require.NoError(t, err)
	_, err = client.FinalizeSessionCheckpoint(ctx, failed.Id, "", "", "failed")
	require.NoError(t, err)
	var after string
	seen := map[string]*CheckpointOperation{}
	for {
		page, err := client.ListPendingCheckpoints(ctx, after, 1)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		require.Len(t, page, 1)
		op := page[0]
		seen[op.Checkpoint.Id] = op
		after = op.Checkpoint.Id
	}
	require.Len(t, seen, 2)
	require.Equal(t, "s3://snapshots/source", seen[creating.Id].Snapshot.URI)
	require.Equal(t, "alice", seen[creating.Id].Creator)
	require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_DELETING, seen[deleting.Id].Checkpoint.State)
	require.Equal(t, "s3://tags/retained", seen[deleting.Id].Snapshot.URI)
	require.Equal(t, "retained-uid", seen[deleting.Id].TagUID)
	require.NotContains(t, seen, failed.Id)
}
