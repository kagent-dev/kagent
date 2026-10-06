package database

import (
	"crypto/sha256"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
)

// testClaimLease outlives every test that does not wait for a claim to expire.
const testClaimLease = time.Hour

// completedBoundaryFixture settles a completed turn whose boundary waits for
// its idle suspend.
func completedBoundaryFixture(t *testing.T, client *Client) (*apiv1alpha1.Session, *a2a.Task) {
	t.Helper()
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, waiting := waitingTaskFixture(t, client)
	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("PostgreSQL"))
	reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
	current, initialVersion := resumeRuntimeTask(t, client, session.Id, reply)
	current.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted}
	hash := sha256.Sum256([]byte("completed"))
	version, err := client.UpdateSessionTask(t.Context(), session.Id, initialVersion, hash[:], current, current, "")
	require.NoError(t, err)
	require.NoError(t, client.SettleSessionTask(t.Context(), session.Id, string(current.ID), version))
	return session, current
}

// A claim whose holder stops renewing its lease is taken over to be settled,
// and the stopped holder can no longer renew, finish or release it.
func TestExpiredQuiescenceClaimIsTakenOver(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	session, _ := completedBoundaryFixture(t, client)
	work, err := client.ClaimSessionQuiescence(t.Context(), testClaimLease, 0, nil)
	require.NoError(t, err)
	require.False(t, work.TakenOver)
	_, err = client.ClaimSessionQuiescence(t.Context(), testClaimLease, 0, nil)
	require.ErrorIs(t, err, ErrNotFound, "a renewed claim is not taken over")

	require.NoError(t, client.RenewSessionQuiescence(t.Context(), work, 100*time.Millisecond))
	var taken *SessionQuiescence
	require.Eventually(t, func() bool {
		taken, err = client.ClaimSessionQuiescence(t.Context(), testClaimLease, 0, nil)
		return err == nil
	}, 5*time.Second, 20*time.Millisecond)
	require.True(t, taken.TakenOver)
	require.Equal(t, work.Version, taken.Version)
	require.Equal(t, work.TaskID, taken.TaskID)
	require.Equal(t, a2a.TaskStateCompleted, taken.State)
	require.NotEqual(t, work.ExecutorID, taken.ExecutorID)
	_, err = client.ClaimSessionQuiescence(t.Context(), testClaimLease, 0, nil)
	require.ErrorIs(t, err, ErrNotFound, "a taken-over claim holds a lease of its own")

	snapshot := &SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshot/stale", ContentScope: "DATA"}
	require.ErrorIs(t, client.RenewSessionQuiescence(t.Context(), work, testClaimLease), ErrNotFound)
	require.ErrorIs(t, client.FinishSessionQuiescence(t.Context(), work, snapshot), ErrNotFound)
	require.ErrorIs(t, client.ReleaseSessionQuiescence(t.Context(), work), ErrNotFound)
	fresh := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("next"))
	fresh.ContextID = session.ContextId
	hash := sha256.Sum256([]byte("next"))
	_, err = client.CreateRuntimeTask(t.Context(), session.Id, hash[:], a2a.NewSubmittedTask(fresh, fresh), "")
	require.ErrorIs(t, err, ErrFailedPrecondition, "a taken-over claim still refuses the next turn")

	require.NoError(t, client.ReleaseSessionQuiescence(t.Context(), taken))
	require.ErrorIs(t, client.RenewSessionQuiescence(t.Context(), taken, testClaimLease), ErrNotFound, "a settled claim is not renewed")
	_, err = client.ClaimSessionQuiescence(t.Context(), testClaimLease, 0, nil)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.CreateRuntimeTask(t.Context(), session.Id, hash[:], a2a.NewSubmittedTask(fresh, fresh), "")
	require.NoError(t, err)
}

// A claim made before claims held leases has none and is taken over at once.
func TestQuiescenceClaimWithoutLeaseIsTakenOver(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	_, task := completedBoundaryFixture(t, client)
	work, err := client.ClaimSessionQuiescence(t.Context(), testClaimLease, 0, nil)
	require.NoError(t, err)
	_, err = client.db.Exec(t.Context(), "UPDATE session_task_event SET quiescence_claimed_until = NULL WHERE sequence = $1", work.Version)
	require.NoError(t, err)

	taken, err := client.ClaimSessionQuiescence(t.Context(), testClaimLease, 0, nil)
	require.NoError(t, err)
	require.True(t, taken.TakenOver)
	require.Equal(t, string(task.ID), taken.TaskID)
}

func TestQuiescenceLeaseRequiresAHolder(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	completedBoundaryFixture(t, client)
	_, err := client.db.Exec(t.Context(), "UPDATE session_task_event SET quiescence_claimed_until = clock_timestamp() WHERE quiescence_pending")
	require.ErrorContains(t, err, "session_task_event_quiescence_lease_held")
	_, err = client.ClaimSessionQuiescence(t.Context(), 0, 0, nil)
	require.Error(t, err, "a claim without a lease is refused")
}
