package database

import (
	"sync"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

func TestNormalDeletionErrorsDuringActiveTurns(t *testing.T) {
	for _, state := range []a2a.TaskState{a2a.TaskStateSubmitted, a2a.TaskStateWorking} {
		t.Run("Given a "+string(state)+" turn, when deletion is requested, assert that admission stays open", func(t *testing.T) {
			client, session, _, _ := deletionTaskFixture(t, state)
			_, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
			require.ErrorIs(t, err, ErrConflict)
			current, err := client.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.State)
			require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE, current.Operation)
		})
	}
}

func TestNormalDeletionAllowsSettledTurns(t *testing.T) {
	for _, state := range []a2a.TaskState{a2a.TaskStateInputRequired, a2a.TaskStateAuthRequired, a2a.TaskStateCompleted, a2a.TaskStateFailed, a2a.TaskStateCanceled, a2a.TaskStateRejected} {
		t.Run("Given a settled "+string(state)+" turn, when deletion is requested, assert that it is admitted", func(t *testing.T) {
			client, session, task, version := deletionTaskFixture(t, state)
			require.NoError(t, client.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
			operation, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
			require.NoError(t, err)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETING, operation.Instance.State)
		})
	}
}

func TestForceDeletionFencesTurnsAndSurvivesPreparationFailure(t *testing.T) {
	client, session, task, version := deletionTaskFixture(t, a2a.TaskStateWorking)
	row, err := readSession(t.Context(), client.db, session.Id)
	require.NoError(t, err)
	operation, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, true)
	require.NoError(t, err)
	_, err = client.UpdateSessionTask(t.Context(), session.Id, version, taskMutationHash("late update"), task, task, "")
	require.ErrorIs(t, err, ErrConflict)
	require.ErrorIs(t, client.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "late input"), ErrConflict)
	current, err := client.GetSessionTask(t.Context(), session.Id, string(task.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateWorking, current.Status.State, "deletion intent does not prove execution stopped")
	_, err = client.FinishSessionOperation(t.Context(), session.Id, operation.ID, uuid.Nil, "", "", "read failed")
	require.NoError(t, err)
	// A new controller/client can continue durable deletion without the force flag.
	restarted := NewClient(client.db, "public")
	operation, err = restarted.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
	require.NoError(t, err)
	finishAdmittedDeletion(t, restarted, operation)
	stored, err := readSessionTask(t.Context(), client.db, row.HistoryID, string(task.ID))
	require.NoError(t, err)
	canceled, err := unmarshalSessionTask(stored.Data)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCanceled, canceled.Status.State)
	finalVersion, err := taskVersion(t.Context(), client.db, row.HistoryID, string(task.ID))
	require.NoError(t, err)
	require.Greater(t, finalVersion, version, "cancellation must be recorded in task history")
	joined, err := restarted.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETED, joined.Instance.State)
	unchangedVersion, err := taskVersion(t.Context(), client.db, row.HistoryID, string(task.ID))
	require.NoError(t, err)
	require.Equal(t, finalVersion, unchangedVersion)
	_, err = client.GetSessionByID(t.Context(), session.Id)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestForceDeletionPreservesSavedBoundary(t *testing.T) {
	for _, state := range []a2a.TaskState{a2a.TaskStateCompleted, a2a.TaskStateInputRequired} {
		t.Run("Given an unpublished "+string(state)+" boundary, when force deletion finishes, assert that its outcome is preserved", func(t *testing.T) {
			client, session, task, version := deletionTaskFixture(t, state)
			row, err := readSession(t.Context(), client.db, session.Id)
			require.NoError(t, err)
			_, err = client.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
			require.ErrorIs(t, err, ErrFailedPrecondition)
			operation, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, true)
			require.NoError(t, err)
			finishAdmittedDeletion(t, client, operation)
			stored, err := readSessionTask(t.Context(), client.db, row.HistoryID, string(task.ID))
			require.NoError(t, err)
			retained, err := unmarshalSessionTask(stored.Data)
			require.NoError(t, err)
			require.Equal(t, state, retained.Status.State)
			require.ErrorIs(t, client.SettleSessionTask(t.Context(), session.Id, string(task.ID), version), ErrNotFound)
		})
	}
}

func TestForceDeletionPreservesConcurrentRuntimeSettlement(t *testing.T) {
	client, session, task, version := deletionTaskFixture(t, a2a.TaskStateCompleted)
	row, err := readSession(t.Context(), client.db, session.Id)
	require.NoError(t, err)
	operation, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, true)
	require.NoError(t, err)

	// The runtime acknowledges its saved result while actor deletion is in progress.
	require.NoError(t, client.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
	finishAdmittedDeletion(t, client, operation)

	stored, err := readSessionTask(t.Context(), client.db, row.HistoryID, string(task.ID))
	require.NoError(t, err)
	completed, err := unmarshalSessionTask(stored.Data)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, completed.Status.State)
	finalVersion, err := taskVersion(t.Context(), client.db, row.HistoryID, string(task.ID))
	require.NoError(t, err)
	require.Equal(t, version, finalVersion, "deletion must not replace a settled outcome with cancellation")
}

func TestForceDeletionFinalizationRollsBackOnIdentityConflict(t *testing.T) {
	client, session, task, version := deletionTaskFixture(t, a2a.TaskStateWorking)
	operation, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, true)
	require.NoError(t, err)
	executor := uuid.New()
	claimed, err := client.ClaimSessionOperation(t.Context(), session.Id, operation.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)

	// This identity conflict occurs after task finalization inside the transaction.
	_, err = client.FinishSessionOperation(t.Context(), session.Id, operation.ID, executor, "", "wrong-actor-uid", "")
	require.ErrorIs(t, err, ErrConflict)
	current, err := client.GetSessionByID(t.Context(), session.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETING, current.State)
	working, err := client.GetSessionTask(t.Context(), session.Id, string(task.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateWorking, working.Status.State)
	row, err := readSession(t.Context(), client.db, session.Id)
	require.NoError(t, err)
	unchangedVersion, err := taskVersion(t.Context(), client.db, row.HistoryID, string(task.ID))
	require.NoError(t, err)
	require.Equal(t, version, unchangedVersion)

	deleted, err := client.FinishSessionOperation(t.Context(), session.Id, operation.ID, executor, "", "", "")
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETED, deleted.State)
	stored, err := readSessionTask(t.Context(), client.db, row.HistoryID, string(task.ID))
	require.NoError(t, err)
	canceled, err := unmarshalSessionTask(stored.Data)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCanceled, canceled.Status.State)
}

func TestForceDeletionRevokesDispatch(t *testing.T) {
	client, session := deletionSessionFixture(t)
	dispatchID := uuid.New()
	require.NoError(t, client.ReserveSessionDispatch(t.Context(), session.Id, dispatchID, "pending"))
	_, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
	require.ErrorIs(t, err, ErrFailedPrecondition)
	operation, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, true)
	require.NoError(t, err)
	late := newSessionTask(uuid.NewString(), "pending")
	late.ContextID = session.ContextId
	_, err = client.CreateRuntimeTask(t.Context(), session.Id, taskMutationHash("late acceptance"), late, dispatchID.String())
	require.ErrorIs(t, err, ErrConflict)
	finishAdmittedDeletion(t, client, operation)
}

func TestForceDeletionRetainsIdleLifecycleOwnership(t *testing.T) {
	client, session, task, version := deletionTaskFixture(t, a2a.TaskStateCompleted)
	require.NoError(t, client.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
	work, err := client.ClaimSessionQuiescence(t.Context())
	require.NoError(t, err)
	require.Equal(t, session.Id, work.Session.Id)
	_, err = client.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, true)
	require.ErrorIs(t, err, ErrConflict)
	current, err := client.GetSessionByID(t.Context(), session.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.State)
}

func TestForceDeletionSupersedesDispatchCleanup(t *testing.T) {
	// Given a waiting task whose abandoned dispatch has an issued cleanup claim.
	client, session, task, version := deletionTaskFixture(t, a2a.TaskStateInputRequired)
	ctx := t.Context()
	require.NoError(t, client.SettleSessionTask(ctx, session.Id, string(task.ID), version))
	candidate := abandonedDispatchFixture(t, client, session)
	work, err := client.ClaimSessionDispatchCleanup(ctx, candidate)
	require.NoError(t, err)
	_, err = client.BeginSessionOperation(ctx, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
	require.ErrorIs(t, err, ErrFailedPrecondition, "ordinary deletion must still wait for issued cleanup")

	// When force deletion is admitted, it retires cleanup while permanently
	// closing admission. A stalled cleanup owner cannot keep deletion blocked.
	operation, err := client.BeginSessionOperation(ctx, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, true)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETING, operation.Instance.State)
	_, err = client.GetPendingSessionDispatchCleanup(ctx, "")
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.ClaimSessionDispatchCleanup(ctx, candidate)
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, client.ReleaseSessionDispatchCleanup(ctx, work), ErrConflict)
	require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()), "retired cleanup completion must do nothing")
	require.ErrorIs(t, client.ReserveSessionDispatch(ctx, session.Id, uuid.New(), "late-input"), ErrConflict)
	for _, token := range []string{work.DispatchID.String(), ""} {
		_, err = client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("late update"), task, task, token)
		require.ErrorIs(t, err, ErrConflict)
	}
	row, err := readSession(ctx, client.db, session.Id)
	require.NoError(t, err)
	stored, err := readSessionTask(ctx, client.db, row.HistoryID, string(task.ID))
	require.NoError(t, err)
	require.Nil(t, stored.SnapshotURI, "late cleanup must not record a snapshot after deletion took ownership")
	current, err := client.GetSessionOperation(ctx, session.Id, operation.ID)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, current.Instance.Operation)

	// A failed preparation keeps DELETING and a new deletion generation. Late
	// cleanup callbacks cannot release its fence or replace that operation.
	_, err = client.FinishSessionOperation(ctx, session.Id, operation.ID, uuid.Nil, "", "", "preparation failed")
	require.NoError(t, err)
	retry, err := client.BeginSessionOperation(ctx, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
	require.NoError(t, err)
	require.NotEqual(t, operation.ID, retry.ID)
	require.ErrorIs(t, client.ReleaseSessionDispatchCleanup(ctx, work), ErrConflict)
	require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()))
	current, err = client.GetSessionOperation(ctx, session.Id, retry.ID)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETING, current.Instance.State)
	require.ErrorIs(t, client.ReserveSessionDispatch(ctx, session.Id, uuid.New(), "late-input"), ErrConflict)
	finishAdmittedDeletion(t, client, retry)
	require.ErrorIs(t, client.ReleaseSessionDispatchCleanup(ctx, work), ErrConflict)
	require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()))
	deleted, err := client.BeginSessionOperation(ctx, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETED, deleted.Instance.State)
	require.ErrorIs(t, client.ReserveSessionDispatch(ctx, session.Id, uuid.New(), "late-input"), ErrConflict)
}

func TestDeletionSerializesWithMessageAdmission(t *testing.T) {
	client, session := deletionSessionFixture(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	workers.Go(func() {
		<-start
		_, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, false)
		results <- err
	})
	workers.Go(func() {
		<-start
		results <- client.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "concurrent input")
	})
	close(start)
	workers.Wait()
	first, second := <-results, <-results
	require.NotEqual(t, first == nil, second == nil, "only deletion or dispatch may be admitted")
}

func deletionTaskFixture(t *testing.T, state a2a.TaskState) (*Client, *apiv1.Session, *a2a.Task, int64) {
	t.Helper()
	client, session := deletionSessionFixture(t)
	task := newSessionTask(uuid.NewString(), "input")
	task.ContextID, task.Status.State = session.ContextId, state
	version, err := client.CreateRuntimeTask(t.Context(), session.Id, taskMutationHash("initial turn"), task, "")
	require.NoError(t, err)
	return client, session, task, version
}

func deletionSessionFixture(t *testing.T) (*Client, *apiv1.Session) {
	t.Helper()
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, _, err := client.CreateSession(t.Context(), newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	session, err = markSessionReady(t.Context(), client, session.Id, "runtime.example")
	require.NoError(t, err)
	return client, session
}

func finishAdmittedDeletion(t *testing.T, client *Client, operation *SessionOperation) {
	t.Helper()
	executor := uuid.New()
	claimed, err := client.ClaimSessionOperation(t.Context(), operation.Instance.Id, operation.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	finished, err := client.FinishSessionOperation(t.Context(), operation.Instance.Id, operation.ID, executor, "", "", "")
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETED, finished.State)
}
