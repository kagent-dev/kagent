package database

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aevent"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	corea2a "github.com/kagent-dev/kagent/go/core/internal/a2a"
	"github.com/stretchr/testify/require"
)

func observationFixture(t *testing.T) (*Client, *apiv1alpha1.AgentInstance, *a2a.Task) {
	t.Helper()
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	instance, err = markAgentInstanceReady(ctx, client, instance.Id, "runtime.example")
	require.NoError(t, err)
	task := newAgentInstanceTask("task", "initial")
	task.ContextID = instance.ContextId
	_, _, err = client.CreateAgentInstanceTask(ctx, instance.Id, []byte("request"), task)
	require.NoError(t, err)
	return client, instance, task
}

func TestTaskReadsDoNotMixConcurrentCommits(t *testing.T) {
	for _, listing := range []bool{false, true} {
		name, query := "snapshot", "SELECT t.data,"
		if listing {
			name, query = "list", "SELECT COUNT(*) FROM agent_instance_task"
		}
		t.Run(name, func(t *testing.T) {
			client, instance, task := observationFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			before, err := client.GetAgentInstanceTaskObservation(ctx, instance.Id, string(task.ID), nil)
			require.NoError(t, err)
			barrier := &runtimeReferenceBarrier{query: query, afterQuery: true, reached: make(chan struct{}), resume: make(chan struct{})}
			var resume sync.Once
			defer resume.Do(func() { close(barrier.resume) })
			config := client.db.Config()
			config.ConnConfig.Tracer = barrier
			pool, err := pgxpool.NewWithConfig(ctx, config)
			require.NoError(t, err)
			t.Cleanup(pool.Close)
			reader := NewClient(pool)
			done := make(chan error, 1)
			var got *TaskObservation
			var listed []*a2a.Task
			var total int
			go func() {
				if listing {
					var err error
					listed, total, err = reader.ListAgentInstanceTasks(ctx, instance.Id, "", "", nil, 10, nil)
					done <- err
				} else {
					var err error
					got, err = reader.GetAgentInstanceTaskObservation(ctx, instance.Id, string(task.ID), nil)
					done <- err
				}
			}()
			select {
			case <-barrier.reached:
			case <-ctx.Done():
				t.Fatal("read did not reach snapshot barrier")
			}
			message := a2a.NewMessageForTask(a2a.MessageRoleAgent, task, a2a.NewTextPart("Committed during read"))
			task.Status.State = a2a.TaskStateCompleted
			task.History = append(task.History, message)
			require.NoError(t, client.StoreAgentInstanceTaskEvent(ctx, instance.Id, task, task, nil))
			if listing {
				next := newAgentInstanceTask("next", "next-input")
				next.ContextID = instance.ContextId
				_, _, err := client.CreateAgentInstanceTask(ctx, instance.Id, []byte("next"), next)
				require.NoError(t, err)
			}
			resume.Do(func() { close(barrier.resume) })
			require.NoError(t, <-done)
			if listing {
				require.Equal(t, 1, total)
				require.Len(t, listed, 1)
				require.Equal(t, before.Task, listed[0])
			} else {
				require.Equal(t, before, got)
				events, err := client.ListAgentInstanceTaskEvents(ctx, instance.Id, string(task.ID), got.Sequence, 10)
				require.NoError(t, err)
				require.Len(t, events, 2, "the concurrent task update and archived message must both follow the snapshot")
				latest, err := client.GetAgentInstanceTask(ctx, instance.Id, string(task.ID), nil)
				require.NoError(t, err)
				require.Equal(t, latest, recoverObservedTask(t, got.Task, events))
			}
		})
	}
}

// recoverObservedTask exercises the existing checkpoint reducer starting from a
// client-visible snapshot, retaining archived messages as its synthesized history.
func recoverObservedTask(t *testing.T, initial *a2a.Task, events []TaskEvent) *a2a.Task {
	t.Helper()
	task, err := pbconv.ToProtoTask(initial)
	require.NoError(t, err)
	history := task.History
	task.History = nil
	for _, record := range events {
		event, err := pbconv.ToProtoStreamResponse(record.Event)
		require.NoError(t, err)
		if message := event.GetMessage(); message != nil {
			history = append(history, message)
		}
		task, err = corea2a.ApplyTaskEvent(task, event)
		require.NoError(t, err)
	}
	task.History = history
	recovered, err := pbconv.FromProtoTask(task)
	require.NoError(t, err)
	return recovered
}

func TestTaskRecoveryUsesExistingHistoryAndArtifactUpdates(t *testing.T) {
	client, instance, task := observationFixture(t)
	ctx := t.Context()
	before, err := client.GetAgentInstanceTaskObservation(ctx, instance.Id, string(task.ID), nil)
	require.NoError(t, err)
	require.Positive(t, before.Sequence)
	// The snapshot includes the archived input, so its position must be past that
	// archive row as well as the task creation record.
	empty, err := client.ListAgentInstanceTaskEvents(ctx, instance.Id, string(task.ID), before.Sequence, 10)
	require.NoError(t, err)
	require.Empty(t, empty)
	for _, event := range []a2a.Event{
		&a2a.TaskStatusUpdateEvent{TaskID: task.ID, ContextID: task.ContextID, Status: a2a.TaskStatus{State: a2a.TaskStateWorking}, Metadata: map[string]any{"progress": "started"}},
		&a2a.TaskArtifactUpdateEvent{TaskID: task.ID, ContextID: task.ContextID, Artifact: &a2a.Artifact{ID: "reply", Parts: a2a.ContentParts{a2a.NewTextPart("hello")}}},
		&a2a.TaskArtifactUpdateEvent{TaskID: task.ID, ContextID: task.ContextID, Append: true, LastChunk: true, Artifact: &a2a.Artifact{ID: "reply", Parts: a2a.ContentParts{a2a.NewTextPart(" world")}}},
	} {
		task, err = a2aevent.ApplyUpdate(task, event)
		require.NoError(t, err)
		require.NoError(t, client.StoreAgentInstanceTaskEvent(ctx, instance.Id, task, event, nil))
	}
	question := a2a.NewMessageForTask(a2a.MessageRoleAgent, task, a2a.NewTextPart("Continue?"))
	task.Status = a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: question}
	require.NoError(t, client.StoreAgentInstanceTaskEvent(ctx, instance.Id, task, task, nil))
	reply := a2a.NewMessageForTask(a2a.MessageRoleUser, task, a2a.NewTextPart("Yes"))
	continued, err := client.ContinueAgentInstanceTask(ctx, instance.Id, []byte("reply"), reply)
	require.NoError(t, err)
	output := a2a.NewMessageForTask(a2a.MessageRoleAgent, continued.Current, a2a.NewTextPart("Finished"))
	finished := *continued.Current
	finished.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted}
	require.NoError(t, client.StoreAgentInstanceTaskEvent(ctx, instance.Id, &finished, output,
		&AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "snapshot", ContentScope: "DATA"}))

	cursor := before.Sequence
	recovered := before.Task
	var records []TaskEvent
	for {
		page, err := client.ListAgentInstanceTaskEvents(ctx, instance.Id, string(task.ID), cursor, 1)
		require.NoError(t, err)
		if len(page) == 0 {
			break
		}
		require.Greater(t, page[0].Sequence, cursor)
		cursor = page[0].Sequence
		recovered = recoverObservedTask(t, recovered, page)
		records = append(records, page...)
	}
	latest, err := client.GetAgentInstanceTaskObservation(ctx, instance.Id, string(task.ID), nil)
	require.NoError(t, err)
	require.Equal(t, latest.Sequence, cursor)
	require.Equal(t, latest.Task, recovered)
	require.Len(t, recovered.History, 4) // input, question, reply, output
	require.Len(t, recovered.Artifacts[0].Parts, 2, "append is applied exactly once")
	appendEvent := records[2].Event.(*a2a.TaskArtifactUpdateEvent)
	require.True(t, appendEvent.Append)
	require.True(t, appendEvent.LastChunk)

	checkpoint, _, err := client.ReserveAgentInstanceCheckpoint(ctx, &apiv1alpha1.Checkpoint{Id: uuid.NewString(), AgentInstanceId: instance.Id}, "alice", "checkpoint")
	require.NoError(t, err)
	_, err = client.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.Id, "tag", "snapshot", "")
	require.NoError(t, err)
	fork, _, err := client.ForkAgentInstance(ctx, checkpoint.Id, "alice", "fork", uuid.NewString())
	require.NoError(t, err)
	forked, err := client.GetAgentInstanceTaskObservation(ctx, fork.Id, string(task.ID), nil)
	require.NoError(t, err)
	require.Equal(t, latest.Task, forked.Task)
	late := a2a.NewMessageForTask(a2a.MessageRoleAgent, &finished, a2a.NewTextPart("source only"))
	require.NoError(t, client.StoreAgentInstanceTaskEvent(ctx, instance.Id, &finished, late, nil))
	empty, err = client.ListAgentInstanceTaskEvents(ctx, fork.Id, string(task.ID), forked.Sequence, 10)
	require.NoError(t, err)
	require.Empty(t, empty, "a fork's task ID must not select the source's later events")
}

func TestTaskObservationRejectsMissingDeletedAndMalformedData(t *testing.T) {
	client, instance, task := observationFixture(t)
	ctx := t.Context()
	for _, ids := range [][2]string{{uuid.NewString(), string(task.ID)}, {instance.Id, "missing"}} {
		_, err := client.GetAgentInstanceTaskObservation(ctx, ids[0], ids[1], nil)
		require.ErrorIs(t, err, ErrNotFound)
		_, err = client.ListAgentInstanceTaskEvents(ctx, ids[0], ids[1], 0, 10)
		require.ErrorIs(t, err, ErrNotFound)
	}
	for _, args := range [][2]int{{-1, 10}, {0, 0}, {0, 1001}} {
		_, err := client.ListAgentInstanceTaskEvents(ctx, instance.Id, string(task.ID), int64(args[0]), args[1])
		require.Error(t, err)
	}
	before, err := client.GetAgentInstanceTaskObservation(ctx, instance.Id, string(task.ID), nil)
	require.NoError(t, err)
	task.History = append(task.History, &a2a.Message{Role: a2a.MessageRoleAgent})
	task.Status.State = a2a.TaskStateCompleted
	require.Error(t, client.StoreAgentInstanceTaskEvent(ctx, instance.Id, task, task, nil))
	after, err := client.GetAgentInstanceTaskObservation(ctx, instance.Id, string(task.ID), nil)
	require.NoError(t, err)
	require.Equal(t, before, after, "failed history writes roll back the task and event position")

	_, err = client.db.Exec(ctx, `UPDATE agent_instance_task_event SET data = $2 WHERE sequence = $1`, before.Sequence, []byte{0xff})
	require.NoError(t, err)
	_, err = client.ListAgentInstanceTaskEvents(ctx, instance.Id, string(task.ID), 0, 10)
	require.Error(t, err)
	_, err = client.GetAgentInstanceTaskObservation(ctx, instance.Id, string(task.ID), nil)
	require.Error(t, err)
	require.NoError(t, client.DeleteAgentInstance(ctx, instance.Id))
	_, err = client.GetAgentInstanceTaskObservation(ctx, instance.Id, string(task.ID), nil)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.ListAgentInstanceTaskEvents(ctx, instance.Id, string(task.ID), 0, 10)
	require.ErrorIs(t, err, ErrNotFound)
}
