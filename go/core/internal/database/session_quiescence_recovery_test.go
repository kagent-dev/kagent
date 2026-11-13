package database

import (
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// claimedQuiescence drives a session to a settled waiting task and claims the
// idle suspension that follows it, which is the state a worker holds while it
// suspends the Actor.
func claimedQuiescence(t *testing.T, client *Client) (string, *SessionQuiescence) {
	t.Helper()
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
	work, err := client.ClaimSessionQuiescence(t.Context())
	require.NoError(t, err)
	return session.Id, work
}

// A suspension whose outcome never came back keeps its claim, which closes the
// session to everything else. Nothing retried it, so the claim has to be
// reachable again before its session can be used, cancelled or deleted.
func TestClaimStaleSessionQuiescenceTakesOverAnUnrecordedClaim(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionID, work := claimedQuiescence(t, client)

	_, err := client.ClaimSessionQuiescence(t.Context())
	require.ErrorIs(t, err, ErrNotFound, "a claimed suspension is never reassigned by the ordinary path")
	_, err = client.ClaimStaleSessionQuiescence(t.Context(), time.Hour)
	require.ErrorIs(t, err, ErrNotFound, "a claim younger than the age is left with its worker")

	taken, err := client.ClaimStaleSessionQuiescence(t.Context(), 0)
	require.NoError(t, err)
	require.Equal(t, work.Version, taken.Version)
	require.Equal(t, work.TaskID, taken.TaskID)
	require.Equal(t, sessionID, taken.Session.Id)
	require.NotEqual(t, work.ExecutorID, taken.ExecutorID, "the previous claim is invalidated")

	// The first worker comes back after all. Its claim no longer exists, so its
	// snapshot is not recorded over the one being reconciled.
	late := SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/late", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}
	require.ErrorIs(t, client.FinishSessionQuiescence(t.Context(), work, &late), ErrNotFound)

	settled := SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/reconciled", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}
	require.NoError(t, client.FinishSessionQuiescence(t.Context(), taken, &settled))
	row, err := readSession(t.Context(), client.db, sessionID)
	require.NoError(t, err)
	recorded, err := queryOne(t.Context(), client.db, `
		SELECT snapshot_uri FROM session_task WHERE history_id = $1 AND id = $2
	`, pgx.RowTo[string], row.HistoryID, taken.TaskID)
	require.NoError(t, err)
	require.Equal(t, settled.URI, recorded)
	_, err = client.ClaimStaleSessionQuiescence(t.Context(), 0)
	require.ErrorIs(t, err, ErrNotFound, "a finished boundary is no longer stranded")
}

// When Substrate says the suspension never happened, the claim is given up
// rather than held. The session has to be usable again immediately: that is
// what makes answering, cancelling and deleting it possible.
func TestReleaseSessionQuiescenceAdmitsTheSessionAgain(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionID, work := claimedQuiescence(t, client)
	next := newSessionTask(uuid.NewString(), "next input")
	next.ContextID, next.Status.State = "context-"+sessionID, a2a.TaskStateWorking

	taken, err := client.ClaimStaleSessionQuiescence(t.Context(), 0)
	require.NoError(t, err)
	require.ErrorIs(t, client.ReleaseSessionQuiescence(t.Context(), work), ErrNotFound, "a superseded claim releases nothing")
	require.NoError(t, client.ReleaseSessionQuiescence(t.Context(), taken))

	row, err := readSession(t.Context(), client.db, sessionID)
	require.NoError(t, err)
	require.NoError(t, requireSettledRuntime(t.Context(), client.db, row.HistoryID, ""), "the session is admitted again")

	// The suspension itself is still owed, so an ordinary worker can retry it.
	retried, err := client.ClaimSessionQuiescence(t.Context())
	require.NoError(t, err)
	require.Equal(t, work.Version, retried.Version)
}

func TestReleaseSessionQuiescenceRequiresAClaim(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	require.Error(t, client.ReleaseSessionQuiescence(t.Context(), nil))
	require.Error(t, client.ReleaseSessionQuiescence(t.Context(), &SessionQuiescence{}))
	require.Error(t, client.RetireSessionQuiescence(t.Context(), nil))
}

// There is no Actor left to suspend, so the boundary ends rather than waiting
// for work that can never succeed. The session is usable and deletable again.
func TestRetireSessionQuiescenceEndsTheBoundary(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionID, _ := claimedQuiescence(t, client)

	taken, err := client.ClaimStaleSessionQuiescence(t.Context(), 0)
	require.NoError(t, err)
	require.NoError(t, client.RetireSessionQuiescence(t.Context(), taken))

	row, err := readSession(t.Context(), client.db, sessionID)
	require.NoError(t, err)
	require.NoError(t, requireSettledRuntime(t.Context(), client.db, row.HistoryID, ""), "the session is admitted again")
	_, err = client.ClaimSessionQuiescence(t.Context())
	require.ErrorIs(t, err, ErrNotFound, "the suspension is no longer owed")
	_, err = client.ClaimStaleSessionQuiescence(t.Context(), 0)
	require.ErrorIs(t, err, ErrNotFound)
}

// A claim is only reconciled once it is old enough. One taken a moment ago
// belongs to a worker that may still be inside its call, and a claim with no
// recorded time belongs to a replica that predates this column.
func TestClaimStaleSessionQuiescenceLeavesLiveClaimsAlone(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	_, work := claimedQuiescence(t, client)
	_, err := client.ClaimStaleSessionQuiescence(t.Context(), time.Minute)
	require.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, execSQL(t.Context(), client.db, `
		UPDATE session_task_event SET quiescence_claimed_at = NULL WHERE sequence = $1
	`, work.Version))
	_, err = client.ClaimStaleSessionQuiescence(t.Context(), 0)
	require.ErrorIs(t, err, ErrNotFound, "an undated claim is never taken over")
}
