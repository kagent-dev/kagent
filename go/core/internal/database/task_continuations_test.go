package database

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

func continuationSession(t *testing.T, client *Client) *apiv1.Session {
	t.Helper()
	session, _, err := client.CreateSession(t.Context(), newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	session, err = markSessionReady(t.Context(), client, session.Id, "agent.example")
	require.NoError(t, err)
	return session
}

func continuationTask(t *testing.T, client *Client, session *apiv1.Session, input string) *a2a.Task {
	t.Helper()
	task := newSessionTask(uuid.NewString(), input)
	task.ContextID = session.Id
	require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, nil))
	return task
}

func settleContinuation(t *testing.T, client *Client, task *a2a.Task, state a2a.TaskState) {
	t.Helper()
	task.Status = a2a.TaskStatus{State: state}
	require.NoError(t, saveRuntimeTask(t, client, task.ContextID, task, task, &SessionTaskSnapshot{Atespace: "team-a", URI: "snapshot-" + string(task.ID), Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}))
}

func TestCancelTaskContinuationFencesPendingDispatch(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	parent, child := continuationSession(t, client), continuationSession(t, client)
	caller := continuationTask(t, client, parent, "parent")
	require.NoError(t, client.RegisterTaskContinuation(t.Context(), parent.Id, child.Id, "child"))
	callee := continuationTask(t, client, child, "child")
	settleContinuation(t, client, callee, a2a.TaskStateInputRequired)
	settleContinuation(t, client, caller, a2a.TaskStateCompleted)
	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("yes"))
	reply.ID, reply.ContextID, reply.TaskID = "yes", child.Id, callee.ID
	callee, _ = resumeRuntimeTask(t, client, child.Id, reply)
	settleContinuation(t, client, callee, a2a.TaskStateCompleted)
	claimed, err := client.ClaimTaskContinuations(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	id := "kagent-continuation-" + claimed[0].ID.String()
	dispatch := uuid.New()
	require.NoError(t, client.ReserveSessionDispatch(t.Context(), parent.Id, dispatch, id))
	require.NoError(t, client.CancelTaskContinuations(t.Context(), parent.Id, string(caller.ID)))
	require.ErrorIs(t, client.ReserveSessionDispatch(t.Context(), parent.Id, uuid.New(), id), ErrFailedPrecondition)
	resumed := newSessionTask(uuid.NewString(), id)
	resumed.ContextID = parent.Id
	_, err = client.CreateRuntimeTask(t.Context(), parent.Id, taskMutationHash(id), resumed, dispatch.String())
	require.Error(t, err)
	require.ErrorIs(t, client.CompleteTaskContinuation(t.Context(), claimed[0], "late"), ErrConflict)
}

func TestTaskContinuationWaitsForEveryApprovalAndRecoversClaims(t *testing.T) {
	pool := setupTestDB(t)
	client := NewClient(pool, "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	parent, child := continuationSession(t, client), continuationSession(t, client)
	caller := continuationTask(t, client, parent, "parent-input")
	require.NoError(t, client.RegisterTaskContinuation(t.Context(), parent.Id, child.Id, "child-input"))
	require.NoError(t, client.RegisterTaskContinuation(t.Context(), parent.Id, child.Id, "child-input"))
	callee := continuationTask(t, client, child, "child-input")
	settleContinuation(t, client, callee, a2a.TaskStateInputRequired)
	settleContinuation(t, client, caller, a2a.TaskStateCompleted)
	claimed, err := client.ClaimTaskContinuations(t.Context(), 4)
	require.NoError(t, err)
	require.Empty(t, claimed)
	for _, answer := range []string{"first", "second"} {
		reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(answer))
		reply.ID, reply.ContextID, reply.TaskID = answer, child.Id, callee.ID
		callee, _ = resumeRuntimeTask(t, client, child.Id, reply)
		if answer == "first" {
			settleContinuation(t, client, callee, a2a.TaskStateInputRequired)
			claimed, err = client.ClaimTaskContinuations(t.Context(), 4)
			require.NoError(t, err)
			require.Empty(t, claimed)
		}
	}
	settleContinuation(t, client, callee, a2a.TaskStateCompleted)
	restarted := NewClient(pool, "public")
	claimed, err = restarted.ClaimTaskContinuations(t.Context(), 4)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	second, err := client.ClaimTaskContinuations(t.Context(), 4)
	require.NoError(t, err)
	require.Empty(t, second)
	stale := claimed[0]
	stale.ClaimToken = uuid.New()
	require.ErrorIs(t, client.CompleteTaskContinuation(t.Context(), stale, "resume"), ErrConflict)
	require.NoError(t, client.CompleteTaskContinuation(t.Context(), claimed[0], ""))
	_, err = pool.Exec(t.Context(), "UPDATE task_continuation SET lease_until = clock_timestamp() - INTERVAL '1 second'")
	require.NoError(t, err)
	retried, err := restarted.ClaimTaskContinuations(t.Context(), 4)
	require.NoError(t, err)
	require.Len(t, retried, 1)
	require.Equal(t, claimed[0].ID, retried[0].ID)
	require.NotEqual(t, claimed[0].ClaimToken, retried[0].ClaimToken)
	require.NoError(t, restarted.CompleteTaskContinuation(t.Context(), retried[0], "resumed-task"))
	final, err := client.ClaimTaskContinuations(t.Context(), 4)
	require.NoError(t, err)
	require.Empty(t, final)
}

func TestTaskContinuationWaitsForNestedParentResume(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	a, b, c := continuationSession(t, client), continuationSession(t, client), continuationSession(t, client)
	ta := continuationTask(t, client, a, "a")
	require.NoError(t, client.RegisterTaskContinuation(t.Context(), a.Id, b.Id, "b"))
	tb := continuationTask(t, client, b, "b")
	require.NoError(t, client.RegisterTaskContinuation(t.Context(), b.Id, c.Id, "c"))
	tc := continuationTask(t, client, c, "c")
	settleContinuation(t, client, tc, a2a.TaskStateInputRequired)
	settleContinuation(t, client, tb, a2a.TaskStateCompleted)
	settleContinuation(t, client, ta, a2a.TaskStateCompleted)
	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("yes"))
	reply.ID, reply.ContextID, reply.TaskID = "yes", c.Id, tc.ID
	tc, _ = resumeRuntimeTask(t, client, c.Id, reply)
	settleContinuation(t, client, tc, a2a.TaskStateCompleted)
	claimed, err := client.ClaimTaskContinuations(t.Context(), 4)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, b.Id, claimed[0].ParentSessionID)
	resumed := continuationTask(t, client, b, "kagent-continuation-"+claimed[0].ID.String())
	require.NoError(t, client.CompleteTaskContinuation(t.Context(), claimed[0], string(resumed.ID)))
	pending, err := client.ClaimTaskContinuations(t.Context(), 4)
	require.NoError(t, err)
	require.Empty(t, pending)
	settleContinuation(t, client, resumed, a2a.TaskStateCompleted)
	ready, err := client.ClaimTaskContinuations(t.Context(), 4)
	require.NoError(t, err)
	require.Len(t, ready, 1)
	require.Equal(t, a.Id, ready[0].ParentSessionID)
	final, err := client.LatestContinuationTask(t.Context(), b.Id, string(tb.ID))
	require.NoError(t, err)
	require.Equal(t, resumed.ID, final.ID)
}

func TestTaskContinuationSkipsSynchronousCompletionAndCancellation(t *testing.T) {
	for _, state := range []a2a.TaskState{a2a.TaskStateCompleted, a2a.TaskStateCanceled} {
		t.Run(string(state), func(t *testing.T) {
			client := NewClient(setupTestDB(t), "public")
			sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
			parent, child := continuationSession(t, client), continuationSession(t, client)
			caller := continuationTask(t, client, parent, "parent")
			require.NoError(t, client.RegisterTaskContinuation(t.Context(), parent.Id, child.Id, "child"))
			callee := continuationTask(t, client, child, "child")
			require.ErrorIs(t, client.RegisterTaskContinuation(t.Context(), child.Id, parent.Id, "cycle"), ErrFailedPrecondition)
			if state == a2a.TaskStateCanceled {
				settleContinuation(t, client, callee, a2a.TaskStateInputRequired)
			}
			settleContinuation(t, client, caller, a2a.TaskStateCompleted)
			settleContinuation(t, client, callee, state)
			claimed, err := client.ClaimTaskContinuations(t.Context(), 4)
			require.NoError(t, err)
			require.Empty(t, claimed)
		})
	}
}
