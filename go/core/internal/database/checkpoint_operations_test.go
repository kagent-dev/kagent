package database

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

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
	require.NoError(t, client.BeginCheckpointCleanup(ctx, checkpoint.Id, "tag-uid", "copy interrupted"))
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
	require.ErrorIs(t, client.BeginCheckpointCleanup(ctx, checkpoint.Id, "replacement", "copy interrupted"), ErrConflict)
	require.NoError(t, client.BeginCheckpointCleanup(ctx, checkpoint.Id, "tag-uid", "copy interrupted"))
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
