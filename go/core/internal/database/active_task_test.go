package database

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

func TestCheckpointCreationBlocksSessionTaskWrites(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	_, err = markSessionReady(ctx, client, session.GetId(), "agent.example")
	require.NoError(t, err)

	task := newSessionTask("completed", "initial-message")
	task.ContextID = session.GetContextId()
	task.Status.State = a2a.TaskStateCompleted
	require.NoError(t, saveRuntimeTask(t, client, session.GetId(), task, task,
		&SessionTaskSnapshot{Atespace: "team-a", URI: "snapshot", ContentScope: "DATA"}))
	checkpoint, _, err := client.ReserveSessionCheckpoint(ctx,
		&apiv1alpha1.Checkpoint{Id: uuid.NewString(), SessionId: session.GetId(), HeadTaskId: string(task.ID)}, "alice", uuid.NewString())
	require.NoError(t, err)

	next := newSessionTask("next", "next-message")
	next.ContextID = session.GetContextId()
	_, err = client.CreateRuntimeTask(ctx, session.GetId(), taskMutationHash("next-request"), next, "")
	require.ErrorIs(t, err, ErrConflict)
	require.ErrorIs(t, saveRuntimeTask(t, client, session.GetId(), task, task, nil), ErrFailedPrecondition)
	_, _, err = client.BeginDeleteSessionCheckpoint(ctx, checkpoint.GetId(), "alice")
	require.ErrorIs(t, err, ErrNotFound)

	_, err = client.FinalizeSessionCheckpoint(ctx, checkpoint.GetId(), "", "", "snapshot failed")
	require.NoError(t, err)
	_, err = client.FinalizeSessionCheckpoint(ctx, checkpoint.GetId(), "tag", "retained", "")
	require.ErrorIs(t, err, ErrNotFound)
	version, err := client.CreateRuntimeTask(ctx, session.GetId(), taskMutationHash("next-request"), next, "")
	require.NoError(t, err)
	require.Positive(t, version)
}
