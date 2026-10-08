package database

import (
	"testing"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
)

func TestSessionOperationGenerationAndTombstone(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, _, err := client.CreateSession(t.Context(), newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	create, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE, create.Instance.Operation)
	creator := uuid.New()
	claimed, err := client.ClaimSessionOperation(t.Context(), session.Id, create.ID, creator)
	require.NoError(t, err)
	require.True(t, claimed)
	ready, err := client.FinishSessionOperation(t.Context(), session.Id, create.ID, creator, "runtime.example", "actor-uid", "")
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, ready.State)
	_, err = client.GetSessionForRuntime(t.Context(), session.Id, "actor-uid")
	require.NoError(t, err)
	_, err = client.GetSessionForRuntime(t.Context(), session.Id, "replacement-uid")
	require.ErrorIs(t, err, ErrNotFound)

	suspend, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND, suspend.Instance.Operation)
	executor := uuid.New()
	claimed, err = client.ClaimSessionOperation(t.Context(), session.Id, suspend.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = client.UpdateSessionName(t.Context(), session.Id, "alice", "renamed during suspend")
	require.NoError(t, err)
	suspended, err := client.FinishSessionOperation(t.Context(), session.Id, suspend.ID, executor, "", "", "")
	require.NoError(t, err)
	require.Equal(t, "renamed during suspend", suspended.Name)

	resume, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME, resume.Instance.Operation)
	resumer := uuid.New()
	claimed, err = client.ClaimSessionOperation(t.Context(), session.Id, resume.ID, resumer)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = client.FinishSessionOperation(t.Context(), session.Id, resume.ID, resumer, "", "replacement-uid", "")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.FinishSessionOperation(t.Context(), session.Id, resume.ID, resumer, "", "", "")
	require.NoError(t, err)
	// The lifecycle state is READY again; that does not restore the old authority.
	_, err = client.FinishSessionOperation(t.Context(), session.Id, create.ID, creator, "obsolete.example", "actor-uid", "")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.GetSessionOperation(t.Context(), session.Id, suspend.ID)
	require.ErrorIs(t, err, ErrConflict)

	deletion, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE, deletion.Instance.Operation)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETING, deletion.Instance.State)
	deleter := uuid.New()
	claimed, err = client.ClaimSessionOperation(t.Context(), session.Id, deletion.ID, deleter)
	require.NoError(t, err)
	require.True(t, claimed)
	deleted, err := client.FinishSessionOperation(t.Context(), session.Id, deletion.ID, deleter, "", "", "")
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETED, deleted.State)
	require.Empty(t, deleted.PreparedRevision)
	_, err = client.GetSessionByID(t.Context(), session.Id)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.GetSessionOperation(t.Context(), session.Id, create.ID)
	require.ErrorIs(t, err, ErrConflict)
	current, err := client.GetSessionOperation(t.Context(), session.Id, deletion.ID)
	require.NoError(t, err)
	require.Equal(t, deleted.State, current.Instance.State)
	current, err = client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	require.NoError(t, err)
	require.Equal(t, deletion.ID, current.ID)
	require.Equal(t, deleted.State, current.Instance.State)
}

func TestSessionOperationClaimsAndPreparationRecovery(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, _, err := client.CreateSession(t.Context(), newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	session, err = markSessionReady(t.Context(), client, session.Id, "runtime.example")
	require.NoError(t, err)
	first, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND)
	require.NoError(t, err)
	// Success requires a claim, even when there is no competing executor.
	_, err = client.FinishSessionOperation(t.Context(), session.Id, first.ID, uuid.Nil, "", "", "")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.FinishSessionOperation(t.Context(), session.Id, first.ID, uuid.Nil, "", "", "preparation unavailable")
	require.NoError(t, err)
	second, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	claimed, err := client.ClaimSessionOperation(t.Context(), session.Id, first.ID, uuid.New())
	require.NoError(t, err)
	require.False(t, claimed)
	_, err = client.FinishSessionOperation(t.Context(), session.Id, first.ID, uuid.Nil, "", "", "late failure")
	require.ErrorIs(t, err, ErrConflict)

	type outcome struct {
		executor uuid.UUID
		claimed  bool
		err      error
	}
	results := make(chan outcome, 8)
	for range cap(results) {
		go func() {
			executor := uuid.New()
			claimed, err := client.ClaimSessionOperation(t.Context(), session.Id, second.ID, executor)
			results <- outcome{executor, claimed, err}
		}()
	}
	var winner uuid.UUID
	for range cap(results) {
		result := <-results
		require.NoError(t, result.err)
		if result.claimed {
			require.Equal(t, uuid.Nil, winner, "only one replica may authorize runtime work")
			winner = result.executor
		}
	}
	require.NotEqual(t, uuid.Nil, winner)
	joined, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND)
	require.NoError(t, err)
	require.Equal(t, winner, joined.ExecutorID)
	require.Equal(t, second.Instance.Operation, joined.Instance.Operation)
	_, err = client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.FinishSessionOperation(t.Context(), session.Id, second.ID, uuid.New(), "", "", "")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.FinishSessionOperation(t.Context(), session.Id, second.ID, uuid.Nil, "", "", "cannot release issued work")
	require.ErrorIs(t, err, ErrConflict)
	// Even the claiming executor cannot release possibly issued work as a failure.
	_, err = client.FinishSessionOperation(t.Context(), session.Id, second.ID, winner, "", "", "runtime outcome unknown")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.FinishSessionOperation(t.Context(), session.Id, second.ID, winner, "", "", "")
	require.NoError(t, err)
}

func TestDeleteSupersedesOnlyUnissuedCreation(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, _, err := client.CreateSession(t.Context(), newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	creation, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
	require.NoError(t, err)
	deletion, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	require.NoError(t, err)
	claimed, err := client.ClaimSessionOperation(t.Context(), session.Id, creation.ID, uuid.New())
	require.NoError(t, err)
	require.False(t, claimed)
	_, err = client.GetSessionOperation(t.Context(), session.Id, creation.ID)
	require.ErrorIs(t, err, ErrConflict)
	executor := uuid.New()
	claimed, err = client.ClaimSessionOperation(t.Context(), session.Id, deletion.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = client.FinishSessionOperation(t.Context(), session.Id, deletion.ID, executor, "", "", "")
	require.NoError(t, err)
	_, err = client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.GetSessionOperation(t.Context(), session.Id, uuid.New())
	require.ErrorIs(t, err, ErrConflict)
}

func TestLifecycleABARejectsOldClaimAndCompletion(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, _, err := client.CreateSession(t.Context(), newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	_, err = markSessionReady(t.Context(), client, session.Id, "runtime.example")
	require.NoError(t, err)
	var first *SessionOperation
	executor := uuid.New()
	for _, kind := range []apiv1alpha1.RuntimeOperation{
		apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND,
		apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME,
	} {
		operation, err := client.BeginSessionOperation(t.Context(), session.Id, kind)
		require.NoError(t, err)
		if first == nil {
			first = operation
		}
		claimed, err := client.ClaimSessionOperation(t.Context(), session.Id, operation.ID, executor)
		require.NoError(t, err)
		require.True(t, claimed)
		_, err = client.FinishSessionOperation(t.Context(), session.Id, operation.ID, executor, "", "", "")
		require.NoError(t, err)
	}
	later, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, later.ID)
	require.Equal(t, first.Instance.State, later.Instance.State)
	require.Equal(t, first.Instance.Operation, later.Instance.Operation)
	claimed, err := client.ClaimSessionOperation(t.Context(), session.Id, first.ID, uuid.New())
	require.NoError(t, err)
	require.False(t, claimed)
	_, err = client.FinishSessionOperation(t.Context(), session.Id, first.ID, executor, "", "", "")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.GetSessionOperation(t.Context(), session.Id, first.ID)
	require.ErrorIs(t, err, ErrConflict)
	claimed, err = client.ClaimSessionOperation(t.Context(), session.Id, later.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
}

func TestLifecycleObservationUsesCurrentFields(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, _, err := client.CreateSession(t.Context(), newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	operation, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
	require.NoError(t, err)
	executor := uuid.New()
	claimed, err := client.ClaimSessionOperation(t.Context(), session.Id, operation.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = client.FinishSessionOperation(t.Context(), session.Id, operation.ID, executor, "runtime.example", "actor-uid", "")
	require.NoError(t, err)
	renamed, err := client.UpdateSessionName(t.Context(), session.Id, "alice", "current name")
	require.NoError(t, err)
	current, err := client.GetSessionOperation(t.Context(), session.Id, operation.ID)
	require.NoError(t, err)
	require.Equal(t, renamed.Name, current.Instance.Name)
	// Already at target is successful without needing a previous Resume receipt.
	resumed, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME)
	require.NoError(t, err)
	require.Equal(t, renamed.Name, resumed.Instance.Name)
	require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE, resumed.Instance.Operation)
	claimed, err = client.ClaimSessionOperation(t.Context(), session.Id, operation.ID, uuid.New())
	require.NoError(t, err)
	require.False(t, claimed, "completion must not become executable when its claim is cleared")
}

func TestFailSessionRecordsRuntimeLoss(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, _, err := client.CreateSession(t.Context(), newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	failure := &apiv1alpha1.Failure{Reason: "RuntimeLost", Message: "runtime lost: Actor team-a/session-1 crashed; start a new conversation"}
	_, err = client.FailSession(t.Context(), session.Id, failure)
	require.ErrorIs(t, err, ErrConflict, "a session still being created is not READY")
	_, err = client.FailSession(t.Context(), uuid.NewString(), failure)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.FailSession(t.Context(), session.Id, &apiv1alpha1.Failure{Reason: "RuntimeLost"})
	require.ErrorIs(t, err, ErrFailedPrecondition)
	_, err = markSessionReady(t.Context(), client, session.Id, "runtime.example")
	require.NoError(t, err)

	failed, err := client.FailSession(t.Context(), session.Id, failure)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_FAILED, failed.State)
	require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE, failed.Operation)
	require.Equal(t, failure.Reason, failed.GetFailure().GetReason())
	require.Equal(t, failure.Message, failed.GetFailure().GetMessage())
	stored, err := client.GetSessionByID(t.Context(), session.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_FAILED, stored.State)
	require.Equal(t, failure.Message, stored.GetFailure().GetMessage())

	again, err := client.FailSession(t.Context(), session.Id, &apiv1alpha1.Failure{Reason: "RuntimeLost", Message: "runtime lost: later"})
	require.NoError(t, err)
	require.Equal(t, failure.Message, again.GetFailure().GetMessage(), "the first recorded loss stands")
	_, err = client.FailSession(t.Context(), session.Id, &apiv1alpha1.Failure{Reason: "Other", Message: "other"})
	require.ErrorIs(t, err, ErrConflict)

	_, err = client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND)
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME)
	require.ErrorIs(t, err, ErrConflict)
	deletion, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	require.NoError(t, err, "a failed session stays deletable")
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETING, deletion.Instance.State)
}

// A session that fails while a worker holds its idle boundary, because the
// runtime the worker was pausing crashed, releases the claim with the failure:
// nothing settles it for a runtime that is gone, and a held claim would refuse
// the delete for good. The holder's late outcome records nothing.
func TestFailSessionReleasesHeldIdleWork(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	session, _ := completedBoundaryFixture(t, client)
	work, err := client.ClaimSessionQuiescence(t.Context(), testClaimLease, 0, nil)
	require.NoError(t, err)
	_, err = client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	require.ErrorIs(t, err, ErrFailedPrecondition, "a held claim refuses the delete of a ready session")

	failure := &apiv1alpha1.Failure{Reason: "RuntimeLost", Message: "runtime lost: Actor team-a/session-1 crashed; start a new conversation"}
	failed, err := client.FailSession(t.Context(), session.Id, failure)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_FAILED, failed.State)
	require.ErrorIs(t, client.RenewSessionQuiescence(t.Context(), work, testClaimLease), ErrNotFound, "the claim is released")
	require.NoError(t, client.ReleaseSessionQuiescence(t.Context(), work, testQuiescenceFailure), "the holder's late release records nothing")
	snapshot := &SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshot/late", ContentScope: "DATA"}
	require.NoError(t, client.FinishSessionQuiescence(t.Context(), work, snapshot), "the holder's late finish records nothing")
	_, err = client.ClaimSessionQuiescence(t.Context(), testClaimLease, 0, nil)
	require.ErrorIs(t, err, ErrNotFound, "a failed session's boundary is not claimed again")

	deletion, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	require.NoError(t, err, "the failed session is deletable at once")
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_DELETING, deletion.Instance.State)
}
