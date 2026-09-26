package database

import (
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aevent"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestTaskViewsRebuildFromEvents(t *testing.T) {
	pool := setupTestDB(t)
	client, q, ctx := NewClient(pool), pool, t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", "Source"), uuid.NewString())
	require.NoError(t, err)
	_, err = markSessionReady(ctx, client, session.Id, "source.example")
	require.NoError(t, err)
	sessionRow, err := readSession(ctx, q, session.Id)
	require.NoError(t, err)
	task := newSessionTask("task", "initial-message")
	task.ContextID = session.ContextId
	_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("request hash"), task, "")
	require.NoError(t, err)
	assertReplay := func() {
		t.Helper()
		events, err := queryMany(ctx, q, `
			SELECT sequence, history_id, task_id, data, created_at, message_id, task_position, snapshot_atespace, snapshot_uri, snapshot_content_scope FROM session_task_event WHERE
			    history_id = $1 ORDER BY sequence
		`, pgx.RowToStructByName[sessionTaskEventRow], sessionRow.HistoryID)
		require.NoError(t, err)
		rows, err := replayTaskEvents(events, session.ContextId)
		require.NoError(t, err)
		require.NotEmpty(t, rows)
		for _, rebuilt := range rows {
			stored, err := readSessionTask(ctx, q, sessionRow.HistoryID, rebuilt.ID)
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
			require.Equal(t, stored.CreatedAt, rebuilt.CreatedAt)
			require.Equal(t, stored.SnapshotURI, rebuilt.SnapshotURI)
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
		require.NoError(t, saveRuntimeTask(t, client, session.Id, task, event, nil))
		assertReplay()
	}
	question := a2a.NewMessageForTask(a2a.MessageRoleAgent, task, a2a.NewTextPart("Which database?"))
	task.Status = a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: question}
	require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, nil))
	assertReplay()
	_, _, err = client.ReserveSessionCheckpoint(ctx, &apiv1alpha1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID)}, "alice", "hitl-checkpoint")
	require.ErrorIs(t, err, ErrFailedPrecondition)

	// Both a user reply and an immediate message result must persist their status.
	for _, state := range []a2a.TaskState{a2a.TaskStateSubmitted, a2a.TaskStateCompleted} {
		message := a2a.NewMessageForTask(a2a.MessageRoleUser, task, a2a.NewTextPart("PostgreSQL"))
		if state == a2a.TaskStateCompleted {
			message.Role = a2a.MessageRoleAgent
		}
		now := time.Now()
		task.Status = a2a.TaskStatus{State: state, Timestamp: &now}
		var snapshot *SessionTaskSnapshot
		if state == a2a.TaskStateCompleted {
			snapshot = &SessionTaskSnapshot{Atespace: "team-a", URI: "completed", ContentScope: "DATA"}
		}
		require.NoError(t, saveRuntimeTask(t, client, session.Id, task, message, snapshot))
		assertReplay()
	}
	checkpoint, _, err := client.ReserveSessionCheckpoint(ctx, &apiv1alpha1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID)}, "alice", "checkpoint")
	require.NoError(t, err)
	_, err = client.FinalizeSessionCheckpoint(ctx, checkpoint.Id, "tag", "retained", "")
	require.NoError(t, err)
	boundaryEvents, err := readCheckpointEvents(ctx, q, uuid.MustParse(checkpoint.Id))
	require.NoError(t, err)
	before, err := client.GetSessionTask(ctx, session.Id, "task", nil)
	require.NoError(t, err)

	advanced := newSessionTask("later-task", "later-message")
	advanced.ContextID = session.ContextId
	require.NoError(t, saveRuntimeTask(t, client, session.Id, advanced, advanced, nil))
	assertReplay()
	// A source task changing or even losing its view must not affect an old fork.
	events, err := queryMany(ctx, q, `
		SELECT sequence, history_id, task_id, data, created_at, message_id, task_position, snapshot_atespace, snapshot_uri, snapshot_content_scope FROM session_task_event WHERE
		    history_id = $1 ORDER BY sequence
	`, pgx.RowToStructByName[sessionTaskEventRow], sessionRow.HistoryID)
	require.NoError(t, err)
	rows, err := replayTaskEvents(events, session.ContextId)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "DELETE FROM session_task WHERE history_id = $1", sessionRow.HistoryID)
	require.NoError(t, err)
	fork, _, err := client.ForkSession(ctx, checkpoint.Id, "alice", "fork", uuid.NewString())
	require.NoError(t, err)
	forkTasks, _, err := client.ListSessionTasks(ctx, fork.Id, "", a2a.TaskStateUnspecified, nil, 10, nil)
	require.NoError(t, err)
	require.Len(t, forkTasks, 1)
	forked := forkTasks[0]
	require.NoError(t, err)
	require.NotEqual(t, before.ID, forked.ID)
	require.Equal(t, fork.Id, forked.ContextID)
	require.Equal(t, before.Status.State, forked.Status.State)
	require.Equal(t, before.Artifacts, forked.Artifacts)
	require.Len(t, forked.History, len(before.History))
	for _, row := range rows {
		row.HistoryID = sessionRow.HistoryID
		require.NoError(t, insertReplayedTask(ctx, q, row))
	}
	assertReplay()
	// Missing creation, out-of-order events, and identity corruption fail closed.
	for _, broken := range [][]sessionTaskEventRow{boundaryEvents[1:], {boundaryEvents[0], boundaryEvents[0]}} {
		_, err := replayTaskEvents(broken, session.ContextId)
		require.Error(t, err)
	}
	_, err = replayTaskEvents(boundaryEvents, uuid.NewString())
	require.Error(t, err)
}
