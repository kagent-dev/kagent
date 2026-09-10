package database

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

func waitingTaskFixture(t *testing.T, client *Client) (*apiv1alpha1.AgentInstance, *a2a.Task) {
	t.Helper()
	instance, _, err := client.CreateAgentInstance(t.Context(), newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	instance, err = markAgentInstanceReady(t.Context(), client, instance.Id, "agent.example")
	require.NoError(t, err)
	task := newAgentInstanceTask("task", "initial")
	task.ContextID = instance.ContextId
	_, _, err = client.CreateAgentInstanceTask(t.Context(), instance.Id, []byte("initial request"), task)
	require.NoError(t, err)
	task.Status = a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Which database?"))}
	require.NoError(t, client.StoreAgentInstanceTaskEvent(t.Context(), instance.Id, task, task, nil))
	return instance, task
}

func TestContinuationAdmissionIsExclusive(t *testing.T) {
	client := NewClient(setupTestDB(t))
	agentInstanceFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	for _, same := range []bool{true, false} {
		name := "distinct replies"
		if same {
			name = "identical retries"
		}
		t.Run(name, func(t *testing.T) {
			instance, task := waitingTaskFixture(t, client)
			type outcome struct {
				continuation *TaskContinuation
				err          error
			}
			results := make(chan outcome, 2)
			start := make(chan struct{})
			for i := range 2 {
				go func() {
					reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("PostgreSQL"))
					reply.ID, reply.TaskID, reply.ContextID = "reply", task.ID, task.ContextID
					if !same && i == 1 {
						reply.ID = "other-reply"
					}
					<-start
					continuation, err := client.ContinueAgentInstanceTask(t.Context(), instance.Id, []byte(reply.ID), reply)
					results <- outcome{continuation, err}
				}()
			}
			close(start)
			admissions, conflicts := 0, 0
			for range 2 {
				result := <-results
				if result.err != nil {
					require.ErrorIs(t, result.err, ErrConflict)
					conflicts++
					continue
				}
				require.Equal(t, a2a.TaskStateSubmitted, result.continuation.Current.Status.State)
				if result.continuation.Previous != nil {
					admissions++
					require.Equal(t, a2a.TaskStateInputRequired, result.continuation.Previous.Status.State)
					require.Equal(t, task.Status.Message.ID, result.continuation.Previous.Status.Message.ID)
				}
			}
			require.Equal(t, 1, admissions)
			if same {
				require.Zero(t, conflicts)
			} else {
				require.Equal(t, 1, conflicts)
			}
			stored, err := client.GetAgentInstanceTask(t.Context(), instance.Id, string(task.ID), nil)
			require.NoError(t, err)
			require.Len(t, stored.History, 3)
			require.Equal(t, "initial", stored.History[0].ID)
			require.Equal(t, task.Status.Message.ID, stored.History[1].ID)
			require.Equal(t, "PostgreSQL", string(stored.History[2].Parts[0].Content.(a2a.Text)))
			require.Nil(t, stored.Status.Message)
		})
	}
}

func TestContinuationAdmissionBarriersAndReplay(t *testing.T) {
	client := NewClient(setupTestDB(t))
	agentInstanceFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	for _, barrier := range []string{"suspended", "lifecycle", "active task", "initial ID", "question ID", "not waiting"} {
		t.Run(barrier, func(t *testing.T) {
			instance, task := waitingTaskFixture(t, client)
			reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("PostgreSQL"))
			reply.TaskID, reply.ContextID = task.ID, task.ContextID
			wantErr := ErrConflict
			switch barrier {
			case "suspended", "lifecycle":
				next := proto.CloneOf(instance)
				if barrier == "suspended" {
					next.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED
				} else {
					next.Operation = apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND
				}
				_, err := client.TransitionAgentInstance(t.Context(), next, instance.State, instance.Operation)
				require.NoError(t, err)
			case "active task":
				active := newAgentInstanceTask("other", "other-initial")
				active.ContextID = instance.ContextId
				_, _, err := client.CreateAgentInstanceTask(t.Context(), instance.Id, []byte("other"), active)
				require.NoError(t, err)
			case "initial ID":
				reply.ID, wantErr = "initial", ErrIdempotencyConflict
			case "question ID":
				reply.ID, wantErr = task.Status.Message.ID, ErrIdempotencyConflict
			case "not waiting":
				task.Status.State = a2a.TaskStateCompleted
				require.NoError(t, client.StoreAgentInstanceTaskEvent(t.Context(), instance.Id, task, task, nil))
			}
			continuation, err := client.ContinueAgentInstanceTask(t.Context(), instance.Id, []byte("reply hash"), reply)
			require.ErrorIs(t, err, wantErr)
			require.Nil(t, continuation)
			stored, err := client.GetAgentInstanceTask(t.Context(), instance.Id, string(task.ID), nil)
			require.NoError(t, err)
			require.Equal(t, task.Status.State, stored.Status.State)
			require.Len(t, stored.History, 1, "rejected reply must not partially archive the question or answer")
		})
	}
	t.Run("retry after completion and suspend", func(t *testing.T) {
		instance, task := waitingTaskFixture(t, client)
		reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("PostgreSQL"))
		reply.TaskID, reply.ContextID = task.ID, task.ContextID
		continuation, err := client.ContinueAgentInstanceTask(t.Context(), instance.Id, []byte("reply hash"), reply)
		require.NoError(t, err)
		require.NotNil(t, continuation.Previous)
		submitted := continuation.Current
		submitted.Status.State = a2a.TaskStateCompleted
		require.NoError(t, client.StoreAgentInstanceTaskEvent(t.Context(), instance.Id, submitted, submitted, nil))
		next := proto.CloneOf(instance)
		next.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED
		_, err = client.TransitionAgentInstance(t.Context(), next, instance.State, instance.Operation)
		require.NoError(t, err)
		replay, err := client.ContinueAgentInstanceTask(t.Context(), instance.Id, []byte("reply hash"), reply)
		require.NoError(t, err)
		require.Nil(t, replay.Previous)
		require.Equal(t, a2a.TaskStateCompleted, replay.Current.Status.State)
		require.Len(t, replay.Current.History, 3)
		_, err = client.ContinueAgentInstanceTask(t.Context(), instance.Id, []byte("different configuration"), reply)
		require.ErrorIs(t, err, ErrIdempotencyConflict)
		initial := newAgentInstanceTask("ignored-task-ID", "initial")
		initial.ContextID = instance.ContextId
		initialReplay, created, err := client.CreateAgentInstanceTask(t.Context(), instance.Id, []byte("initial request"), initial)
		require.NoError(t, err)
		require.False(t, created)
		require.Equal(t, task.ID, initialReplay.ID)
	})
}

func TestContinuationReceiptSurvivesCheckpointFork(t *testing.T) {
	client := NewClient(setupTestDB(t))
	agentInstanceFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	instance, task := waitingTaskFixture(t, client)
	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("PostgreSQL"))
	reply.TaskID, reply.ContextID = task.ID, task.ContextID
	continuation, err := client.ContinueAgentInstanceTask(t.Context(), instance.Id, []byte("reply hash"), reply)
	require.NoError(t, err)
	submitted := continuation.Current
	submitted.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted}
	require.NoError(t, client.StoreAgentInstanceTaskEvent(t.Context(), instance.Id, submitted, submitted,
		&AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "after-reply", ContentScope: "DATA"}))
	checkpoint, _, err := client.ReserveAgentInstanceCheckpoint(t.Context(), &apiv1alpha1.Checkpoint{Id: uuid.NewString(), AgentInstanceId: instance.Id}, "alice", uuid.NewString())
	require.NoError(t, err)
	_, err = client.FinalizeAgentInstanceCheckpoint(t.Context(), checkpoint.Id, "tag-uid", "retained-reply", "")
	require.NoError(t, err)
	laterTask := newAgentInstanceTask("later-task", "later-initial")
	laterTask.ContextID = instance.ContextId
	laterTask.Status = a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Which table?"))}
	require.NoError(t, client.StoreAgentInstanceTaskEvent(t.Context(), instance.Id, laterTask, laterTask, nil))
	later := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("customers"))
	later.TaskID, later.ContextID = laterTask.ID, laterTask.ContextID
	_, err = client.ContinueAgentInstanceTask(t.Context(), instance.Id, []byte("later hash"), later)
	require.NoError(t, err)
	fork, _, err := client.ForkAgentInstance(t.Context(), checkpoint.Id, "alice", uuid.NewString(), uuid.NewString())
	require.NoError(t, err)
	fork, err = markAgentInstanceReady(t.Context(), client, fork.Id, "fork.example")
	require.NoError(t, err)
	replay, err := client.ContinueAgentInstanceTask(t.Context(), fork.Id, []byte("reply hash"), reply)
	require.NoError(t, err)
	require.Nil(t, replay.Previous, "an inherited reply must not dispatch again")
	require.Equal(t, a2a.TaskStateCompleted, replay.Current.Status.State)
	require.Len(t, replay.Current.History, 3)
	require.Equal(t, reply.ID, replay.Current.History[2].ID)
	_, err = client.ContinueAgentInstanceTask(t.Context(), fork.Id, []byte("changed request"), reply)
	require.ErrorIs(t, err, ErrIdempotencyConflict)
	missing, err := client.ContinueAgentInstanceTask(t.Context(), fork.Id, []byte("later hash"), later)
	require.ErrorIs(t, err, ErrNotFound, "a post-checkpoint task and its reply must not be inherited")
	require.Nil(t, missing)
}
