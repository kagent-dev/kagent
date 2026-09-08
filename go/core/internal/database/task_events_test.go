package database

import (
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aevent"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database/internal/dbgen"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestTaskViewsRebuildFromEvents(t *testing.T) {
	pool := setupTestDB(t)
	client, q, ctx := NewClient(pool), dbgen.New(pool), t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", "Source"), uuid.NewString())
	require.NoError(t, err)
	_, err = client.MarkAgentInstanceReady(ctx, instance.Id, "source.example")
	require.NoError(t, err)
	historyID, err := client.agentInstanceHistoryID(ctx, instance.Id)
	require.NoError(t, err)
	task := newAgentInstanceTask("task", "initial-message")
	task.ContextID = instance.ContextId
	_, _, err = client.CreateAgentInstanceTask(ctx, instance.Id, []byte("request hash"), task)
	require.NoError(t, err)
	assertReplay := func() {
		t.Helper()
		events, err := q.ListAgentInstanceTaskEvents(ctx, historyID)
		require.NoError(t, err)
		rows, err := replayTaskEvents(events, instance.ContextId)
		require.NoError(t, err)
		require.NotEmpty(t, rows)
		for _, rebuilt := range rows {
			stored, err := q.GetAgentInstanceTask(ctx, dbgen.GetAgentInstanceTaskParams{HistoryID: historyID, ID: rebuilt.ID})
			require.NoError(t, err)
			want, got := &a2apb.Task{}, &a2apb.Task{}
			require.NoError(t, proto.Unmarshal(stored.Data, want))
			require.NoError(t, proto.Unmarshal(rebuilt.Data, got))
			require.True(t, proto.Equal(want, got), "replay differs from live task")
			require.Equal(t, stored.State, rebuilt.State)
			if stored.StatusTimestamp == nil {
				require.Nil(t, rebuilt.StatusTimestamp)
			} else {
				require.NotNil(t, rebuilt.StatusTimestamp)
				// PostgreSQL timestamp indexes have microsecond precision; protobufs
				// retain the original timestamp and are compared exactly above.
				require.Equal(t, stored.StatusTimestamp.UnixMicro(), rebuilt.StatusTimestamp.UnixMicro())
			}
			require.Equal(t, stored.Position, rebuilt.Position)
			require.Equal(t, stored.InitialMessageID, rebuilt.InitialMessageID)
			require.Equal(t, stored.RequestHash, rebuilt.RequestHash)
			require.Equal(t, stored.CreatedAt, rebuilt.CreatedAt)
			require.Equal(t, stored.UpdatedAt, rebuilt.UpdatedAt)
			require.Equal(t, stored.SnapshotUri, rebuilt.SnapshotUri)
			require.Equal(t, stored.SnapshotAtespace, rebuilt.SnapshotAtespace)
			require.Equal(t, stored.SnapshotContentScope, rebuilt.SnapshotContentScope)
			require.Equal(t, stored.HistorySequence, rebuilt.HistorySequence)
		}
	}
	assertReplay()
	for _, event := range []a2a.Event{
		&a2a.TaskStatusUpdateEvent{TaskID: task.ID, ContextID: task.ContextID, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}, Metadata: map[string]any{"progress": "started"}},
		&a2a.TaskArtifactUpdateEvent{TaskID: task.ID, ContextID: task.ContextID, Artifact: &a2a.Artifact{ID: "result", Parts: a2a.ContentParts{a2a.NewTextPart("first")}}},
		&a2a.TaskArtifactUpdateEvent{TaskID: task.ID, ContextID: task.ContextID, Append: true, Artifact: &a2a.Artifact{ID: "result", Parts: a2a.ContentParts{a2a.NewTextPart("second")}, Metadata: map[string]any{"chunk": "last"}}},
		&a2a.TaskArtifactUpdateEvent{TaskID: task.ID, ContextID: task.ContextID, Artifact: &a2a.Artifact{ID: "result", Parts: a2a.ContentParts{a2a.NewTextPart("replacement")}}},
	} {
		task, err = a2aevent.ApplyUpdate(task, event)
		require.NoError(t, err)
		require.NoError(t, client.StoreAgentInstanceTaskEvent(ctx, instance.Id, task, event, nil))
		assertReplay()
	}
	question := a2a.NewMessageForTask(a2a.MessageRoleAgent, task, a2a.NewTextPart("Which database?"))
	task.Status = a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: question}
	require.NoError(t, client.StoreAgentInstanceTaskEvent(ctx, instance.Id, task, task, &AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "paused", ContentScope: "DATA"}))
	assertReplay()
	checkpoint, _, err := client.ReserveAgentInstanceCheckpoint(ctx, &apiv1alpha1.Checkpoint{Id: uuid.NewString(), AgentInstanceId: instance.Id}, "alice", "checkpoint")
	require.NoError(t, err)
	_, err = client.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.Id, "tag", "retained", "")
	require.NoError(t, err)
	boundaryEvents, err := q.ListAgentInstanceCheckpointEvents(ctx, uuid.MustParse(checkpoint.Id))
	require.NoError(t, err)
	before, err := client.GetAgentInstanceTask(ctx, instance.Id, "task")
	require.NoError(t, err)
	// Both a user reply and an immediate message result must persist their status.
	for _, state := range []a2a.TaskState{a2a.TaskStateSubmitted, a2a.TaskStateCompleted} {
		message := a2a.NewMessageForTask(a2a.MessageRoleUser, task, a2a.NewTextPart("PostgreSQL"))
		if state == a2a.TaskStateCompleted {
			message.Role = a2a.MessageRoleAgent
		}
		now := time.Now()
		task.Status = a2a.TaskStatus{State: state, Timestamp: &now}
		require.NoError(t, client.StoreAgentInstanceTaskEvent(ctx, instance.Id, task, message, nil))
		assertReplay()
	}
	// A source task changing or even losing its view must not affect an old fork.
	events, err := q.ListAgentInstanceTaskEvents(ctx, historyID)
	require.NoError(t, err)
	rows, err := replayTaskEvents(events, instance.ContextId)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "DELETE FROM agent_instance_task WHERE history_id = $1", historyID)
	require.NoError(t, err)
	fork, _, err := client.ForkAgentInstance(ctx, checkpoint.Id, "alice", "fork", uuid.NewString())
	require.NoError(t, err)
	forked, err := client.GetAgentInstanceTask(ctx, fork.Id, "task")
	require.NoError(t, err)
	require.Equal(t, before, forked)
	for _, row := range rows {
		row.HistoryID = historyID
		require.NoError(t, q.InsertCopiedAgentInstanceTask(ctx, row))
	}
	assertReplay()
	retry := newAgentInstanceTask("ignored", "initial-message")
	retry.ContextID = instance.ContextId
	_, created, err := client.CreateAgentInstanceTask(ctx, instance.Id, []byte("request hash"), retry)
	require.NoError(t, err)
	require.False(t, created)
	// Missing creation, out-of-order events, and identity corruption fail closed.
	for _, broken := range [][]dbgen.AgentInstanceTaskEvent{boundaryEvents[1:], {boundaryEvents[0], boundaryEvents[0]}} {
		_, err := replayTaskEvents(broken, instance.ContextId)
		require.Error(t, err)
	}
	_, err = replayTaskEvents(boundaryEvents, uuid.NewString())
	require.Error(t, err)
	// Reclamation must record its failure transition, not just its message.
	active := newAgentInstanceTask("interrupted", "next-message")
	active.ContextID = instance.ContextId
	_, _, err = client.CreateAgentInstanceTask(ctx, instance.Id, []byte("next request"), active)
	require.NoError(t, err)
	interrupted, err := client.InterruptActiveAgentInstanceTask(ctx, instance.Id, string(active.ID))
	require.NoError(t, err)
	require.True(t, interrupted)
	assertReplay()
}
