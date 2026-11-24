package database

import (
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

func TestIdlePurgeDiscoversPreviouslyExpiredHistory(t *testing.T) {
	client, expired := expirationFixture(t)
	ctx := t.Context()
	expiredRow, err := readSession(ctx, client.db, expired.Id)
	require.NoError(t, err)
	// Expire an empty Session using the existing lifecycle. No purge metadata
	// has been written, just as with expirations before this version.
	deletion, err := client.BeginIdleSessionDeletion(ctx, expired.Id, time.Now())
	require.NoError(t, err)
	finishAdmittedDeletion(t, client, deletion.Operation)

	manual, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), "manual")
	require.NoError(t, err)
	manual, err = markSessionReady(ctx, client, manual.Id, "manual.example")
	require.NoError(t, err)
	manualRow, err := readSession(ctx, client.db, manual.Id)
	require.NoError(t, err)
	savePurgeTask(t, client, manual, "snapshot")
	require.NoError(t, deleteSession(ctx, client, manual.Id))

	live, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), "live")
	require.NoError(t, err)
	liveRow, err := readSession(ctx, client.db, live.Id)
	require.NoError(t, err)

	var found []uuid.UUID
	var cursor uuid.UUID
	for range 3 {
		candidates, err := client.ListIdlePurgeCandidates(ctx, cursor, 1)
		require.NoError(t, err)
		require.Len(t, candidates, 1)
		cursor = candidates[0].HistoryID
		if candidates[0].Orphaned {
			found = append(found, cursor)
		}
	}
	require.Equal(t, []uuid.UUID{expiredRow.HistoryID}, found)
	_, err = client.DeleteIdlePurgeHistory(ctx, manualRow.HistoryID)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.DeleteIdlePurgeHistory(ctx, liveRow.HistoryID)
	require.ErrorIs(t, err, ErrNotFound)
	done, err := client.DeleteIdlePurgeHistory(ctx, found[0])
	require.NoError(t, err)
	require.True(t, done)
	assertPurgeRows(t, client, expiredRow.HistoryID, 0)
	assertPurgeRows(t, client, manualRow.HistoryID, 1)
}

func TestIdlePurgeFencesNewForksAndProtectsExistingFork(t *testing.T) {
	client, session := expirationFixture(t)
	ctx := t.Context()
	row, err := readSession(ctx, client.db, session.Id)
	require.NoError(t, err)
	task := savePurgeTask(t, client, session, "source-snapshot")
	checkpoint, _, err := client.ReserveSessionCheckpoint(ctx, &apiv1.Checkpoint{
		Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID),
	}, session.Creator, "checkpoint")
	require.NoError(t, err)
	_, err = client.FinalizeSessionCheckpoint(ctx, checkpoint.Id, "tag-uid", "s3://tag-snapshot", "")
	require.NoError(t, err)
	fork, _, err := client.ForkSession(ctx, checkpoint.Id, session.Creator, "fork", uuid.NewString())
	require.NoError(t, err)
	fork, err = markSessionReady(ctx, client, fork.Id, "fork.example")
	require.NoError(t, err)

	deletion, err := client.BeginIdleSessionDeletion(ctx, session.Id, time.Now())
	require.NoError(t, err)
	finishAdmittedDeletion(t, client, deletion.Operation)

	// Upgrading with purge off retains the READY checkpoint. Turning purge on
	// fences new requests immediately, even before the first backlog sweep.
	_, err = client.GetSessionCheckpointForkSource(ctx, checkpoint.Id, session.Creator, "new-fork")
	require.NoError(t, err)
	retainedFork, created, err := client.ForkSession(ctx, checkpoint.Id, session.Creator, "retained-fork", uuid.NewString())
	require.NoError(t, err)
	require.True(t, created)
	_, err = markSessionReady(ctx, client, retainedFork.Id, "retained.example")
	require.NoError(t, err)
	require.NoError(t, deleteSession(ctx, client, retainedFork.Id))

	client = NewClient(client.db.pool, "public", WithIdleExpiryPurge(true))
	_, err = client.GetSessionCheckpointForkSource(ctx, checkpoint.Id, session.Creator, "new-fork")
	require.ErrorIs(t, err, ErrNotFound)
	_, _, err = client.ForkSession(ctx, checkpoint.Id, session.Creator, "new-fork", uuid.NewString())
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.GetSessionCheckpointForkSource(ctx, checkpoint.Id, session.Creator, "fork")
	require.NoError(t, err)

	work, retired, err := client.BeginIdlePurgeCheckpointDeletion(ctx, row.HistoryID)
	require.NoError(t, err)
	require.True(t, retired)
	require.Nil(t, work, "a live fork still holds the checkpoint")
	_, err = client.DeleteIdlePurgeHistory(ctx, row.HistoryID)
	require.ErrorIs(t, err, ErrConflict)
	lastEventAt, err := client.IdlePurgeLastEventAt(ctx, row.HistoryID)
	require.NoError(t, err)
	require.False(t, lastEventAt.IsZero())
	_, err = client.GetSessionCheckpointForkSource(ctx, checkpoint.Id, session.Creator, "new-fork")
	require.ErrorIs(t, err, ErrNotFound)
	_, _, err = client.ForkSession(ctx, checkpoint.Id, session.Creator, "new-fork", uuid.NewString())
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.GetSessionCheckpointForkSource(ctx, checkpoint.Id, session.Creator, "fork")
	require.NoError(t, err, "the admitted fork can retry using its pin")
	retried, created, err := client.ForkSession(ctx, checkpoint.Id, session.Creator, "fork", uuid.NewString())
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, fork.Id, retried.Id)
	assertPurgeRows(t, client, row.HistoryID, 1)

	forks, err := client.ListIdlePurgeForks(ctx, row.HistoryID, "", 16)
	require.NoError(t, err)
	require.Empty(t, forks, "a fork with only the borrowed snapshot has no handoff to reconcile")
	snapshot := SessionTaskSnapshot{Atespace: "team-a", URI: "s3://fork-snapshot", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}
	forkTask := savePurgeTask(t, client, fork, snapshot.URI)
	forks, err = client.ListIdlePurgeForks(ctx, row.HistoryID, "", 16)
	require.NoError(t, err)
	require.Len(t, forks, 1)
	unknown := snapshot
	unknown.URI = "s3://unrecorded"
	released, err := client.ReleaseIdlePurgeForkPin(ctx, forks[0], unknown)
	require.NoError(t, err)
	require.False(t, released, "an unrecorded snapshot cannot release the pin")
	released, err = client.ReleaseIdlePurgeForkPin(ctx, forks[0], snapshot)
	require.NoError(t, err)
	require.True(t, released)

	work, _, err = client.BeginIdlePurgeCheckpointDeletion(ctx, row.HistoryID)
	require.NoError(t, err)
	require.NotNil(t, work)
	require.Equal(t, checkpoint.Id, work.ID)
	require.Equal(t, "tag-uid", work.TagUID)
	require.NoError(t, client.DeleteSessionCheckpoint(ctx, work.ID, work.UserID))
	_, err = client.DeleteIdlePurgeHistory(ctx, row.HistoryID)
	require.NoError(t, err)
	assertPurgeRows(t, client, row.HistoryID, 0)
	forkRow, err := readSession(ctx, client.db, fork.Id)
	require.NoError(t, err)
	require.Equal(t, checkpoint.Id, forkRow.SourceCheckpointID.String(), "fork request identity is retained")
	_, err = client.GetSessionTask(ctx, fork.Id, string(forkTask.ID), nil)
	require.NoError(t, err)
}

func TestIdlePurgeBatchesLargeHistory(t *testing.T) {
	client, session := expirationFixture(t)
	ctx := t.Context()
	row, err := readSession(ctx, client.db, session.Id)
	require.NoError(t, err)
	savePurgeTask(t, client, session, "snapshot")
	// Seed a large history from one valid event to exercise the storage limit.
	_, err = client.db.Exec(ctx, `
		INSERT INTO session_task_event (history_id, task_id, data)
		SELECT e.history_id, e.task_id, e.data FROM session_task_event e
		CROSS JOIN generate_series(1, 1000)
		WHERE e.sequence = (SELECT MIN(sequence) FROM session_task_event WHERE history_id = $1)
	`, row.HistoryID)
	require.NoError(t, err)
	deletion, err := client.BeginIdleSessionDeletion(ctx, session.Id, time.Now())
	require.NoError(t, err)
	finishAdmittedDeletion(t, client, deletion.Operation)

	done, err := client.DeleteIdlePurgeHistory(ctx, row.HistoryID)
	require.NoError(t, err)
	require.False(t, done)
	remaining, err := queryOne(ctx, client.db, `SELECT count(*) FROM session_task_event WHERE history_id = $1`, pgx.RowTo[int], row.HistoryID)
	require.NoError(t, err)
	require.Positive(t, remaining, "one batch cannot remove the full history")
	require.Less(t, remaining, 1000)

	done, err = client.DeleteIdlePurgeHistory(ctx, row.HistoryID)
	require.NoError(t, err)
	require.True(t, done)
	assertPurgeRows(t, client, row.HistoryID, 0)
}

func savePurgeTask(t *testing.T, client *Client, session *apiv1.Session, uri string) *a2a.Task {
	t.Helper()
	task := newSessionTask(uuid.NewString(), "message")
	task.ContextID, task.Status.State = session.ContextId, a2a.TaskStateCompleted
	require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, &SessionTaskSnapshot{
		Atespace: "team-a", URI: uri, Fidelity: "SNAPSHOT_FIDELITY_VOLUMES",
	}))
	return task
}

func assertPurgeRows(t *testing.T, client *Client, historyID uuid.UUID, want int) {
	t.Helper()
	type counts struct {
		Contexts int
		Tasks    int
		Events   int
	}
	rows, err := queryOne(t.Context(), client.db, `
		SELECT (SELECT count(*) FROM a2a_context WHERE id = $1) AS contexts,
		    (SELECT count(*) FROM session_task WHERE history_id = $1) AS tasks,
		    (SELECT count(*) FROM session_task_event WHERE history_id = $1) AS events
	`, pgx.RowToStructByName[counts], historyID)
	require.NoError(t, err)
	require.Equal(t, want, rows.Contexts)
	require.Equal(t, want, rows.Tasks)
	if want == 0 {
		require.Zero(t, rows.Events)
	} else {
		require.Positive(t, rows.Events)
	}
}
