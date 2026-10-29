package session

import (
	"crypto/sha256"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

func TestCrashedRecoveryMarksOnlyDiscardedMessagesInOneTask(t *testing.T) {
	store, actors, session, task, version := crashedTurnFixture(t)
	task.Status.State = a2a.TaskStateInputRequired
	hash := sha256.Sum256([]byte("captured question"))
	version, err := store.UpdateSessionTask(t.Context(), session.Id, version, hash[:], task, task, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(task.ID), version))
	captureRecoveryMessageBoundary(t, store, actors, session)

	answer := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("continue"))
	answer.TaskID, answer.ContextID = task.ID, session.ContextId
	reply := a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("done"))
	reply.TaskID, reply.ContextID = task.ID, session.ContextId
	// Repeated history must not move a captured input past the snapshot boundary.
	task.History = append(task.History, answer, reply)
	task.Status.State = a2a.TaskStateCompleted
	hash = sha256.Sum256([]byte("unpublished completed answer"))
	_, err = store.UpdateSessionTask(t.Context(), session.Id, version, hash[:], task, task, "")
	require.NoError(t, err)
	before, _, err := store.GetVersionedSessionTask(t.Context(), session.Id, string(task.ID))
	require.NoError(t, err)

	recovered, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute, false).Revert(t.Context(), session)
	require.NoError(t, err)
	require.Equal(t, [][2]string{{string(task.ID), answer.ID}}, recoveryMessageIdentities(recovered))
	stored, err := store.GetSettledSessionTask(t.Context(), session.Id, string(task.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, stored.Status.State)
	require.Equal(t, before.Status, stored.Status)
	require.Equal(t, before.History, stored.History, "publishing recovery retains the completed turn's full history")
	require.Equal(t, before.Artifacts, stored.Artifacts)
	current, err := store.GetSessionByID(t.Context(), session.Id)
	require.NoError(t, err)
	require.Equal(t, recoveryMessageIdentities(recovered), recoveryMessageIdentities(current), "delivery failures survive a fresh Session read")
}

func TestCrashedRecoveryScopesMessageFailuresToTheirTask(t *testing.T) {
	store, actors, session, retained, version := crashedTurnFixture(t)
	retained.Status.State = a2a.TaskStateCompleted
	hash := sha256.Sum256([]byte("retained completed turn"))
	version, err := store.UpdateSessionTask(t.Context(), session.Id, version, hash[:], retained, retained, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(retained.ID), version))
	captureRecoveryMessageBoundary(t, store, actors, session)
	beforeRetained, err := store.GetSettledSessionTask(t.Context(), session.Id, string(retained.ID), nil)
	require.NoError(t, err)

	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("a different turn"))
	message.ID, message.ContextID = retained.History[0].ID, session.ContextId
	discarded := a2a.NewSubmittedTask(message, message)
	discarded.Status.State = a2a.TaskStateCompleted
	hash = sha256.Sum256([]byte("completed without a newer snapshot"))
	version, err = store.CreateRuntimeTask(t.Context(), session.Id, hash[:], discarded, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(discarded.ID), version))
	beforeDiscarded, err := store.GetSettledSessionTask(t.Context(), session.Id, string(discarded.ID), nil)
	require.NoError(t, err)

	workflow := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute, false)
	recovered, err := workflow.Revert(t.Context(), session)
	require.NoError(t, err)
	want := [][2]string{{string(discarded.ID), message.ID}}
	require.Equal(t, want, recoveryMessageIdentities(recovered))
	for _, before := range []*a2a.Task{beforeRetained, beforeDiscarded} {
		after, err := store.GetSettledSessionTask(t.Context(), session.Id, string(before.ID), nil)
		require.NoError(t, err)
		require.Equal(t, before, after, "recovery does not rewrite completed A2A tasks or their history")
	}

	// Reverting the same runtime interval must not duplicate delivery failures.
	actors.actors[actorKey("team-a", substrate.ActorName(session.Id))].Status.State = ateapipb.ActorState_ACTOR_STATE_CRASHED
	recovered, err = workflow.Revert(t.Context(), recovered)
	require.NoError(t, err)
	require.Equal(t, want, recoveryMessageIdentities(recovered))

	message = a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("a retained new turn"))
	message.ContextID = session.ContextId
	later := a2a.NewSubmittedTask(message, message)
	later.Status.State = a2a.TaskStateCompleted
	hash = sha256.Sum256([]byte("completed after recovery"))
	version, err = store.CreateRuntimeTask(t.Context(), session.Id, hash[:], later, "")
	require.NoError(t, err)
	require.NoError(t, store.SettleSessionTask(t.Context(), session.Id, string(later.ID), version))
	captureRecoveryMessageBoundary(t, store, actors, session)

	message = a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("another lost input"))
	message.ContextID = session.ContextId
	lost := a2a.NewSubmittedTask(message, message)
	hash = sha256.Sum256([]byte("new interrupted turn"))
	_, err = store.CreateRuntimeTask(t.Context(), session.Id, hash[:], lost, "")
	require.NoError(t, err)
	actors.actors[actorKey("team-a", substrate.ActorName(session.Id))].Status.State = ateapipb.ActorState_ACTOR_STATE_CRASHED
	recovered, err = workflow.Revert(t.Context(), recovered)
	require.NoError(t, err)
	want = append(want, [2]string{string(lost.ID), message.ID})
	require.Equal(t, want, recoveryMessageIdentities(recovered), "a newer rollback boundary does not erase earlier delivery failures")
}

func TestCrashedRecoveryWithoutSnapshotMarksAllInputs(t *testing.T) {
	store, actors, session, task, _ := crashedTurnFixture(t)
	recovered, err := NewActorWorkflow(store, actors, make(chan struct{}, 1), time.Minute, false).Revert(t.Context(), session)
	require.NoError(t, err)
	require.Equal(t, [][2]string{{string(task.ID), task.History[0].ID}}, recoveryMessageIdentities(recovered))
}

func captureRecoveryMessageBoundary(t *testing.T, store *lifecycleTestStore, actors *lifecycleTestActors, session *apiv1.Session) {
	t.Helper()
	work, err := store.ClaimSessionQuiescence(t.Context())
	require.NoError(t, err)
	require.Equal(t, session.Id, work.Session.Id)
	snapshotURI := "s3://snapshots/" + uuid.NewString()
	require.NoError(t, store.FinishSessionQuiescence(t.Context(), work, &database.SessionTaskSnapshot{
		Atespace: "team-a", URI: snapshotURI, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES.String(),
	}))
	actors.actors[actorKey("team-a", substrate.ActorName(session.Id))].Status.Snapshots = []*ateapipb.Snapshot{{
		Generation: 1, Fidelity: ateapipb.SnapshotFidelity_SNAPSHOT_FIDELITY_VOLUMES,
		DurableSnapshot: &ateapipb.SnapshotStorage{
			Status: ateapipb.SnapshotStorageStatus_SNAPSHOT_STORAGE_STATUS_COMPLETED,
			Object: &ateapipb.ObjectSnapshot{SnapshotUri: snapshotURI},
		},
	}}
}

func recoveryMessageIdentities(session *apiv1.Session) [][2]string {
	result := make([][2]string, 0, len(session.GetRecovery().GetFailedMessages()))
	for _, message := range session.GetRecovery().GetFailedMessages() {
		result = append(result, [2]string{message.TaskId, message.MessageId})
	}
	return result
}
