package database

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

func TestTaskEventsRequireTaskIdentity(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	_, task := waitingTaskFixture(t, client)
	_, err := client.db.Exec(t.Context(), "UPDATE session_task_event SET task_id = NULL WHERE task_id = $1", string(task.ID))
	require.ErrorContains(t, err, "violates not-null constraint")
}

func TestRuntimeTaskSaveRetriesAndVersions(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, waiting := waitingTaskFixture(t, client)
	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("PostgreSQL"))
	reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
	resumeRuntimeTask(t, client, session.Id, reply)
	task, initialVersion, err := client.GetVersionedSessionTask(t.Context(), session.Id, string(waiting.ID))
	require.NoError(t, err)
	require.Positive(t, initialVersion)
	require.Len(t, task.History, 3)

	task.Status = a2a.TaskStatus{State: a2a.TaskStateWorking}
	firstHash := sha256.Sum256([]byte("working"))
	firstVersion, err := client.UpdateSessionTask(t.Context(), session.Id, initialVersion, firstHash[:], task, task, "")
	require.NoError(t, err)
	require.Greater(t, firstVersion, initialVersion)

	completed := *task
	completed.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted}
	secondHash := sha256.Sum256([]byte("completed"))
	secondVersion, err := client.UpdateSessionTask(t.Context(), session.Id, firstVersion, secondHash[:], &completed, &completed, "")
	require.NoError(t, err)
	require.Greater(t, secondVersion, firstVersion)

	// A response lost before the next update remains recoverable afterward.
	replayedVersion, err := client.UpdateSessionTask(t.Context(), session.Id, initialVersion, firstHash[:], task, task, "")
	require.NoError(t, err)
	require.Equal(t, firstVersion, replayedVersion)
	_, err = client.UpdateSessionTask(t.Context(), session.Id, initialVersion, secondHash[:], &completed, &completed, "")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.UpdateSessionTask(t.Context(), session.Id, secondVersion, firstHash[:], task, task, "")
	require.ErrorIs(t, err, ErrFailedPrecondition)
	stored, version, err := client.GetVersionedSessionTask(t.Context(), session.Id, string(task.ID))
	require.NoError(t, err)
	require.Equal(t, secondVersion, version)
	require.Equal(t, a2a.TaskStateCompleted, stored.Status.State)
	require.Len(t, stored.History, 3)
}

func TestConcurrentRuntimeTaskSaves(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, waiting := waitingTaskFixture(t, client)
	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("PostgreSQL"))
	reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
	resumeRuntimeTask(t, client, session.Id, reply)
	task, version, err := client.GetVersionedSessionTask(t.Context(), session.Id, string(waiting.ID))
	require.NoError(t, err)
	start, results := make(chan struct{}), make(chan error, 2)
	for _, text := range []string{"one", "two"} {
		go func() {
			update := *task
			update.Status = a2a.TaskStatus{State: a2a.TaskStateWorking, Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(text))}
			hash := sha256.Sum256([]byte(text))
			<-start
			_, err := client.UpdateSessionTask(t.Context(), session.Id, version, hash[:], &update, &update, "")
			results <- err
		}()
	}
	close(start)
	first, second := <-results, <-results
	if first != nil {
		first, second = second, first
	}
	require.NoError(t, first)
	require.ErrorIs(t, second, ErrConflict)
}

func TestRuntimeTaskSaveScopeAndAtomicity(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, waiting := waitingTaskFixture(t, client)
	task, version, err := client.GetVersionedSessionTask(t.Context(), session.Id, string(waiting.ID))
	require.NoError(t, err)
	hash := sha256.Sum256([]byte("update"))
	_, _, err = client.GetVersionedSessionTask(t.Context(), uuid.NewString(), string(task.ID))
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.UpdateSessionTask(t.Context(), uuid.NewString(), version, hash[:], task, task, "")
	require.ErrorIs(t, err, ErrNotFound)
	wrongScope := *task
	wrongScope.ContextID = uuid.NewString()
	_, err = client.UpdateSessionTask(t.Context(), session.Id, version, hash[:], &wrongScope, &wrongScope, "")
	require.ErrorIs(t, err, ErrFailedPrecondition)

	// Cancellation is allowed while waiting, but malformed event identity must
	// roll back the entire save, including any archived status message.
	canceled := *task
	canceled.Status = a2a.TaskStatus{State: a2a.TaskStateCanceled}
	_, err = client.UpdateSessionTask(t.Context(), session.Id, version, hash[:], &canceled, &wrongScope, "")
	require.Error(t, err)
	stored, after, err := client.GetVersionedSessionTask(t.Context(), session.Id, string(task.ID))
	require.NoError(t, err)
	require.Equal(t, version, after)
	require.Equal(t, a2a.TaskStateInputRequired, stored.Status.State)
	require.Len(t, stored.History, len(task.History))
}

func TestRuntimeTaskCreateRetries(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, _ := waitingTaskFixture(t, client)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("new task"))
	message.ContextID = session.ContextId
	task := a2a.NewSubmittedTask(message, message)
	hash := sha256.Sum256([]byte("create"))
	version, err := client.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, "")
	require.NoError(t, err)
	retry, err := client.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, "")
	require.NoError(t, err)
	require.Equal(t, version, retry)
	different := sha256.Sum256([]byte("different create"))
	_, err = client.CreateRuntimeTask(t.Context(), session.Id, different[:], task, "")
	require.ErrorIs(t, err, ErrIdempotencyConflict)
	working := *task
	working.Status.State = a2a.TaskStateWorking
	_, err = client.UpdateSessionTask(t.Context(), session.Id, version, different[:], &working, &working, "")
	require.NoError(t, err)
	retry, err = client.CreateRuntimeTask(t.Context(), session.Id, hash[:], task, "")
	require.NoError(t, err)
	require.Equal(t, version, retry)
}

// resumeRuntimeTask models the SDK's ordinary read, append-input, and working save.
func resumeRuntimeTask(t *testing.T, client *Client, sessionID string, reply *a2a.Message) (*a2a.Task, int64) {
	t.Helper()
	task, version, err := client.GetVersionedSessionTask(t.Context(), sessionID, string(reply.TaskID))
	require.NoError(t, err)
	if task.Status.Message != nil {
		task.History = append(task.History, task.Status.Message)
	}
	task.History = append(task.History, reply)
	hash := sha256.Sum256([]byte("input " + reply.ID))
	version, err = client.UpdateSessionTask(t.Context(), sessionID, version, hash[:], task, task, "")
	require.NoError(t, err)
	task.Status = a2a.TaskStatus{State: a2a.TaskStateWorking}
	hash = sha256.Sum256([]byte("start " + reply.ID))
	version, err = client.UpdateSessionTask(t.Context(), sessionID, version, hash[:], task, task, "")
	require.NoError(t, err)
	return task, version
}

func TestRuntimeCompletionDoesNotWaitForSnapshot(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, waiting := waitingTaskFixture(t, client)
	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("PostgreSQL"))
	reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
	current, initialVersion := resumeRuntimeTask(t, client, session.Id, reply)
	finished := *current
	finished.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted, Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("done"))}
	hash := sha256.Sum256([]byte("complete"))
	version, err := client.UpdateSessionTask(t.Context(), session.Id, initialVersion, hash[:], &finished, &finished, "")
	require.NoError(t, err)
	private, privateVersion, err := client.GetVersionedSessionTask(t.Context(), session.Id, string(waiting.ID))
	require.NoError(t, err)
	require.Equal(t, version, privateVersion)
	require.Equal(t, a2a.TaskStateCompleted, private.Status.State)
	public, err := client.GetSessionTask(t.Context(), session.Id, string(waiting.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateWorking, public.Status.State)
	_, err = client.GetSettledSessionTask(t.Context(), session.Id, string(waiting.ID), nil)
	require.ErrorIs(t, err, ErrConflict)
	require.ErrorIs(t, deleteSession(t.Context(), client, session.Id), ErrFailedPrecondition)
	_, err = client.ClaimSessionQuiescence(t.Context())
	require.ErrorIs(t, err, ErrNotFound) // The native cleanup callback has not finished.
	require.NoError(t, client.SettleSessionTask(t.Context(), session.Id, string(waiting.ID), version))
	public, err = client.GetSettledSessionTask(t.Context(), session.Id, string(waiting.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, public.Status.State)
	require.Len(t, public.History, 3)
	work, err := client.ClaimSessionQuiescence(t.Context())
	require.NoError(t, err)
	require.Equal(t, version, work.Version)
	_, err = client.ClaimSessionQuiescence(t.Context())
	require.ErrorIs(t, err, ErrNotFound) // Claims cannot expire into a second suspend.
	require.Error(t, client.FinishSessionQuiescence(t.Context(), work, nil))
	// Even an unfinished or failed snapshot never hides the completed task.
	public, err = client.GetSettledSessionTask(t.Context(), session.Id, string(waiting.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, public.Status.State)
	require.ErrorIs(t, deleteSession(t.Context(), client, session.Id), ErrFailedPrecondition)

	fresh := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("next"))
	fresh.ContextID = session.ContextId
	_, err = client.CreateRuntimeTask(t.Context(), session.Id, hash[:], a2a.NewSubmittedTask(fresh, fresh), "")
	require.ErrorIs(t, err, ErrFailedPrecondition)
	snapshot := &SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshot/exact", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}
	require.NoError(t, client.FinishSessionQuiescence(t.Context(), work, snapshot))
	require.NoError(t, client.FinishSessionQuiescence(t.Context(), work, snapshot))
	require.NoError(t, client.SettleSessionTask(t.Context(), session.Id, string(waiting.ID), version))
	public, err = client.GetSessionTask(t.Context(), session.Id, string(waiting.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, public.Status.State)
	require.Len(t, public.History, 3)
	_, err = client.ClaimSessionQuiescence(t.Context())
	require.ErrorIs(t, err, ErrNotFound)
}

func TestRuntimeForkRetainsOnlyTheCheckpointBoundary(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	source, waiting := waitingTaskFixture(t, client)
	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("PostgreSQL"))
	reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
	completed, initialVersion := resumeRuntimeTask(t, client, source.Id, reply)
	completed.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted}
	hash := sha256.Sum256([]byte("checkpoint boundary"))
	version, err := client.UpdateSessionTask(t.Context(), source.Id, initialVersion, hash[:], completed, completed, "")
	require.NoError(t, err)
	_, _, err = client.ReserveSessionCheckpoint(t.Context(), &apiv1.Checkpoint{Id: uuid.NewString(), SessionId: source.Id, HeadTaskId: string(completed.ID)}, "alice", uuid.NewString())
	require.ErrorIs(t, err, ErrFailedPrecondition)
	require.NoError(t, client.SettleSessionTask(t.Context(), source.Id, string(completed.ID), version))
	boundary, err := client.ClaimSessionQuiescence(t.Context())
	require.NoError(t, err)
	require.NoError(t, client.FinishSessionQuiescence(t.Context(), boundary, &SessionTaskSnapshot{Atespace: "team-a", URI: "turn-N", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}))
	checkpoint, _, err := client.ReserveSessionCheckpoint(t.Context(), &apiv1.Checkpoint{Id: uuid.NewString(), SessionId: source.Id, HeadTaskId: string(completed.ID)}, "alice", uuid.NewString())
	require.NoError(t, err)
	_, err = client.FinalizeSessionCheckpoint(t.Context(), checkpoint.Id, "tag-N", "retained-N", "")
	require.NoError(t, err)
	newInput := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("source after checkpoint"))
	newInput.ContextID = source.ContextId
	later := a2a.NewSubmittedTask(newInput, newInput)
	_, err = client.CreateRuntimeTask(t.Context(), source.Id, hash[:], later, "")
	require.NoError(t, err)
	fork, _, err := client.ForkSession(t.Context(), checkpoint.Id, "alice", uuid.NewString(), uuid.NewString())
	require.NoError(t, err)
	fork, err = markSessionReady(t.Context(), client, fork.Id, "fork.example")
	require.NoError(t, err)
	forkTasks, _, err := client.ListSessionTasks(t.Context(), fork.Id, "", a2a.TaskStateUnspecified, nil, 10, nil)
	require.NoError(t, err)
	require.Len(t, forkTasks, 1)
	inherited, forkVersion, err := client.GetVersionedSessionTask(t.Context(), fork.Id, string(forkTasks[0].ID))
	require.NoError(t, err)
	require.Equal(t, fork.Id, inherited.ContextID)
	require.NotEqual(t, completed.ID, inherited.ID)
	require.Equal(t, a2a.TaskStateCompleted, inherited.Status.State)
	require.NotEqual(t, version, forkVersion)
	// Public task IDs resolve to exactly one conversation, including copies
	// whose historical positions are equal to their source's positions.
	for _, pair := range []struct{ taskID, sessionID string }{
		{string(completed.ID), source.Id}, {string(inherited.ID), fork.Id},
	} {
		owner, err := client.SessionForTask(t.Context(), pair.taskID)
		require.NoError(t, err)
		require.Equal(t, pair.sessionID, owner)
	}
	seen := map[a2a.TaskID]bool{}
	cursor := ""
	for range 3 {
		page, total, err := client.ListAgentTasks(t.Context(), []string{source.Id, fork.Id}, cursor, a2a.TaskStateUnspecified, nil, 1, nil)
		require.NoError(t, err)
		require.Equal(t, 3, total)
		require.Len(t, page, 1)
		require.False(t, seen[page[0].ID])
		seen[page[0].ID], cursor = true, string(page[0].ID)
	}
	page, total, err := client.ListAgentTasks(t.Context(), []string{fork.Id}, "", a2a.TaskStateUnspecified, nil, 10, nil)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, inherited.ID, page[0].ID)
	page, total, err = client.ListAgentTasks(t.Context(), nil, "", a2a.TaskStateUnspecified, nil, 10, nil)
	require.NoError(t, err)
	require.Empty(t, page)
	require.Zero(t, total)
	_, _, err = client.GetVersionedSessionTask(t.Context(), fork.Id, string(later.ID))
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.UpdateSessionTask(t.Context(), fork.Id, initialVersion, hash[:], inherited, inherited, "")
	require.ErrorIs(t, err, ErrConflict, "source mutation receipts must not grant fork writes")
	newInput.ID = "fork-input"
	newInput.ContextID = fork.Id
	newInput.Parts = a2a.ContentParts{a2a.NewTextPart("independent fork")}
	forkInput := a2a.NewSubmittedTask(newInput, newInput)
	_, err = client.CreateRuntimeTask(t.Context(), fork.Id, hash[:], forkInput, "")
	require.NoError(t, err)
	_, err = client.GetSessionTask(t.Context(), source.Id, string(forkInput.ID), nil)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestNewExecutionRacesIdleClaim(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	for range 12 {
		session, waiting := waitingTaskFixture(t, client)
		reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("continue"))
		reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
		task, version := resumeRuntimeTask(t, client, session.Id, reply)
		task.Status.State = a2a.TaskStateCompleted
		version, err := client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("finish"), task, task, "")
		require.NoError(t, err)
		require.NoError(t, client.SettleSessionTask(ctx, session.Id, string(task.ID), version))

		start := make(chan struct{})
		claimed := make(chan *SessionQuiescence, 1)
		claimErrors, writeErrors := make(chan error, 1), make(chan error, 1)
		go func() {
			<-start
			work, err := client.ClaimSessionQuiescence(ctx)
			claimed <- work
			claimErrors <- err
		}()
		next := newSessionTask(uuid.NewString(), "next-message")
		next.ContextID = session.ContextId
		dispatchID := uuid.New()
		go func() {
			<-start
			err := client.ReserveSessionDispatch(ctx, session.Id, dispatchID, "")
			writeErrors <- err
		}()
		close(start)
		work, claimErr, writeErr := <-claimed, <-claimErrors, <-writeErrors
		if claimErr == nil {
			require.ErrorIs(t, writeErr, ErrDispatchBusy)
			require.NoError(t, client.FinishSessionQuiescence(ctx, work, &SessionTaskSnapshot{Atespace: "team-a", URI: "snapshot", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}))
			require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, dispatchID, ""))
		} else {
			require.ErrorIs(t, claimErr, ErrNotFound)
			require.NoError(t, writeErr)
		}
		_, err = client.ClaimSessionQuiescence(ctx)
		require.ErrorIs(t, err, ErrNotFound, "dispatch must protect the network gap before persistence")
		_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("next"), next, dispatchID.String())
		require.NoError(t, err)
		// A lost cleanup acknowledgement cannot requeue an obsolete suspension.
		require.NoError(t, client.SettleSessionTask(ctx, session.Id, string(task.ID), version))
		_, err = client.ClaimSessionQuiescence(ctx)
		require.ErrorIs(t, err, ErrNotFound)
		visible, err := client.GetSettledSessionTask(ctx, session.Id, string(task.ID), nil)
		require.NoError(t, err)
		require.Equal(t, a2a.TaskStateCompleted, visible.Status.State)
	}
}

func TestRuntimeTaskLookupRejectsAmbiguousMessageIDs(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, waiting := waitingTaskFixture(t, client)
	messageID := waiting.History[0].ID
	found, err := client.GetSessionTaskByMessage(t.Context(), session.Id, "", messageID)
	require.NoError(t, err)
	require.Equal(t, waiting.ID, found.ID)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("another task"))
	message.ID, message.ContextID = messageID, session.ContextId
	task := a2a.NewSubmittedTask(message, message)
	_, err = client.CreateRuntimeTask(t.Context(), session.Id, taskMutationHash("another task"), task, "")
	require.NoError(t, err)
	_, err = client.GetSessionTaskByMessage(t.Context(), session.Id, "", messageID)
	require.ErrorIs(t, err, ErrConflict)
	found, err = client.GetSessionTaskByMessage(t.Context(), session.Id, string(task.ID), messageID)
	require.NoError(t, err)
	require.Equal(t, task.ID, found.ID)
	_, err = client.GetSessionTaskByMessage(t.Context(), uuid.NewString(), "", messageID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestDispatchFencesIdleWorkAndLateAcceptance(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, waiting := waitingTaskFixture(t, client)
	dispatchID := uuid.New()
	require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, dispatchID, ""))
	_, err := client.ClaimSessionQuiescence(ctx)
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, client.ReserveSessionDispatch(ctx, session.Id, uuid.New(), ""), ErrDispatchBusy)
	revoked, err := client.RevokeSessionDispatch(ctx, session.Id, dispatchID, "new-input")
	require.NoError(t, err)
	require.True(t, revoked)
	_, version, err := client.GetVersionedSessionTask(ctx, session.Id, string(waiting.ID))
	require.NoError(t, err)
	resumed := *waiting
	resumed.Status.State = a2a.TaskStateWorking
	_, err = client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("late"), &resumed, &resumed, dispatchID.String())
	require.ErrorIs(t, err, ErrFailedPrecondition)

	dispatchID = uuid.New()
	require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, dispatchID, ""))
	// SDKs may first append input while retaining the waiting state.
	version, err = client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("input-only"), waiting, waiting, dispatchID.String())
	require.NoError(t, err)
	_, err = client.ClaimSessionQuiescence(ctx)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("active"), &resumed, &resumed, dispatchID.String())
	require.NoError(t, err)
	revoked, err = client.RevokeSessionDispatch(ctx, session.Id, dispatchID, "new-input")
	require.NoError(t, err)
	require.False(t, revoked, "accepted work must never be reported retryable")
	require.ErrorIs(t, client.ReserveSessionDispatch(ctx, session.Id, uuid.New(), ""), ErrConflict)
}

func TestDispatchExpiryRejectsLateSave(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, waiting := waitingTaskFixture(t, client)
	id := uuid.New()
	require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, id, ""))
	_, err := client.db.Exec(ctx, `UPDATE session SET dispatch_expires_at = clock_timestamp() - INTERVAL '1 second' WHERE id = $1`, session.Id)
	require.NoError(t, err)
	next := newSessionTask(uuid.NewString(), "next-message")
	next.ContextID = session.ContextId
	_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("expired"), next, id.String())
	require.ErrorIs(t, err, ErrFailedPrecondition)
	newID := uuid.New()
	require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, newID, ""))
	revoked, err := client.RevokeSessionDispatch(ctx, session.Id, id, "next-message")
	require.NoError(t, err)
	require.False(t, revoked)
	_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("stale"), next, id.String())
	require.ErrorIs(t, err, ErrFailedPrecondition)
	_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("current"), next, newID.String())
	require.NoError(t, err)
	stored, err := client.GetSessionTask(ctx, session.Id, string(waiting.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateInputRequired, stored.Status.State)
}

func TestRevokedContinuationWithSavedInputIsNotRetryable(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, waiting := waitingTaskFixture(t, client)
	id := uuid.New()
	require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, id, ""))
	_, version, err := client.GetVersionedSessionTask(ctx, session.Id, string(waiting.ID))
	require.NoError(t, err)
	input := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("continue"))
	input.TaskID, input.ContextID = waiting.ID, waiting.ContextID
	waiting.History = append(waiting.History, input)
	_, err = client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("input"), waiting, input, id.String())
	require.NoError(t, err)
	notAccepted, err := client.RevokeSessionDispatch(ctx, session.Id, id, input.ID)
	require.NoError(t, err)
	require.False(t, notAccepted, "revocation must not make persisted input retryable")
}

func TestCheckpointPinsExpectedTaskWhileSnapshotIsPending(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, waiting := waitingTaskFixture(t, client)
	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("continue"))
	reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
	task, version := resumeRuntimeTask(t, client, session.Id, reply)
	task.Status.State = a2a.TaskStateCompleted
	version, err := client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("finish"), task, task, "")
	require.NoError(t, err)
	require.NoError(t, client.SettleSessionTask(ctx, session.Id, string(task.ID), version))
	request := &apiv1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID)}
	_, _, err = client.ReserveSessionCheckpoint(ctx, request, "alice", "checkpoint")
	require.ErrorIs(t, err, ErrSnapshotPending)
	work, err := client.ClaimSessionQuiescence(ctx)
	require.NoError(t, err)
	require.NoError(t, client.FinishSessionQuiescence(ctx, work, &SessionTaskSnapshot{Atespace: "team-a", URI: "snapshot", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}))
	saved, _, err := client.ReserveSessionCheckpoint(ctx, request, "alice", "saved")
	require.NoError(t, err)
	_, err = client.FinalizeSessionCheckpoint(ctx, saved.Id, "tag", "retained-snapshot", "")
	require.NoError(t, err)
	next := newSessionTask(uuid.NewString(), "next-message")
	next.ContextID = session.ContextId
	_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("next"), next, "")
	require.NoError(t, err)
	_, _, err = client.ReserveSessionCheckpoint(ctx, request, "alice", "checkpoint")
	require.ErrorIs(t, err, ErrCheckpointAdvanced)
	replayed, _, err := client.ReserveSessionCheckpoint(ctx, request, "alice", "saved")
	require.NoError(t, err)
	require.Equal(t, saved.Id, replayed.Id)
	request.HeadTaskId = string(next.ID)
	_, _, err = client.ReserveSessionCheckpoint(ctx, request, "alice", "saved")
	require.ErrorIs(t, err, ErrIdempotencyConflict)
}

func TestInitialMessageReservationDeduplicatesAfterAcceptance(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	session, err = markSessionReady(ctx, client, session.Id, "runtime")
	require.NoError(t, err)
	dispatch := uuid.New()
	require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, dispatch, "first-message"))
	require.ErrorIs(t, client.ReserveSessionDispatch(ctx, session.Id, uuid.New(), "first-message"), ErrDispatchBusy)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
	message.ID, message.ContextID = "first-message", session.Id
	task := a2a.NewSubmittedTask(message, message)
	_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("first"), task, dispatch.String())
	require.NoError(t, err)
	require.ErrorIs(t, client.ReserveSessionDispatch(ctx, session.Id, uuid.New(), "first-message"), ErrMessageAccepted)
	require.ErrorIs(t, client.ReserveSessionDispatch(ctx, session.Id, uuid.New(), "different-message"), ErrConflict)
	recovered, err := client.GetSessionTaskByMessage(ctx, session.Id, "", "first-message")
	require.NoError(t, err)
	require.Equal(t, task.ID, recovered.ID)
	// Two concurrent retries both observe acceptance, never a new dispatch.
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- client.ReserveSessionDispatch(ctx, session.Id, uuid.New(), "first-message") }()
	}
	for range 2 {
		require.ErrorIs(t, <-results, ErrMessageAccepted)
	}
}

func TestCheckpointRejectsOlderPendingTask(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, _ := waitingTaskFixture(t, client)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("another task"))
	message.ContextID = session.Id
	task := a2a.NewSubmittedTask(message, message)
	require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, nil))
	task.Status.State = a2a.TaskStateCompleted
	require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, &SessionTaskSnapshot{Atespace: "team-a", URI: "snapshot", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}))
	_, _, err := client.ReserveSessionCheckpoint(ctx, &apiv1.Checkpoint{
		Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID),
	}, "alice", uuid.NewString())
	require.ErrorIs(t, err, ErrFailedPrecondition)
	require.ErrorContains(t, err, "all tasks to be terminal")
}

func TestDispatchCleanupPreservesTasklessSession(t *testing.T) {
	t.Run("Given a restored actor without a turn, when its dispatch is revoked, assert that cleanup preserves empty history and rejects the expired input", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		id := uuid.New()
		require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, id, "input"))
		require.Empty(t, lifecycleJobsForRuntime(t, client, session.Id), "successful dispatch reservation must not write a recovery job")
		_, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound, "live delivery must not become cleanup")
		revoked, err := client.RevokeSessionDispatch(ctx, session.Id, id, "input")
		require.NoError(t, err)
		require.True(t, revoked)
		jobs := lifecycleJobsForRuntime(t, client, session.Id)
		require.Len(t, jobs, 1, "revocation must enqueue its recovery in the same transaction")
		require.Equal(t, id, jobs[0].Generation)
		require.Equal(t, RuntimeLifecycleJobRecovering, jobs[0].Phase)
		require.Nil(t, jobs[0].ExecutorID)
		revoked, err = client.RevokeSessionDispatch(ctx, session.Id, id, "input")
		require.NoError(t, err)
		require.True(t, revoked, "repeated revocation must preserve proof that input was never accepted")
		require.Equal(t, jobs, lifecycleJobsForRuntime(t, client, session.Id), "repeated revocation must preserve the existing schedule")
		task := newSessionTask(uuid.NewString(), "input")
		task.ContextID = session.ContextId
		_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("late"), task, id.String())
		require.ErrorIs(t, err, ErrFailedPrecondition, "revocation fences late input before cleanup claims")
		work, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.Equal(t, id, work.DispatchID)
		replaced := *work
		replaced.ActorUID = "another-actor"
		require.ErrorIs(t, client.ContinueSessionDispatchCleanup(ctx, &replaced, false), ErrConflict,
			"the claim must remain bound to the observed actor")
		require.NotEqual(t, uuid.Nil, work.ExecutorID)
		revoked, err = client.RevokeSessionDispatch(ctx, session.Id, id, "input")
		require.NoError(t, err)
		require.True(t, revoked, "repeated revocation remains safe after cleanup is claimed")
		_, err = client.ClaimSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound)
		_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("late"), task, id.String())
		require.ErrorIs(t, err, ErrFailedPrecondition)
		require.ErrorIs(t, client.FinishSessionDispatchCleanup(ctx, work, nil), ErrFailedPrecondition)
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, work, true))
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()))
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()))
		revoked, err = client.RevokeSessionDispatch(ctx, session.Id, id, "input")
		require.NoError(t, err)
		require.False(t, revoked, "completed cleanup no longer retains the dispatch proof")
		_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("late"), task, id.String())
		require.ErrorIs(t, err, ErrFailedPrecondition, "completed cleanup must not revive the old dispatch")
		_, err = client.ClaimSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound)
		tasks, total, err := client.ListSessionTasks(ctx, session.Id, "", a2a.TaskStateUnspecified, nil, 10, nil)
		require.NoError(t, err)
		require.Empty(t, tasks)
		require.Zero(t, total)
		current, err := client.GetSessionByID(ctx, session.Id)
		require.NoError(t, err)
		require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, current.State)
		require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, uuid.New(), "next"))
	})
}

func TestDispatchCleanupRecoversExpiredDelivery(t *testing.T) {
	t.Run("Given a gateway that never revoked its delivery, when its reservation expires, assert that cleanup fences late acceptance", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		id := uuid.New()
		require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, id, "input"))
		require.Empty(t, lifecycleJobsForRuntime(t, client, session.Id))
		// Advance durable deadlines without waiting for the production lease.
		_, err := client.db.Exec(ctx, `UPDATE session SET dispatch_expires_at = clock_timestamp() - INTERVAL '1 second' WHERE id = $1`, session.Id)
		require.NoError(t, err)
		restarted := NewClient(sharedDB, "public")
		work, err := restarted.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		jobs := lifecycleJobsForRuntime(t, restarted, session.Id)
		require.Len(t, jobs, 1, "a replacement controller must persist ownership before runtime work even if revocation never ran")
		require.Equal(t, id, jobs[0].Generation)
		require.Equal(t, work.ExecutorID, *jobs[0].ExecutorID)
		revoked, err := client.RevokeSessionDispatch(ctx, session.Id, id, "input")
		require.NoError(t, err)
		require.True(t, revoked, "expiry must preserve proof that the input was never accepted")
		task := newSessionTask(uuid.NewString(), "input")
		task.ContextID = session.ContextId
		_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("late"), task, id.String())
		require.ErrorIs(t, err, ErrFailedPrecondition)
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, work, true))
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()))
	})
}

func TestDispatchCleanupClaimsCurrentGeneration(t *testing.T) {
	t.Run("Given a replacement abandoned delivery, when cleanup claims, assert that it owns only the current dispatch", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		first := abandonedDispatchFixture(t, client, session)
		second := abandonedDispatchFixture(t, client, session)
		revoked, err := client.RevokeSessionDispatch(ctx, session.Id, first.DispatchID, "input")
		require.NoError(t, err)
		require.False(t, revoked)
		work, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.Equal(t, second.DispatchID, work.DispatchID)
		require.NotEqual(t, first.DispatchID, work.DispatchID)
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, work, true))
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()))
		third := abandonedDispatchFixture(t, client, session)
		require.ErrorIs(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()), ErrConflict)
		pending, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.Equal(t, third.DispatchID, pending.DispatchID)
		require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, uuid.New(), "next"), "claimed cleanup must not block sends")
	})
}

func TestDispatchCleanupRacesNewDelivery(t *testing.T) {
	t.Run("Given pending cleanup, when a new delivery and cleanup claim race, assert that the new delivery supersedes cleanup", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		for range 12 {
			abandonedDispatchFixture(t, client, session)
			start := make(chan struct{})
			claimed := make(chan *SessionDispatchCleanup, 1)
			claimErrors, sendErrors := make(chan error, 1), make(chan error, 1)
			next := uuid.New()
			go func() {
				<-start
				work, err := client.ClaimSessionDispatchCleanup(ctx, "")
				claimed <- work
				claimErrors <- err
			}()
			go func() {
				<-start
				sendErrors <- client.ReserveSessionDispatch(ctx, session.Id, next, "next")
			}()
			close(start)
			work, claimErr, sendErr := <-claimed, <-claimErrors, <-sendErrors
			require.NoError(t, sendErr)
			if claimErr == nil {
				require.ErrorIs(t, client.ContinueSessionDispatchCleanup(ctx, work, false), ErrConflict)
				require.ErrorIs(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()), ErrConflict)
			} else {
				require.ErrorIs(t, claimErr, ErrNotFound)
			}
			_, err := client.ClaimSessionDispatchCleanup(ctx, "")
			require.ErrorIs(t, err, ErrNotFound, "the newer live dispatch must not become cleanup")
			_, err = client.RevokeSessionDispatch(ctx, session.Id, next, "next")
			require.NoError(t, err)
		}
	})
}

func TestNewDispatchSupersedesClaimedCleanup(t *testing.T) {
	for _, suspending := range []bool{false, true} {
		name := "Given claimed cleanup before suspension, when a newer message arrives, assert that its turn starts and old cleanup cannot continue"
		if suspending {
			name = "Given cleanup with suspension already issued, when a newer message arrives, assert that its turn starts and old cleanup cannot retry"
		}
		t.Run(name, func(t *testing.T) {
			client, session := dispatchCleanupFixture(t)
			ctx := t.Context()
			abandonedDispatchFixture(t, client, session)
			work, err := client.ClaimSessionDispatchCleanup(ctx, "")
			require.NoError(t, err)
			require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, work, suspending))

			next := uuid.New()
			require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, next, "new-input"))
			require.ErrorIs(t, client.ContinueSessionDispatchCleanup(ctx, work, false), ErrConflict)
			require.ErrorIs(t, client.ContinueSessionDispatchCleanup(ctx, work, true), ErrConflict)
			require.NoError(t, client.ReleaseSessionDispatchCleanup(ctx, work), "the owner may retire its obsolete job without changing the newer dispatch")
			require.ErrorIs(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()), ErrConflict)

			task := newSessionTask(uuid.NewString(), "new-input")
			task.ContextID = session.ContextId
			version, err := client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("new-input"), task, next.String())
			require.NoError(t, err, "stale cleanup must not retire the newer dispatch")
			require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()))
			stored, storedVersion, err := client.GetVersionedSessionTask(ctx, session.Id, string(task.ID))
			require.NoError(t, err)
			require.Equal(t, version, storedVersion)
			require.Equal(t, task.Status.State, stored.Status.State)
			row, err := readSession(ctx, client.db, session.Id)
			require.NoError(t, err)
			storedRow, err := readSessionTask(ctx, client.db, row.HistoryID, string(task.ID))
			require.NoError(t, err)
			require.Nil(t, storedRow.SnapshotURI, "an old cleanup snapshot must not be attached to the new turn")
			_, err = client.ClaimSessionDispatchCleanup(ctx, "")
			require.ErrorIs(t, err, ErrNotFound)
		})
	}
}

func TestDispatchCleanupRecoversExpiredOwner(t *testing.T) {
	for _, suspending := range []bool{false, true} {
		name := "Given an abandoned cleanup owner before suspension, when another controller takes over, assert that cleanup can resume and the old owner cannot act"
		if suspending {
			name = "Given an abandoned cleanup owner after suspension starts, when another controller takes over, assert that it preserves the suspension phase"
		}
		t.Run(name, func(t *testing.T) {
			client, session := dispatchCleanupFixture(t)
			ctx := t.Context()
			abandonedDispatchFixture(t, client, session)
			first, err := client.ClaimSessionDispatchCleanup(ctx, "")
			require.NoError(t, err)
			require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, first, suspending))
			_, err = client.ClaimSessionDispatchCleanup(ctx, "")
			require.ErrorIs(t, err, ErrNotFound, "a live worker must retain its bounded attempt")
			// Simulate loss of the worker without waiting for the production timeout.
			_, err = client.db.Exec(ctx, `UPDATE runtime_lifecycle_job SET available_at = clock_timestamp() - INTERVAL '1 second' WHERE runtime_id = $1 AND kind = $2`, session.Id, RuntimeLifecycleJobRecoverAbandonedDispatch)
			require.NoError(t, err)

			restarted := NewClient(sharedDB, "public")
			second, err := restarted.ClaimSessionDispatchCleanup(ctx, "")
			require.NoError(t, err)
			require.Equal(t, first.DispatchID, second.DispatchID)
			require.NotEqual(t, first.ExecutorID, second.ExecutorID)
			require.Equal(t, suspending, second.Suspending)
			require.ErrorIs(t, client.ContinueSessionDispatchCleanup(ctx, first, true), ErrConflict)
			require.ErrorIs(t, client.ReleaseSessionDispatchCleanup(ctx, first), ErrConflict)
			require.ErrorIs(t, client.FinishSessionDispatchCleanup(ctx, first, dispatchCleanupSnapshot()), ErrConflict)
			require.NoError(t, restarted.ContinueSessionDispatchCleanup(ctx, second, false))

			// A renewal must not roll an already issued suspension back to resume.
			_, err = client.db.Exec(ctx, `UPDATE runtime_lifecycle_job SET available_at = clock_timestamp() - INTERVAL '1 second' WHERE runtime_id = $1 AND kind = $2`, session.Id, RuntimeLifecycleJobRecoverAbandonedDispatch)
			require.NoError(t, err)
			third, err := client.ClaimSessionDispatchCleanup(ctx, "")
			require.NoError(t, err)
			require.Equal(t, suspending, third.Suspending)
			require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, third, true))
			require.NoError(t, client.FinishSessionDispatchCleanup(ctx, third, dispatchCleanupSnapshot()))
			_, err = client.ClaimSessionDispatchCleanup(ctx, "")
			require.ErrorIs(t, err, ErrNotFound)
		})
	}
}

func TestNewDispatchResetsCleanupPhase(t *testing.T) {
	t.Run("Given obsolete cleanup in the suspension phase, when a newer abandoned message is claimed, assert that it gets its own resume phase and owner", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		abandonedDispatchFixture(t, client, session)
		first, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, first, true))
		abandonedDispatchFixture(t, client, session)
		second, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.NotEqual(t, first.DispatchID, second.DispatchID)
		require.NotEqual(t, first.ExecutorID, second.ExecutorID)
		require.False(t, second.Suspending)
		require.ErrorIs(t, client.ContinueSessionDispatchCleanup(ctx, first, true), ErrConflict)
		require.ErrorIs(t, client.FinishSessionDispatchCleanup(ctx, first, dispatchCleanupSnapshot()), ErrConflict)
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, second, false))
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, second, true))
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, second, dispatchCleanupSnapshot()))
	})
}

func TestDispatchCleanupToleratesOldControllerWrites(t *testing.T) {
	t.Run("Given claimed cleanup, when an older controller clears the dispatch, assert that leftover metadata cannot authorize cleanup", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		abandonedDispatchFixture(t, client, session)
		work, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, work, true))

		// Older controllers update Session columns without touching the new
		// cleanup table. Preserve that stale row to exercise mixed versions.
		_, err = client.db.Exec(ctx, `UPDATE session SET dispatch_id = NULL, dispatch_expires_at = NULL WHERE id = $1`, session.Id)
		require.NoError(t, err)
		require.ErrorIs(t, client.ContinueSessionDispatchCleanup(ctx, work, true), ErrConflict)
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()))
		_, err = client.ClaimSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound)
		require.Empty(t, lifecycleJobsForRuntime(t, client, session.Id), "discovery must remove the obsolete job left by the older writer")
		require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, uuid.New(), "new-input"))
	})
	t.Run("Given claimed suspension, when an older controller replaces the dispatch, assert that new cleanup ignores the old owner and phase", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		abandonedDispatchFixture(t, client, session)
		first, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, first, true))
		next := uuid.New()
		// Leave the previous controller's cleanup row while replacing the
		// dispatch, as an older controller would during rollout or rollback.
		_, err = client.db.Exec(ctx, `UPDATE session SET dispatch_id = $2, dispatch_expires_at = clock_timestamp() + interval '1 minute' WHERE id = $1`, session.Id, next)
		require.NoError(t, err)
		require.ErrorIs(t, client.ContinueSessionDispatchCleanup(ctx, first, true), ErrConflict)
		require.ErrorIs(t, client.FinishSessionDispatchCleanup(ctx, first, dispatchCleanupSnapshot()), ErrConflict)
		_, err = client.ClaimSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound, "the replacement is still delivering input")

		// If the new controller later finds that delivery abandoned, the old
		// row must neither delay its claim nor make it skip interrupted resume.
		_, err = client.RevokeSessionDispatch(ctx, session.Id, next, "new-input")
		require.NoError(t, err)
		second, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.Equal(t, next, second.DispatchID)
		require.False(t, second.Suspending)
		require.False(t, second.Suspending)
		require.NotEqual(t, first.ExecutorID, second.ExecutorID)
		require.ErrorIs(t, client.ReleaseSessionDispatchCleanup(ctx, first), ErrConflict)
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, second, false))
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, second, true))
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, second, dispatchCleanupSnapshot()))
	})
	t.Run("Given claimed cleanup, when an older controller leaves a deleted runtime tombstone, assert that discovery removes the obsolete job", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		abandonedDispatchFixture(t, client, session)
		work, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, work, true))
		// Older lifecycle code may retain the runtime row without knowing jobs.
		_, err = client.db.Exec(ctx, `UPDATE runtime_instance SET state = 'RUNTIME_STATE_DELETED', prepared_revision = NULL WHERE id = $1`, session.Id)
		require.NoError(t, err)
		_, err = client.ClaimSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound)
		require.Empty(t, lifecycleJobsForRuntime(t, client, session.Id))
		require.ErrorIs(t, client.ContinueSessionDispatchCleanup(ctx, work, true), ErrConflict)
		state, err := queryOne(ctx, client.db, `SELECT state FROM runtime_instance WHERE id = $1`, pgx.RowTo[string], session.Id)
		require.NoError(t, err, "job retirement must preserve the tombstone")
		require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_DELETED.String(), state)
	})
}

func TestDispatchCleanupRetainsAcceptedWaitingInput(t *testing.T) {
	t.Run("Given input saved on a waiting task, when delivery is revoked before native execution, assert that cleanup preserves receipt and snapshot coverage", func(t *testing.T) {
		client := NewClient(setupTestDB(t), "public")
		ctx := t.Context()
		sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
		session, waiting := waitingTaskFixture(t, client)
		id := uuid.New()
		require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, id, ""))
		_, version, err := client.GetVersionedSessionTask(ctx, session.Id, string(waiting.ID))
		require.NoError(t, err)
		input := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("continue"))
		input.TaskID, input.ContextID = waiting.ID, waiting.ContextID
		waiting.History = append(waiting.History, input)
		version, err = client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("input"), waiting, input, id.String())
		require.NoError(t, err)
		revoked, err := client.RevokeSessionDispatch(ctx, session.Id, id, input.ID)
		require.NoError(t, err)
		require.False(t, revoked, "a persisted receipt must not become retryable")
		revoked, err = client.RevokeSessionDispatch(ctx, session.Id, id, input.ID)
		require.NoError(t, err)
		require.False(t, revoked, "repeating revocation must not make saved input retryable")
		work, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		revoked, err = client.RevokeSessionDispatch(ctx, session.Id, id, input.ID)
		require.NoError(t, err)
		require.False(t, revoked, "claiming cleanup must preserve the input receipt")
		active := *waiting
		active.Status.State = a2a.TaskStateWorking
		_, err = client.UpdateSessionTask(ctx, session.Id, version, taskMutationHash("late active"), &active, &active, id.String())
		require.ErrorIs(t, err, ErrFailedPrecondition)
		_, _, err = client.ReserveSessionCheckpoint(ctx, &apiv1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(waiting.ID)}, session.Creator, uuid.NewString())
		require.ErrorIs(t, err, ErrFailedPrecondition)
		snapshot := dispatchCleanupSnapshot()
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, work, true))
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, snapshot))
		stored, afterVersion, err := client.GetVersionedSessionTask(ctx, session.Id, string(waiting.ID))
		require.NoError(t, err)
		require.Equal(t, version, afterVersion, "cleanup must not append a task event")
		require.Equal(t, a2a.TaskStateInputRequired, stored.Status.State)
		require.Equal(t, input.ID, stored.History[len(stored.History)-1].ID)
		_, err = client.BeginCrashedSessionRecovery(ctx, observedGeneration(t, client, session.Id, "actor-"+session.Id), *snapshot)
		require.NoError(t, err)
		stored, err = client.GetSettledSessionTask(ctx, session.Id, string(waiting.ID), nil)
		require.NoError(t, err)
		require.Equal(t, a2a.TaskStateInputRequired, stored.Status.State, "the latest cleanup snapshot must cover the parked wait")
	})
}

func TestDispatchCleanupTransfersToActiveTask(t *testing.T) {
	t.Run("Given a reserved delivery, when its initial active task commits, assert that dispatch cleanup is retired", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		abandonedDispatchFixture(t, client, session)
		id := uuid.New()
		require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, id, "accepted"))
		task := newSessionTask(uuid.NewString(), "accepted")
		task.ContextID = session.ContextId
		_, err := client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("accepted"), task, id.String())
		require.NoError(t, err)
		revoked, err := client.RevokeSessionDispatch(ctx, session.Id, id, "accepted")
		require.NoError(t, err)
		require.False(t, revoked)
		_, err = client.ClaimSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound)
	})
}

func TestDispatchCleanupCoordinatesTaskQuiescence(t *testing.T) {
	for _, claimed := range []bool{false, true} {
		name := "unclaimed dispatch"
		if claimed {
			name = "claimed dispatch"
		}
		t.Run("Given a "+name+", when task suspension is polled, assert that only dispatch cleanup can suspend", func(t *testing.T) {
			client, session := dispatchCleanupFixture(t)
			ctx := t.Context()
			task := newSessionTask(uuid.NewString(), "completed")
			task.ContextID, task.Status.State = session.ContextId, a2a.TaskStateCompleted
			version, err := client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("completed"), task, "")
			require.NoError(t, err)
			require.NoError(t, client.SettleSessionTask(ctx, session.Id, string(task.ID), version))
			abandonedDispatchFixture(t, client, session)
			var work *SessionDispatchCleanup
			if claimed {
				work, err = client.ClaimSessionDispatchCleanup(ctx, "")
				require.NoError(t, err)
			}
			_, err = client.ClaimSessionQuiescence(ctx)
			require.ErrorIs(t, err, ErrNotFound, "ordinary suspension cannot recover a RESUMING actor")
			if !claimed {
				work, err = client.ClaimSessionDispatchCleanup(ctx, "")
				require.NoError(t, err)
			}
			pending, err := client.HasPendingSessionQuiescence(ctx)
			require.NoError(t, err)
			require.False(t, pending, "claiming dispatch cleanup also retires the marker older controllers use")
			require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, work, true))
			snapshot := dispatchCleanupSnapshot()
			require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, snapshot))
			_, err = client.ClaimSessionQuiescence(ctx)
			require.ErrorIs(t, err, ErrNotFound)
			_, err = client.ClaimSessionDispatchCleanup(ctx, "")
			require.ErrorIs(t, err, ErrNotFound)
			_, afterVersion, err := client.GetVersionedSessionTask(ctx, session.Id, string(task.ID))
			require.NoError(t, err)
			require.Equal(t, version, afterVersion)
			checkpoint, storedSnapshot, err := client.ReserveSessionCheckpoint(ctx,
				&apiv1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID)}, session.Creator, uuid.NewString())
			require.NoError(t, err)
			require.Equal(t, snapshot, storedSnapshot)
			_, err = client.FinalizeSessionCheckpoint(ctx, checkpoint.Id, "cleanup-checkpoint", snapshot.URI, "")
			require.NoError(t, err)
			require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, uuid.New(), "next-message"))
		})
	}
}

func TestDispatchCleanupCoordinatesCheckpoints(t *testing.T) {
	t.Run("Given a completed head with a snapshot and claimed cleanup, when a checkpoint is requested, assert that it captures committed state and cleanup waits for retention", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		task := newSessionTask(uuid.NewString(), "completed")
		task.ContextID, task.Status.State = session.ContextId, a2a.TaskStateCompleted
		require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, dispatchCleanupSnapshot()))
		abandonedDispatchFixture(t, client, session)
		work, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)

		checkpoint := &apiv1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID)}
		requestID := uuid.NewString()
		reserved, captured, err := client.ReserveSessionCheckpoint(ctx, checkpoint, session.Creator, requestID)
		require.NoError(t, err)
		require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_CREATING, reserved.State)
		require.Equal(t, dispatchCleanupSnapshot(), captured)
		require.ErrorIs(t, client.ContinueSessionDispatchCleanup(ctx, work, false), ErrConflict)
		require.ErrorIs(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()), ErrConflict)
		_, err = client.FinalizeSessionCheckpoint(ctx, checkpoint.Id, "retained-before-cleanup", "s3://snapshots/retained-before-cleanup", "")
		require.NoError(t, err)

		const snapshotURI = "s3://snapshots/cleanup-checkpoint"
		snapshot := dispatchCleanupSnapshot()
		snapshot.URI = snapshotURI
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, work, true))
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, snapshot))
		_, captured, err = client.ReserveSessionCheckpoint(ctx, &apiv1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID)}, session.Creator, uuid.NewString())
		require.NoError(t, err)
		require.Equal(t, snapshot, captured, "a later checkpoint must capture the completed cleanup snapshot")
	})
	t.Run("Given a checkpoint being created, when cleanup attempts to claim the actor, assert that it waits for snapshot retention", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		task := newSessionTask(uuid.NewString(), "completed")
		task.ContextID, task.Status.State = session.ContextId, a2a.TaskStateCompleted
		require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, dispatchCleanupSnapshot()))
		candidate := abandonedDispatchFixture(t, client, session)
		checkpoint, _, err := client.ReserveSessionCheckpoint(ctx, &apiv1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID)}, session.Creator, uuid.NewString())
		require.NoError(t, err)

		_, err = client.ClaimSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound, "snapshot retention must finish before cleanup can claim")

		const (
			checkpointTag       = "checkpoint-tag"
			retainedSnapshotURI = "s3://snapshots/checkpoint-retained"
		)
		_, err = client.FinalizeSessionCheckpoint(ctx, checkpoint.Id, checkpointTag, retainedSnapshotURI, "")
		require.NoError(t, err)
		work, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.Equal(t, candidate.DispatchID, work.DispatchID)
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, work, true))
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()))
	})
}

func TestDispatchCleanupCapturesForkedHistory(t *testing.T) {
	t.Run("Given forked history without a task quiescence marker, when an abandoned delivery is cleaned up, assert that the existing history receives its snapshot", func(t *testing.T) {
		client, source := dispatchCleanupFixture(t)
		ctx := t.Context()
		task := newSessionTask(uuid.NewString(), "completed")
		task.ContextID, task.Status.State = source.ContextId, a2a.TaskStateCompleted
		require.NoError(t, saveRuntimeTask(t, client, source.Id, task, task, dispatchCleanupSnapshot()))
		checkpoint, _, err := client.ReserveSessionCheckpoint(ctx, &apiv1.Checkpoint{Id: uuid.NewString(), SessionId: source.Id, HeadTaskId: string(task.ID)}, source.Creator, uuid.NewString())
		require.NoError(t, err)
		_, err = client.FinalizeSessionCheckpoint(ctx, checkpoint.Id, "fork-tag", "s3://snapshots/fork-retained", "")
		require.NoError(t, err)
		fork, _, err := client.ForkSession(ctx, checkpoint.Id, source.Creator, uuid.NewString(), uuid.NewString())
		require.NoError(t, err)
		fork, err = markSessionReady(ctx, client, fork.Id, "fork.example")
		require.NoError(t, err)
		inherited, _, err := client.ListSessionTasks(ctx, fork.Id, "", a2a.TaskStateUnspecified, nil, 10, nil)
		require.NoError(t, err)
		require.Len(t, inherited, 1)
		_, version, err := client.GetVersionedSessionTask(ctx, fork.Id, string(inherited[0].ID))
		require.NoError(t, err)
		abandonedDispatchFixture(t, client, fork)
		work, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		snapshot := dispatchCleanupSnapshot()
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, work, true))
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, snapshot))
		_, captured, err := client.ReserveSessionCheckpoint(ctx, &apiv1.Checkpoint{Id: uuid.NewString(), SessionId: fork.Id, HeadTaskId: string(inherited[0].ID)}, fork.Creator, uuid.NewString())
		require.NoError(t, err)
		require.Equal(t, snapshot, captured)
		_, afterVersion, err := client.GetVersionedSessionTask(ctx, fork.Id, string(inherited[0].ID))
		require.NoError(t, err)
		require.Equal(t, version, afterVersion)
	})
}

func TestDispatchCleanupRetiresAfterRuntimeRecovery(t *testing.T) {
	t.Run("Given issued dispatch cleanup, when crash recovery is requested, assert that recovery supersedes cleanup", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		candidate := abandonedDispatchFixture(t, client, session)
		work, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		_, err = client.BeginCrashedSessionRecovery(ctx, observedGeneration(t, client, session.Id, candidate.ActorUID), SessionTaskSnapshot{})
		require.NoError(t, err)
		current, err := client.GetSessionByID(ctx, session.Id)
		require.NoError(t, err)
		require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_FAILED, current.State)
		require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_REVERT, current.Operation)
		require.ErrorIs(t, client.ContinueSessionDispatchCleanup(ctx, work, true), ErrConflict)
		require.ErrorIs(t, client.ReleaseSessionDispatchCleanup(ctx, work), ErrConflict)
		completeCrashedRecovery(t, client, session.Id, candidate.ActorUID)
		_, err = client.ClaimSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound)
		abandonedDispatchFixture(t, client, session)
		require.ErrorIs(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()), ErrConflict)
		claimed, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, claimed, true))
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, claimed, dispatchCleanupSnapshot()))
	})
	t.Run("Given unclaimed dispatch cleanup, when crash recovery completes, assert that the obligation is retired", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		candidate := abandonedDispatchFixture(t, client, session)
		completeCrashedRecovery(t, client, session.Id, candidate.ActorUID)
		_, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound)
		_, err = client.ClaimSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound)
		require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, uuid.New(), "next-message"))
	})
	t.Run("Given pending cleanup, when explicit suspension and resume complete, assert that the old obligation is retired", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		abandonedDispatchFixture(t, client, session)
		_, err := finishSessionOperation(ctx, client, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND, "")
		require.NoError(t, err)
		_, err = finishSessionOperation(ctx, client, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME, "")
		require.NoError(t, err)
		_, err = client.ClaimSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound)
	})
}

func TestDispatchCleanupReleaseRequiresOwner(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*SessionDispatchCleanup)
	}{
		{name: "another dispatch", change: func(work *SessionDispatchCleanup) { work.DispatchID = uuid.New() }},
		{name: "another executor", change: func(work *SessionDispatchCleanup) { work.ExecutorID = uuid.New() }},
		{name: "another actor", change: func(work *SessionDispatchCleanup) { work.ActorUID = uuid.NewString() }},
	} {
		t.Run("Given claimed cleanup, when "+scenario.name+" releases it, assert that the original worker retains ownership", func(t *testing.T) {
			client, session := dispatchCleanupFixture(t)
			ctx := t.Context()
			candidate := abandonedDispatchFixture(t, client, session)
			work, err := client.ClaimSessionDispatchCleanup(ctx, "")
			require.NoError(t, err)
			mismatch := *work
			scenario.change(&mismatch)
			require.ErrorIs(t, client.ReleaseSessionDispatchCleanup(ctx, &mismatch), ErrConflict)
			require.ErrorIs(t, client.ReleaseSessionDispatchCleanup(ctx, candidate), ErrFailedPrecondition, "an unclaimed candidate cannot release ownership")
			require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, work, false))
			require.NoError(t, client.ReleaseSessionDispatchCleanup(ctx, work))
			pending, err := client.ClaimSessionDispatchCleanup(ctx, "")
			require.NoError(t, err)
			require.Equal(t, work.DispatchID, pending.DispatchID)
			task := newSessionTask(uuid.NewString(), "late-input")
			task.ContextID = session.ContextId
			_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("late-input"), task, work.DispatchID.String())
			require.ErrorIs(t, err, ErrFailedPrecondition, "release must not restore the expired dispatch's execution permission")
		})
	}
	t.Run("Given claimed cleanup, when release names another Session, assert that the original worker retains ownership", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		abandonedDispatchFixture(t, client, session)
		work, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		other, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
		require.NoError(t, err)
		other, err = markSessionReady(ctx, client, other.Id, "other-runtime.example")
		require.NoError(t, err)
		mismatch := *work
		mismatch.Session = other
		require.ErrorIs(t, client.ReleaseSessionDispatchCleanup(ctx, &mismatch), ErrConflict)
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, work, false))
		require.NoError(t, client.ReleaseSessionDispatchCleanup(ctx, work))
		pending, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.Equal(t, work.DispatchID, pending.DispatchID)
	})
}

func TestDispatchCleanupReleaseRejectsStaleOwner(t *testing.T) {
	t.Run("Given cleanup reclaimed after release, when the old owner releases again, assert that the replacement worker retains ownership", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		abandonedDispatchFixture(t, client, session)
		first, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.NoError(t, client.ReleaseSessionDispatchCleanup(ctx, first))
		require.ErrorIs(t, client.ReleaseSessionDispatchCleanup(ctx, first), ErrConflict, "released owners no longer have authority")
		second, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.NotEqual(t, first.ExecutorID, second.ExecutorID)
		require.ErrorIs(t, client.ReleaseSessionDispatchCleanup(ctx, first), ErrConflict)
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, second, false))
		require.ErrorIs(t, client.FinishSessionDispatchCleanup(ctx, first, dispatchCleanupSnapshot()), ErrConflict)
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, second, true))
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, second, dispatchCleanupSnapshot()))
		abandonedDispatchFixture(t, client, session)
		third, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.ErrorIs(t, client.ReleaseSessionDispatchCleanup(ctx, second), ErrConflict, "an old generation cannot release newer cleanup")
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, third, false))
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, third, true))
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, third, dispatchCleanupSnapshot()))
	})
}

func TestDispatchCleanupSkipsLockedSessions(t *testing.T) {
	// Given two abandoned dispatches, with the earlier Session locked by
	// another operation, claiming must leave it for a later scan.
	client, first := dispatchCleanupFixture(t)
	second, _, err := client.CreateSession(t.Context(), newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	second, err = markSessionReady(t.Context(), client, second.Id, "second.example")
	require.NoError(t, err)
	if second.Id < first.Id {
		first, second = second, first
	}
	abandonedDispatchFixture(t, client, first)
	abandonedDispatchFixture(t, client, second)
	tx, err := client.db.BeginTx(t.Context(), pgx.TxOptions{})
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = lockSession(t.Context(), tx, first.Id)
	require.NoError(t, err)

	// When another worker scans, assert that it claims the later Session
	// without waiting for the lock, then recovers the skipped work after release.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	work, err := client.ClaimSessionDispatchCleanup(ctx, "")
	require.NoError(t, err)
	require.Equal(t, second.Id, work.Session.Id)
	require.NoError(t, tx.Rollback(t.Context()))
	next, err := client.ClaimSessionDispatchCleanup(t.Context(), "")
	require.NoError(t, err)
	require.Equal(t, first.Id, next.Session.Id)
}

func TestDispatchCleanupCursorSkipsReleasedActor(t *testing.T) {
	t.Run("Given two pending actors, when a worker releases the first claim, assert that its cursor reaches the next and a new pass revisits the first", func(t *testing.T) {
		client, first := dispatchCleanupFixture(t)
		ctx := t.Context()
		second, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
		require.NoError(t, err)
		second, err = markSessionReady(ctx, client, second.Id, "second.example")
		require.NoError(t, err)
		if second.Id < first.Id {
			first, second = second, first
		}
		for _, session := range []*apiv1.Session{first, second} {
			id := uuid.New()
			require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, id, "pending"))
			_, err := client.RevokeSessionDispatch(ctx, session.Id, id, "pending")
			require.NoError(t, err)
		}
		candidate, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.Equal(t, first.Id, candidate.Session.Id)
		require.NoError(t, client.ReleaseSessionDispatchCleanup(ctx, candidate))
		next, err := client.ClaimSessionDispatchCleanup(ctx, candidate.Session.Id)
		require.NoError(t, err)
		require.Equal(t, second.Id, next.Session.Id)
		require.NoError(t, client.ReleaseSessionDispatchCleanup(ctx, next))
		_, err = client.ClaimSessionDispatchCleanup(ctx, next.Session.Id)
		require.ErrorIs(t, err, ErrNotFound)
		again, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.Equal(t, candidate.DispatchID, again.DispatchID)
		require.NoError(t, client.ReserveSessionDispatch(ctx, first.Id, uuid.New(), "new delivery"))
		next, err = client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.Equal(t, second.Id, next.Session.Id, "a live dispatch is ineligible without an extra retry timestamp")
	})
}

func TestDispatchCleanupLeavesSuccessfulMessagesJobless(t *testing.T) {
	t.Run("Given a ready Session, when a message starts a turn, assert that no recovery job is written", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		dispatchID := uuid.New()
		require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, dispatchID, "accepted"))
		require.Empty(t, lifecycleJobsForRuntime(t, client, session.Id))
		task := newSessionTask(uuid.NewString(), "accepted")
		task.ContextID = session.ContextId
		_, err := client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("accepted"), task, dispatchID.String())
		require.NoError(t, err)
		require.Empty(t, lifecycleJobsForRuntime(t, client, session.Id))
	})
}

func TestDispatchCleanupParksDurably(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		expiredFor time.Duration
		delay      time.Duration
	}{
		{name: "cancellation before expiry", delay: time.Minute},
		{name: "expiry four minutes ago", expiredFor: 4 * time.Minute, delay: time.Minute},
		{name: "expiry five minutes ago", expiredFor: 5 * time.Minute, delay: 5 * time.Minute},
	} {
		t.Run("Given "+scenario.name+", when cleanup parks an already suspended Actor, assert that restarts and repeated discovery preserve its due time", func(t *testing.T) {
			client, session := dispatchCleanupFixture(t)
			ctx := t.Context()
			dispatchID := uuid.New()
			require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, dispatchID, "abandoned"))
			if scenario.expiredFor == 0 {
				_, err := client.RevokeSessionDispatch(ctx, session.Id, dispatchID, "abandoned")
				require.NoError(t, err)
			} else {
				// Simulate a controller that died before revocation, using an old
				// durable expiry rather than waiting through production timeouts.
				_, err := client.db.Exec(ctx, `UPDATE session SET dispatch_expires_at = clock_timestamp() - $2 * interval '1 second' WHERE id = $1`, session.Id, scenario.expiredFor.Seconds())
				require.NoError(t, err)
			}
			work, err := client.ClaimSessionDispatchCleanup(ctx, "")
			require.NoError(t, err)
			before := lifecycleDatabaseTime(t, client)
			require.NoError(t, client.ParkSessionDispatchCleanup(ctx, work))
			after := lifecycleDatabaseTime(t, client)
			parked := lifecycleJobsForRuntime(t, client, session.Id)
			require.Len(t, parked, 1)
			require.Equal(t, dispatchID, parked[0].Generation)
			require.Equal(t, RuntimeLifecycleJobRecovering, parked[0].Phase)
			require.Nil(t, parked[0].ExecutorID, "parking must release worker ownership")
			require.False(t, parked[0].AvailableAt.Before(before.Add(scenario.delay)))
			require.False(t, parked[0].AvailableAt.After(after.Add(scenario.delay)))
			require.ErrorIs(t, client.ContinueSessionDispatchCleanup(ctx, work, false), ErrConflict)
			require.ErrorIs(t, client.ParkSessionDispatchCleanup(ctx, work), ErrConflict)

			restarted := NewClient(sharedDB, "public")
			for range 3 {
				_, err = restarted.ClaimSessionDispatchCleanup(ctx, "")
				require.ErrorIs(t, err, ErrNotFound, "parked work must not be immediately rediscovered")
				_, err = restarted.RevokeSessionDispatch(ctx, session.Id, dispatchID, "abandoned")
				require.NoError(t, err)
			}
			require.Equal(t, parked, lifecycleJobsForRuntime(t, client, session.Id), "neither a scan nor another revoke may reset a parked schedule")
			_, err = restarted.ClaimSessionDispatchCleanup(ctx, "")
			require.ErrorIs(t, err, ErrNotFound, "another scan cannot bypass the stored due time")

			// Advance the due time and let a replacement process claim the job.
			_, err = client.db.Exec(ctx, `UPDATE runtime_lifecycle_job SET available_at = clock_timestamp() WHERE runtime_id = $1 AND kind = $2`, session.Id, RuntimeLifecycleJobRecoverAbandonedDispatch)
			require.NoError(t, err)
			replacement, err := restarted.ClaimSessionDispatchCleanup(ctx, "")
			require.NoError(t, err)
			require.NotEqual(t, work.ExecutorID, replacement.ExecutorID)
			require.False(t, replacement.Suspending)
		})
	}
}

func TestDispatchCleanupParkingPreservesSuspension(t *testing.T) {
	t.Run("Given suspension already authorized, when the owner tries to park, assert that suspension progress and ownership are unchanged", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		abandonedDispatchFixture(t, client, session)
		work, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, work, true))
		before := lifecycleJobsForRuntime(t, client, session.Id)
		require.ErrorIs(t, client.ParkSessionDispatchCleanup(ctx, work), ErrConflict)
		require.Equal(t, before, lifecycleJobsForRuntime(t, client, session.Id))
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()))
	})
}

func TestNewDispatchBypassesParkedCleanup(t *testing.T) {
	t.Run("Given cleanup parked for five minutes, when a newer delivery expires without revocation, assert that recovery replaces the old schedule", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		abandonedDispatchFixture(t, client, session)
		_, err := client.db.Exec(ctx, `UPDATE session SET dispatch_expires_at = clock_timestamp() - interval '10 minutes' WHERE id = $1`, session.Id)
		require.NoError(t, err)
		first, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.NoError(t, client.ParkSessionDispatchCleanup(ctx, first))
		parked := lifecycleJobsForRuntime(t, client, session.Id)
		next := uuid.New()
		require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, next, "next"))
		require.Equal(t, parked, lifecycleJobsForRuntime(t, client, session.Id), "message admission should invalidate through the dispatch without a job write")
		_, err = client.db.Exec(ctx, `UPDATE session SET dispatch_expires_at = clock_timestamp() - interval '1 second' WHERE id = $1`, session.Id)
		require.NoError(t, err)
		restarted := NewClient(sharedDB, "public")
		second, err := restarted.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.Equal(t, next, second.DispatchID)
		require.NotEqual(t, first.ExecutorID, second.ExecutorID)
		require.False(t, second.Suspending)
		require.ErrorIs(t, client.ParkSessionDispatchCleanup(ctx, first), ErrConflict)
	})
}

func TestDispatchCleanupRejectsShortClaims(t *testing.T) {
	t.Run("Given pending recovery, when a worker has only 200 milliseconds left, assert that it changes no ownership", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		abandonedDispatchFixture(t, client, session)
		before := lifecycleJobsForRuntime(t, client, session.Id)
		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer cancel()
		_, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, before, lifecycleJobsForRuntime(t, client, session.Id))

		work, err := client.ClaimSessionDispatchCleanup(t.Context(), "")
		require.NoError(t, err)
		before = lifecycleJobsForRuntime(t, client, session.Id)
		renewalCtx, renewalCancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer renewalCancel()
		require.ErrorIs(t, client.ContinueSessionDispatchCleanup(renewalCtx, work, true), context.DeadlineExceeded)
		require.Equal(t, before, lifecycleJobsForRuntime(t, client, session.Id), "short renewal must preserve both phase and the current takeover deadline")
	})
	for _, scenario := range []struct {
		name    string
		timeout time.Duration
	}{
		{name: "no deadline", timeout: 0},
		{name: "five seconds remaining", timeout: 5 * time.Second},
	} {
		t.Run("Given pending recovery, when the owner claims with "+scenario.name+", assert that takeover is bounded by its attempt", func(t *testing.T) {
			client, session := dispatchCleanupFixture(t)
			abandonedDispatchFixture(t, client, session)
			ctx := context.WithoutCancel(t.Context())
			limit := RuntimeOperationTimeout
			if scenario.timeout != 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(t.Context(), scenario.timeout)
				defer cancel()
				limit = scenario.timeout
			}
			before := lifecycleDatabaseTime(t, client)
			work, err := client.ClaimSessionDispatchCleanup(ctx, "")
			require.NoError(t, err)
			after := lifecycleDatabaseTime(t, client)
			job := lifecycleJobsForRuntime(t, client, session.Id)[0]
			require.Equal(t, work.ExecutorID, *job.ExecutorID)
			require.True(t, job.AvailableAt.After(before.Add(time.Second)))
			require.False(t, job.AvailableAt.After(after.Add(limit+time.Millisecond)))
		})
	}
}

func TestDispatchCleanupClaimsAcrossReplicas(t *testing.T) {
	t.Run("Given one pending recovery job, when eight replicas race to claim it, assert that exactly one receives ownership", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		abandonedDispatchFixture(t, client, session)
		const replicas = 8
		start := make(chan struct{})
		type result struct {
			work *SessionDispatchCleanup
			err  error
		}
		results := make(chan result, replicas)
		for range replicas {
			go func() {
				<-start
				replica := NewClient(sharedDB, "public")
				work, err := replica.ClaimSessionDispatchCleanup(t.Context(), "")
				results <- result{work: work, err: err}
			}()
		}
		close(start)
		var owner *SessionDispatchCleanup
		for range replicas {
			result := <-results
			if result.err != nil {
				require.ErrorIs(t, result.err, ErrNotFound)
				continue
			}
			require.Nil(t, owner, "more than one replica claimed the same job")
			owner = result.work
		}
		require.NotNil(t, owner)
		job := lifecycleJobsForRuntime(t, client, session.Id)[0]
		require.Equal(t, owner.ExecutorID, *job.ExecutorID)
	})
}

func TestDispatchCleanupPreservesFutureLifecycleJobs(t *testing.T) {
	t.Run("Given another scheduled lifecycle kind, when recovery completes, assert that the other job survives message admission and cleanup", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		allowFutureLifecycleJobs(t, client)
		ctx := t.Context()
		_, err := client.db.Exec(ctx, `
			INSERT INTO runtime_lifecycle_job (runtime_id, kind, generation, phase, available_at)
			VALUES ($1, $2, $3, $2, clock_timestamp() + interval '1 hour')
		`, session.Id, futureLifecycleJobValue, uuid.New())
		require.NoError(t, err)
		future := lifecycleJobsForRuntime(t, client, session.Id)
		abandonedDispatchFixture(t, client, session)
		work, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.Len(t, lifecycleJobsForRuntime(t, client, session.Id), 2, "different kinds must coexist on the same runtime")
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, work, true))
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()))
		require.Equal(t, future, lifecycleJobsForRuntime(t, client, session.Id))
		_, err = client.ClaimSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound)
		require.Equal(t, future, lifecycleJobsForRuntime(t, client, session.Id), "obsolete recovery pruning must leave unknown kinds untouched")
	})
	t.Run("Given a different lifecycle kind with a live owner, when recovery attempts to claim, assert that it waits until that owner expires", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		allowFutureLifecycleJobs(t, client)
		ctx := t.Context()
		abandonedDispatchFixture(t, client, session)
		_, err := client.db.Exec(ctx, `
			INSERT INTO runtime_lifecycle_job (runtime_id, kind, generation, phase, available_at, executor_id)
			VALUES ($1, $2, $3, $2, clock_timestamp() + interval '1 minute', $4)
		`, session.Id, futureLifecycleJobValue, uuid.New(), uuid.New())
		require.NoError(t, err)
		before := lifecycleJobsForRuntime(t, client, session.Id)
		_, err = client.ClaimSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound)
		require.Equal(t, before, lifecycleJobsForRuntime(t, client, session.Id))
		_, err = client.db.Exec(ctx, `UPDATE runtime_lifecycle_job SET available_at = clock_timestamp() - interval '1 second' WHERE runtime_id = $1 AND kind = $2`, session.Id, futureLifecycleJobValue)
		require.NoError(t, err)
		_, err = client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
	})
	t.Run("Given a recovery phase introduced by a newer controller, when an older worker scans and revokes again, assert that it preserves unsupported work", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		allowFutureLifecycleJobs(t, client)
		ctx := t.Context()
		candidate := abandonedDispatchFixture(t, client, session)
		_, err := client.db.Exec(ctx, `UPDATE runtime_lifecycle_job SET phase = $2 WHERE runtime_id = $1 AND kind = $3`, session.Id, futureLifecycleJobValue, RuntimeLifecycleJobRecoverAbandonedDispatch)
		require.NoError(t, err)
		before := lifecycleJobsForRuntime(t, client, session.Id)
		_, err = client.ClaimSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound)
		_, err = client.RevokeSessionDispatch(ctx, session.Id, candidate.DispatchID, "abandoned")
		require.NoError(t, err)
		require.Equal(t, before, lifecycleJobsForRuntime(t, client, session.Id))
	})
}

func TestDispatchCleanupCompletionRollsBack(t *testing.T) {
	t.Run("Given a claimed suspension and existing snapshot, when conditional job deletion fails, assert that snapshot and dispatch retirement roll back together", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		task := newSessionTask(uuid.NewString(), "completed")
		task.ContextID, task.Status.State = session.ContextId, a2a.TaskStateCompleted
		previous := dispatchCleanupSnapshot()
		require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, previous))
		abandonedDispatchFixture(t, client, session)
		work, err := client.ClaimSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.NoError(t, client.ContinueSessionDispatchCleanup(ctx, work, true))
		before := lifecycleJobsForRuntime(t, client, session.Id)

		// Force the final conditional DELETE to affect no rows after the
		// transaction has written its snapshot and cleared the dispatch.
		_, err = client.db.Exec(ctx, `
			CREATE FUNCTION test_skip_lifecycle_job_delete() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN RETURN NULL; END $$;
			CREATE TRIGGER test_skip_lifecycle_job_delete BEFORE DELETE ON runtime_lifecycle_job
			FOR EACH ROW EXECUTE FUNCTION test_skip_lifecycle_job_delete()
		`)
		require.NoError(t, err)
		faultInstalled := true
		removeFault := func() {
			if !faultInstalled {
				return
			}
			_, err := client.db.Exec(context.Background(), `
				DROP TRIGGER test_skip_lifecycle_job_delete ON runtime_lifecycle_job;
				DROP FUNCTION test_skip_lifecycle_job_delete()
			`)
			require.NoError(t, err)
			faultInstalled = false
		}
		t.Cleanup(removeFault)
		snapshot := dispatchCleanupSnapshot()
		snapshot.URI = "s3://snapshots/new-cleanup"
		require.ErrorIs(t, client.FinishSessionDispatchCleanup(ctx, work, snapshot), ErrConflict)
		require.Equal(t, before, lifecycleJobsForRuntime(t, client, session.Id))
		row, err := readSession(ctx, client.db, session.Id)
		require.NoError(t, err)
		stored, err := readSessionTask(ctx, client.db, row.HistoryID, string(task.ID))
		require.NoError(t, err)
		require.Equal(t, previous.URI, *stored.SnapshotURI)
		revoked, err := client.RevokeSessionDispatch(ctx, session.Id, work.DispatchID, "abandoned")
		require.NoError(t, err)
		require.True(t, revoked, "failed completion must leave the matching dispatch intact")

		removeFault()
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, snapshot))
		require.Empty(t, lifecycleJobsForRuntime(t, client, session.Id))
		stored, err = readSessionTask(ctx, client.db, row.HistoryID, string(task.ID))
		require.NoError(t, err)
		require.Equal(t, snapshot.URI, *stored.SnapshotURI)
	})
}

const futureLifecycleJobValue = "TEST_FUTURE"

func allowFutureLifecycleJobs(t *testing.T, client *Client) {
	t.Helper()
	// Model the CHECK extension that ships before a future controller rollout.
	_, err := client.db.Exec(t.Context(), `
		ALTER TABLE runtime_lifecycle_job DROP CONSTRAINT runtime_lifecycle_job_phase_check;
		ALTER TABLE runtime_lifecycle_job ADD CONSTRAINT runtime_lifecycle_job_phase_check CHECK (
		    (kind = 'RECOVER_ABANDONED_DISPATCH' AND phase IN ('RECOVERING', 'SUSPENDING', 'TEST_FUTURE'))
		    OR (kind = 'TEST_FUTURE' AND phase = 'TEST_FUTURE'))
	`)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx := context.Background()
		_, err := client.db.Exec(ctx, `DELETE FROM runtime_lifecycle_job WHERE kind = $1 OR phase = $1`, futureLifecycleJobValue)
		require.NoError(t, err)
		_, err = client.db.Exec(ctx, `
			ALTER TABLE runtime_lifecycle_job DROP CONSTRAINT runtime_lifecycle_job_phase_check;
			ALTER TABLE runtime_lifecycle_job ADD CONSTRAINT runtime_lifecycle_job_phase_check CHECK (
			    kind = 'RECOVER_ABANDONED_DISPATCH' AND phase IN ('RECOVERING', 'SUSPENDING'))
		`)
		require.NoError(t, err)
	})
}

type lifecycleJobTestRow struct {
	Kind        string
	Generation  uuid.UUID
	Phase       string
	AvailableAt time.Time
	ExecutorID  *uuid.UUID
}

func lifecycleJobsForRuntime(t *testing.T, client *Client, runtimeID string) []lifecycleJobTestRow {
	t.Helper()
	rows, err := queryMany(t.Context(), client.db, `
		SELECT kind, generation, phase, available_at, executor_id
		FROM runtime_lifecycle_job WHERE runtime_id = $1 ORDER BY kind
	`, pgx.RowToStructByName[lifecycleJobTestRow], runtimeID)
	require.NoError(t, err)
	return rows
}

func lifecycleDatabaseTime(t *testing.T, client *Client) time.Time {
	t.Helper()
	now, err := queryOne(t.Context(), client.db, `SELECT clock_timestamp()`, pgx.RowTo[time.Time])
	require.NoError(t, err)
	return now
}

func dispatchCleanupFixture(t *testing.T) (*Client, *apiv1.Session) {
	t.Helper()
	client := NewClient(setupTestDB(t), "public")
	sessionFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	session, _, err := client.CreateSession(t.Context(), newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	session, err = markSessionReady(t.Context(), client, session.Id, "runtime.example")
	require.NoError(t, err)
	return client, session
}

func abandonedDispatchFixture(t *testing.T, client *Client, session *apiv1.Session) *SessionDispatchCleanup {
	t.Helper()
	id := uuid.New()
	require.NoError(t, client.ReserveSessionDispatch(t.Context(), session.Id, id, "abandoned"))
	_, err := client.RevokeSessionDispatch(t.Context(), session.Id, id, "abandoned")
	require.NoError(t, err)
	return &SessionDispatchCleanup{Session: session, DispatchID: id, ActorUID: "actor-" + session.Id}
}

func dispatchCleanupSnapshot() *SessionTaskSnapshot {
	return &SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/dispatch-cleanup", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}
}
