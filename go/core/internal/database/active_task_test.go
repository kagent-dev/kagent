package database

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestGetActiveAgentInstanceTaskUsesInstanceHistory(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	_, err = client.MarkAgentInstanceReady(ctx, instance.GetId(), "agent.example")
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
