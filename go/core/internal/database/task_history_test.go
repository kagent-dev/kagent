package database

import (
	"context"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTaskHistoryLimitsAndInstanceScope(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	other, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)

	for _, id := range []string{"first", "second"} {
		task := newAgentInstanceTask(id, id+"-1")
		task.ContextID = instance.ContextId
		task.Status.State = a2a.TaskStateCompleted
		for _, suffix := range []string{"-2", "-3"} {
			message := a2a.NewMessageForTask(a2a.MessageRoleAgent, task, a2a.NewTextPart(suffix))
			message.ID = id + suffix
			task.History = append(task.History, message)
		}
		require.NoError(t, client.StoreAgentInstanceTaskEvent(ctx, instance.Id, task, task, nil))
	}
	for _, test := range []struct {
		name  string
		limit *int
		want  []string
	}{
		{name: "all", want: []string{"-1", "-2", "-3"}},
		{name: "none", limit: new(0)},
		{name: "latest two", limit: new(2), want: []string{"-2", "-3"}},
		{name: "larger than history", limit: new(10), want: []string{"-1", "-2", "-3"}},
		{name: "negative is unlimited", limit: new(-1), want: []string{"-1", "-2", "-3"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			task, err := client.GetAgentInstanceTask(ctx, instance.Id, "first", test.limit)
			require.NoError(t, err)
			tasks, total, err := client.ListAgentInstanceTasks(ctx, instance.Id, "", a2a.TaskStateUnspecified, nil, 10, test.limit)
			require.NoError(t, err)
			require.Equal(t, 2, total)
			require.Len(t, tasks, 2)
			for _, got := range append(tasks, task) {
				require.Len(t, got.History, len(test.want))
				for i, suffix := range test.want {
					require.Equal(t, string(got.ID)+suffix, got.History[i].ID)
					require.Equal(t, got.ID, got.History[i].TaskID)
					require.Equal(t, instance.ContextId, got.History[i].ContextID)
				}
			}
		})
	}
	_, err = client.GetAgentInstanceTask(ctx, other.Id, "first", nil)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.GetAgentInstanceTask(ctx, uuid.NewString(), "first", nil)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.GetAgentInstanceTask(ctx, instance.Id, "absent", nil)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestZeroTaskHistoryDoesNotQueryMessages(t *testing.T) {
	task := &a2a.Task{ID: "task", History: []*a2a.Message{{ID: "inline"}}}
	// No executor is needed when the caller asks to omit history.
	require.NoError(t, loadAgentInstanceTaskHistories(context.Background(), nil, uuid.New(), []*a2a.Task{task}, new(0)))
	require.Empty(t, task.History)
}

func TestTaskHistoryLimitWithInlineMessages(t *testing.T) {
	client := NewClient(setupTestDB(t))
	task := &a2a.Task{ID: "task", History: []*a2a.Message{{ID: "first"}, {ID: "second"}, {ID: "third"}}}
	require.NoError(t, loadAgentInstanceTaskHistories(t.Context(), client.db, uuid.New(), []*a2a.Task{task}, new(2)))
	require.Equal(t, []*a2a.Message{{ID: "second"}, {ID: "third"}}, task.History)
}
