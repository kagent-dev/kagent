package database

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

func TestCheckpointCreationBlocksInstanceTaskWrites(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	_, err = markAgentInstanceReady(ctx, client, instance.GetId(), "agent.example")
	require.NoError(t, err)

	task := newAgentInstanceTask("completed", "initial-message")
	task.ContextID = instance.GetContextId()
	task.Status.State = a2a.TaskStateCompleted
	require.NoError(t, saveRuntimeTask(t, client, instance.GetId(), task, task,
		&AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "snapshot", ContentScope: "DATA"}))
	checkpoint, _, err := client.ReserveAgentInstanceCheckpoint(ctx,
		&apiv1alpha1.Checkpoint{Id: uuid.NewString(), AgentInstanceId: instance.GetId(), HeadTaskId: string(task.ID)}, "alice", uuid.NewString())
	require.NoError(t, err)

	next := newAgentInstanceTask("next", "next-message")
	next.ContextID = instance.GetContextId()
	_, err = client.CreateRuntimeTask(ctx, instance.GetId(), taskMutationHash("next-request"), next, "")
	require.ErrorIs(t, err, ErrConflict)
	require.ErrorIs(t, saveRuntimeTask(t, client, instance.GetId(), task, task, nil), ErrFailedPrecondition)
	_, _, err = client.BeginDeleteAgentInstanceCheckpoint(ctx, checkpoint.GetId(), "alice")
	require.ErrorIs(t, err, ErrNotFound)

	_, err = client.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.GetId(), "", "", "snapshot failed")
	require.NoError(t, err)
	_, err = client.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.GetId(), "tag", "retained", "")
	require.ErrorIs(t, err, ErrNotFound)
	version, err := client.CreateRuntimeTask(ctx, instance.GetId(), taskMutationHash("next-request"), next, "")
	require.NoError(t, err)
	require.Positive(t, version)
}
