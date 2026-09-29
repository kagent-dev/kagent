package database

import (
	"context"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// pausedTaskFixture leaves a session paused on a waiting task, its pause done
// and its boundary recorded age ago.
func pausedTaskFixture(t *testing.T, age time.Duration) (*Client, *pgxpool.Pool, *apiv1alpha1.Session, *a2a.Task) {
	t.Helper()
	pool := setupTestDB(t)
	client := NewClient(pool, "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, task := waitingTaskFixture(t, client)
	agePause(t, pool, session, age)
	return client, pool, session, task
}

func agePause(t *testing.T, pool *pgxpool.Pool, session *apiv1alpha1.Session, age time.Duration) {
	t.Helper()
	_, err := pool.Exec(context.WithoutCancel(t.Context()), `
		UPDATE session_task_event SET created_at = created_at - $2::interval
		WHERE history_id = (SELECT history_id FROM session WHERE id = $1)
	`, session.Id, age)
	require.NoError(t, err)
}

func TestExpiredPauseIsClaimedForASuspend(t *testing.T) {
	client, _, session, task := pausedTaskFixture(t, time.Hour)
	ctx := t.Context()
	_, err := client.ClaimSessionQuiescence(ctx, 0)
	require.ErrorIs(t, err, ErrNotFound, "without a TTL a pause stays in place")
	_, err = client.ClaimSessionQuiescence(ctx, 2*time.Hour)
	require.ErrorIs(t, err, ErrNotFound, "a pause younger than the TTL stays in place")

	work, err := client.ClaimSessionQuiescence(ctx, 2*time.Minute)
	require.NoError(t, err)
	require.True(t, work.Suspend)
	require.Equal(t, session.Id, work.Session.Id)
	require.Equal(t, string(task.ID), work.TaskID)
	require.Equal(t, a2a.TaskStateInputRequired, work.State)
	_, err = client.ClaimSessionQuiescence(ctx, 2*time.Minute)
	require.ErrorIs(t, err, ErrNotFound, "a claimed suspend is not handed out twice")
	require.ErrorIs(t, client.ReserveSessionDispatch(ctx, session.Id, uuid.New(), "reply"), ErrDispatchBusy, "a reply waits for the suspend")

	snapshot := &SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/paused", ContentScope: "FULL"}
	require.NoError(t, client.FinishSessionQuiescence(ctx, work, snapshot))
	_, err = client.ClaimSessionQuiescence(ctx, 2*time.Minute)
	require.ErrorIs(t, err, ErrNotFound, "a suspended runtime has its snapshot")
	require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, uuid.New(), "reply"))
	_, _, err = client.ReserveSessionCheckpoint(ctx, &apiv1alpha1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID)}, "alice", uuid.NewString())
	require.ErrorIs(t, err, ErrFailedPrecondition, "a waiting head is still no checkpoint")
	stored, err := client.GetSessionTask(ctx, session.Id, string(task.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateInputRequired, stored.Status.State)
}

func TestExpiredPauseIsNotClaimedAfterAReply(t *testing.T) {
	client, pool, session, task := pausedTaskFixture(t, time.Hour)
	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("PostgreSQL"))
	reply.TaskID, reply.ContextID = task.ID, task.ContextID
	resumeRuntimeTask(t, client, session.Id, reply)
	agePause(t, pool, session, time.Hour)
	_, err := client.ClaimSessionQuiescence(t.Context(), 2*time.Minute)
	require.ErrorIs(t, err, ErrNotFound, "a task that took its reply is not paused")
}

func TestExpiredPauseWaitsForALiveDispatch(t *testing.T) {
	client, _, session, _ := pausedTaskFixture(t, time.Hour)
	require.NoError(t, client.ReserveSessionDispatch(t.Context(), session.Id, uuid.New(), "reply"))
	_, err := client.ClaimSessionQuiescence(t.Context(), 2*time.Minute)
	require.ErrorIs(t, err, ErrNotFound, "a reply being dispatched wins over the suspend")
}

func TestReleasedSuspendClaimCanBeClaimedAgain(t *testing.T) {
	client, _, session, _ := pausedTaskFixture(t, time.Hour)
	work, err := client.ClaimSessionQuiescence(t.Context(), 2*time.Minute)
	require.NoError(t, err)
	require.NoError(t, client.FinishSessionQuiescence(t.Context(), work, nil), "a suspend that did not happen releases the claim")
	dispatch := uuid.New()
	require.NoError(t, client.ReserveSessionDispatch(t.Context(), session.Id, dispatch, "reply"))
	_, err = client.RevokeSessionDispatch(t.Context(), session.Id, dispatch, "reply")
	require.NoError(t, err)
	again, err := client.ClaimSessionQuiescence(t.Context(), 2*time.Minute)
	require.NoError(t, err)
	require.True(t, again.Suspend)
	require.Equal(t, work.Version, again.Version)
}
