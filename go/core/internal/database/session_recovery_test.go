package database

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

func TestCrashedRecoverySettlesTasksAtomically(t *testing.T) {
	for _, test := range []struct {
		name                string
		state, want         a2a.TaskState
		published, captured bool
	}{
		{name: "submitted", state: a2a.TaskStateSubmitted, want: a2a.TaskStateFailed},
		{name: "working", state: a2a.TaskStateWorking, want: a2a.TaskStateFailed},
		{name: "completed before publication", state: a2a.TaskStateCompleted, want: a2a.TaskStateCompleted},
		{name: "unpublished input wait", state: a2a.TaskStateInputRequired, want: a2a.TaskStateFailed},
		{name: "unpublished auth wait", state: a2a.TaskStateAuthRequired, want: a2a.TaskStateFailed},
		{name: "published input wait", state: a2a.TaskStateInputRequired, want: a2a.TaskStateFailed, published: true},
		{name: "published auth wait", state: a2a.TaskStateAuthRequired, want: a2a.TaskStateFailed, published: true},
		{name: "captured input wait", state: a2a.TaskStateInputRequired, want: a2a.TaskStateInputRequired, published: true, captured: true},
		{name: "captured auth wait", state: a2a.TaskStateAuthRequired, want: a2a.TaskStateAuthRequired, published: true, captured: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient(setupTestDB(t), "public")
			sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
			session, _, err := client.CreateSession(t.Context(), newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
			require.NoError(t, err)
			session, err = markSessionReady(t.Context(), client, session.Id, "runtime.example")
			require.NoError(t, err)
			task := newSessionTask(uuid.NewString(), "original input")
			task.ContextID, task.Status.State = session.ContextId, test.state
			version, err := client.CreateRuntimeTask(t.Context(), session.Id, taskMutationHash("create"), task, "")
			require.NoError(t, err)
			if test.published {
				require.NoError(t, client.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
			}
			snapshot := SessionTaskSnapshot{}
			if test.captured {
				work, err := client.ClaimSessionQuiescence(t.Context())
				require.NoError(t, err)
				snapshot = SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/captured", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}
				require.NoError(t, client.FinishSessionQuiescence(t.Context(), work, &snapshot))
			}
			before, _, err := client.GetVersionedSessionTask(t.Context(), session.Id, string(task.ID))
			require.NoError(t, err)
			page, err := client.ListSessionRecoveryCandidates(t.Context(), "", 100)
			require.NoError(t, err)
			require.Empty(t, page)

			operation, err := client.BeginCrashedSessionRecovery(t.Context(), session.Id, "actor-"+session.Id, snapshot)
			require.NoError(t, err)
			require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_FAILED, operation.Instance.State)
			require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME, operation.Instance.Operation)
			require.Equal(t, SessionRuntimeCrashed, operation.Instance.Failure.Reason)
			stored, err := client.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
			require.NoError(t, err)
			require.Equal(t, test.want, stored.Status.State)
			require.Equal(t, before.History, stored.History)
			require.Equal(t, before.Artifacts, stored.Artifacts)
			if test.want == test.state {
				require.Equal(t, before.Status, stored.Status)
			}
			_, settledVersion, err := client.GetVersionedSessionTask(t.Context(), session.Id, string(task.ID))
			require.NoError(t, err)
			replayed, err := client.BeginCrashedSessionRecovery(t.Context(), session.Id, "actor-"+session.Id, snapshot)
			require.NoError(t, err)
			require.Equal(t, operation.ID, replayed.ID)
			_, replayedVersion, err := client.GetVersionedSessionTask(t.Context(), session.Id, string(task.ID))
			require.NoError(t, err)
			require.Equal(t, settledVersion, replayedVersion)
			_, err = client.UpdateSessionTask(t.Context(), session.Id, version, taskMutationHash("late"), task, task, "")
			if test.state.Terminal() {
				require.ErrorIs(t, err, ErrFailedPrecondition)
			} else {
				require.ErrorIs(t, err, ErrConflict)
			}
			require.ErrorIs(t, client.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "next"), ErrConflict)
			_, _, err = client.ReserveSessionCheckpoint(t.Context(), &apiv1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID)}, session.Creator, uuid.NewString())
			require.ErrorIs(t, err, ErrConflict)
			page, err = client.ListSessionRecoveryCandidates(t.Context(), "", 100)
			require.NoError(t, err)
			require.Len(t, page, 1)
			require.Equal(t, session.Id, page[0].Id)
		})
	}
}

func TestCrashedRecoveryAdmissionRespectsFences(t *testing.T) {
	for _, test := range []struct {
		name                               string
		actorUID                           string
		checkpoint, claimedIdle, lifecycle bool
	}{
		{name: "replacement actor", actorUID: "replacement"},
		{name: "checkpoint", checkpoint: true},
		{name: "claimed idle work", claimedIdle: true},
		{name: "lifecycle", lifecycle: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient(setupTestDB(t), "public")
			sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
			session, task := waitingTaskFixture(t, client)
			uid := "actor-" + session.Id
			switch {
			case test.actorUID != "":
				uid = test.actorUID
			case test.checkpoint:
				task.Status.State = a2a.TaskStateCompleted
				require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, &SessionTaskSnapshot{
					Atespace: "team-a", URI: "s3://snapshots/completed", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES",
				}))
				_, _, err := client.ReserveSessionCheckpoint(t.Context(), &apiv1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID)}, session.Creator, uuid.NewString())
				require.NoError(t, err)
			case test.claimedIdle:
				_, version, err := client.GetVersionedSessionTask(t.Context(), session.Id, string(task.ID))
				require.NoError(t, err)
				for _, state := range []a2a.TaskState{a2a.TaskStateWorking, a2a.TaskStateInputRequired} {
					task.Status.State = state
					version, err = client.UpdateSessionTask(t.Context(), session.Id, version, taskMutationHash(string(state)), task, task, "")
					require.NoError(t, err)
				}
				require.NoError(t, client.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
				_, err = client.ClaimSessionQuiescence(t.Context())
				require.NoError(t, err)
			case test.lifecycle:
				_, err := client.BeginSessionOperation(t.Context(), session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND)
				require.NoError(t, err)
			}
			before, err := client.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			_, err = client.BeginCrashedSessionRecovery(t.Context(), session.Id, uid, SessionTaskSnapshot{})
			require.ErrorIs(t, err, ErrConflict)
			after, err := client.GetSessionByID(t.Context(), session.Id)
			require.NoError(t, err)
			require.True(t, proto.Equal(before, after))
		})
	}
}

func TestCrashedRecoveryAdmissionRollsBackOnMalformedTask(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, task := waitingTaskFixture(t, client)
	_, version, err := client.GetVersionedSessionTask(t.Context(), session.Id, string(task.ID))
	require.NoError(t, err)
	for _, state := range []a2a.TaskState{a2a.TaskStateWorking, a2a.TaskStateInputRequired} {
		task.Status.State = state
		version, err = client.UpdateSessionTask(t.Context(), session.Id, version, taskMutationHash(string(state)), task, task, "")
		require.NoError(t, err)
	}
	require.NoError(t, client.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
	row, err := readSession(t.Context(), client.db, session.Id)
	require.NoError(t, err)
	before, err := readSessionTask(t.Context(), client.db, row.HistoryID, string(task.ID))
	require.NoError(t, err)
	// A corrupt projection makes settlement fail after the lifecycle write.
	_, err = client.db.Exec(t.Context(), `UPDATE session_task SET data = $2 WHERE history_id = $1 AND id = $3`, row.HistoryID, []byte{0xff}, string(task.ID))
	require.NoError(t, err)
	_, err = client.BeginCrashedSessionRecovery(t.Context(), session.Id, "actor-"+session.Id, SessionTaskSnapshot{})
	require.Error(t, err)
	current, err := client.GetSessionByID(t.Context(), session.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.State)
	require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE, current.Operation)
	require.Nil(t, current.Failure)
	_, err = client.db.Exec(t.Context(), `UPDATE session_task SET data = $2 WHERE history_id = $1 AND id = $3`, row.HistoryID, before.Data, string(task.ID))
	require.NoError(t, err)
	work, err := client.ClaimSessionQuiescence(t.Context())
	require.NoError(t, err)
	require.Equal(t, string(task.ID), work.TaskID)
}
