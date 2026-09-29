package database

import (
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
)

func stalledTaskFixture(t *testing.T, state a2a.TaskState) (*Client, *apiv1alpha1.Session, *a2a.Task, int64) {
	t.Helper()
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision-1", "assistant", "kagent")
	session, _, err := client.CreateSession(t.Context(), newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	session, err = markSessionReady(t.Context(), client, session.Id, "agent.example")
	require.NoError(t, err)
	task := newSessionTask(uuid.NewString(), "initial")
	task.ContextID, task.Status.State = session.ContextId, state
	version, err := client.CreateRuntimeTask(t.Context(), session.Id, taskMutationHash("initial request"), task, "")
	require.NoError(t, err)
	return client, session, task, version
}

func TestStalledSessionTaskIsListedOnlyPastTheCutoff(t *testing.T) {
	for _, state := range []a2a.TaskState{a2a.TaskStateSubmitted, a2a.TaskStateWorking} {
		t.Run(string(state), func(t *testing.T) {
			client, session, task, _ := stalledTaskFixture(t, state)
			stalled, err := client.ListStalledSessionTasks(t.Context(), time.Now().Add(-time.Hour), 100)
			require.NoError(t, err)
			require.Empty(t, stalled, "a turn younger than the cutoff is not stalled")
			stalled, err = client.ListStalledSessionTasks(t.Context(), time.Now().Add(time.Hour), 100)
			require.NoError(t, err)
			require.Len(t, stalled, 1)
			require.Equal(t, session.Id, stalled[0].SessionID)
			require.Equal(t, string(task.ID), stalled[0].TaskID)
			require.Equal(t, state, stalled[0].State)
			_, err = client.InterruptSessionTask(t.Context(), session.Id, string(task.ID), time.Now().Add(-time.Hour), "interrupted")
			require.ErrorIs(t, err, ErrConflict, "the cutoff is rechecked under the lock")
		})
	}
}

func TestInterruptSessionTaskFailsTheTurnAndReopensTheSession(t *testing.T) {
	client, session, task, version := stalledTaskFixture(t, a2a.TaskStateWorking)
	require.ErrorIs(t, client.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next"), ErrConflict)

	outcome, err := client.InterruptSessionTask(t.Context(), session.Id, string(task.ID), time.Now().Add(time.Hour), "turn interrupted: no progress for 1h0m0s")
	require.NoError(t, err)
	require.Equal(t, &SessionTaskInterruption{State: a2a.TaskStateFailed}, outcome)
	failed, err := client.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateFailed, failed.Status.State)
	require.Equal(t, a2a.MessageRoleAgent, failed.Status.Message.Role)
	require.Equal(t, a2a.NewTextPart("turn interrupted: no progress for 1h0m0s"), failed.Status.Message.Parts[0])
	require.Equal(t, task.ID, failed.Status.Message.TaskID)

	// A late save from the runtime that stalled finds a terminal task.
	late := *task
	late.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted}
	_, err = client.UpdateSessionTask(t.Context(), session.Id, version, taskMutationHash("late"), &late, &late, "")
	require.ErrorIs(t, err, ErrConflict, "the interruption advanced the version")
	_, current, err := client.GetVersionedSessionTask(t.Context(), session.Id, string(task.ID))
	require.NoError(t, err)
	_, err = client.UpdateSessionTask(t.Context(), session.Id, current, taskMutationHash("late"), &late, &late, "")
	require.ErrorIs(t, err, ErrFailedPrecondition)

	// The interruption is no idle boundary: nothing to pause or suspend.
	_, err = client.ClaimSessionQuiescence(t.Context(), 0, nil)
	require.ErrorIs(t, err, ErrNotFound)
	stalled, err := client.ListStalledSessionTasks(t.Context(), time.Now().Add(time.Hour), 100)
	require.NoError(t, err)
	require.Empty(t, stalled)
	require.NoError(t, client.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next"))
	_, err = client.InterruptSessionTask(t.Context(), session.Id, string(task.ID), time.Now().Add(time.Hour), "again")
	require.ErrorIs(t, err, ErrConflict)
}

func TestInterruptSessionTaskSettlesTheRuntimesOwnBoundary(t *testing.T) {
	client, session, task, version := stalledTaskFixture(t, a2a.TaskStateWorking)
	done := *task
	done.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted, Message: a2a.NewMessageForTask(a2a.MessageRoleAgent, task, a2a.NewTextPart("done"))}
	boundary, err := client.UpdateSessionTask(t.Context(), session.Id, version, taskMutationHash("complete"), &done, &done, "")
	require.NoError(t, err)
	stalled, err := client.ListStalledSessionTasks(t.Context(), time.Now().Add(time.Hour), 100)
	require.NoError(t, err)
	require.Len(t, stalled, 1, "an unsettled boundary keeps the projection working")

	outcome, err := client.InterruptSessionTask(t.Context(), session.Id, string(task.ID), time.Now().Add(time.Hour), "interrupted")
	require.NoError(t, err)
	require.Equal(t, &SessionTaskInterruption{State: a2a.TaskStateCompleted, Settled: true}, outcome)
	settled, err := client.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, settled.Status.State)
	require.Equal(t, a2a.NewTextPart("done"), settled.Status.Message.Parts[0])
	work, err := client.ClaimSessionQuiescence(t.Context(), 0, nil)
	require.NoError(t, err, "the runtime's boundary keeps its idle work")
	require.Equal(t, boundary, work.Version)
}

func TestInterruptSessionTaskRefusesASessionInALifecycleOperation(t *testing.T) {
	client, session, task, _ := stalledTaskFixture(t, a2a.TaskStateSubmitted)
	_, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	require.NoError(t, err)
	stalled, err := client.ListStalledSessionTasks(t.Context(), time.Now().Add(time.Hour), 100)
	require.NoError(t, err)
	require.Empty(t, stalled)
	_, err = client.InterruptSessionTask(t.Context(), session.Id, string(task.ID), time.Now().Add(time.Hour), "interrupted")
	require.ErrorIs(t, err, ErrConflict)
}
