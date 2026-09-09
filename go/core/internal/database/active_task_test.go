package database

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

func TestGetActiveAgentInstanceTaskUsesInstanceHistory(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	_, err = markAgentInstanceReady(ctx, client, instance.GetId(), "agent.example")
	require.NoError(t, err)

	_, err = client.GetActiveAgentInstanceTask(ctx, instance.GetId())
	require.ErrorIs(t, err, ErrNotFound)

	task := newAgentInstanceTask("active", "initial-message")
	task.ContextID = instance.GetContextId()
	_, _, err = client.CreateAgentInstanceTask(ctx, instance.GetId(), []byte("request"), task)
	require.NoError(t, err)
	active, err := client.GetActiveAgentInstanceTask(ctx, instance.GetId())
	require.NoError(t, err)
	require.Equal(t, task.ID, active.ID)
	require.Equal(t, task.ContextID, active.ContextID)
	require.Len(t, active.History, 1)
	require.Equal(t, task.History[0].ID, active.History[0].ID)
	require.Equal(t, task.ID, active.History[0].TaskID)
	require.Equal(t, task.ContextID, active.History[0].ContextID)

	_, err = client.GetActiveAgentInstanceTask(ctx, uuid.NewString())
	require.ErrorIs(t, err, ErrNotFound)

	task.Status.State = a2a.TaskStateCompleted
	require.NoError(t, client.StoreAgentInstanceTaskEvent(ctx, instance.GetId(), task, task, nil))
	_, err = client.GetActiveAgentInstanceTask(ctx, instance.GetId())
	require.ErrorIs(t, err, ErrNotFound)
}

func TestStoreAgentInstanceTaskEventRequiresTaskAndEvent(t *testing.T) {
	client := &Client{}
	task := newAgentInstanceTask("task", "message")
	for _, test := range []struct {
		name  string
		task  *a2a.Task
		event a2a.Event
	}{
		{name: "missing task", event: task},
		{name: "missing event", task: task},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.ErrorContains(t, client.StoreAgentInstanceTaskEvent(t.Context(), uuid.NewString(), test.task, test.event, nil), "task and event are required")
		})
	}
}

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
	require.NoError(t, client.StoreAgentInstanceTaskEvent(ctx, instance.GetId(), task, task,
		&AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "snapshot", ContentScope: "DATA"}))
	checkpoint, _, err := client.ReserveAgentInstanceCheckpoint(ctx,
		&apiv1alpha1.Checkpoint{Id: uuid.NewString(), AgentInstanceId: instance.GetId()}, "alice", uuid.NewString())
	require.NoError(t, err)

	next := newAgentInstanceTask("next", "next-message")
	next.ContextID = instance.GetContextId()
	_, _, err = client.CreateAgentInstanceTask(ctx, instance.GetId(), []byte("next-request"), next)
	require.ErrorIs(t, err, ErrAgentInstanceTaskConflict)
	require.ErrorIs(t, client.StoreAgentInstanceTaskEvent(ctx, instance.GetId(), task, task, nil), ErrAgentInstanceConflict)
	_, _, err = client.BeginDeleteAgentInstanceCheckpoint(ctx, checkpoint.GetId(), "alice")
	require.ErrorIs(t, err, ErrNotFound)

	_, err = client.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.GetId(), "", "", "snapshot failed")
	require.NoError(t, err)
	_, err = client.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.GetId(), "tag", "retained", "")
	require.ErrorIs(t, err, ErrNotFound)
	_, created, err := client.CreateAgentInstanceTask(ctx, instance.GetId(), []byte("next-request"), next)
	require.NoError(t, err)
	require.True(t, created)
}
