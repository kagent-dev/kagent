package database

import (
	"crypto/sha256"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
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

func TestDispatchCleanupFencesTasklessSession(t *testing.T) {
	t.Run("Given a restored actor without a turn, when its dispatch is revoked, assert that cleanup fences late execution and preserves empty history", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		id := uuid.New()
		require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, id, "input"))
		_, err := client.GetPendingSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound, "live delivery must not become cleanup")
		revoked, err := client.RevokeSessionDispatch(ctx, session.Id, id, "input")
		require.NoError(t, err)
		require.True(t, revoked)
		revoked, err = client.RevokeSessionDispatch(ctx, session.Id, id, "input")
		require.NoError(t, err)
		require.True(t, revoked, "repeated revocation must preserve proof that input was never accepted")
		task := newSessionTask(uuid.NewString(), "input")
		task.ContextID = session.ContextId
		_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("late"), task, id.String())
		require.ErrorIs(t, err, ErrFailedPrecondition, "revocation fences late input before cleanup claims")
		candidate, err := client.GetPendingSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.Equal(t, id, candidate.DispatchID)
		replaced := *candidate
		replaced.ActorUID = "another-actor"
		_, err = client.ClaimSessionDispatchCleanup(ctx, &replaced)
		require.ErrorIs(t, err, ErrNotFound, "the claim must remain bound to the observed actor")
		work, err := client.ClaimSessionDispatchCleanup(ctx, candidate)
		require.NoError(t, err)
		require.NotEqual(t, uuid.Nil, work.ExecutorID)
		revoked, err = client.RevokeSessionDispatch(ctx, session.Id, id, "input")
		require.NoError(t, err)
		require.True(t, revoked, "repeated revocation remains safe after cleanup is claimed")
		_, err = client.ClaimSessionDispatchCleanup(ctx, candidate)
		require.ErrorIs(t, err, ErrNotFound)
		require.ErrorIs(t, client.ReserveSessionDispatch(ctx, session.Id, uuid.New(), "next"), ErrDispatchBusy)
		for _, token := range []string{id.String(), ""} {
			_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("late"), task, token)
			require.ErrorIs(t, err, ErrFailedPrecondition)
		}
		_, err = client.BeginSessionOperation(ctx, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND, false)
		require.ErrorIs(t, err, ErrFailedPrecondition)
		_, err = client.BeginSessionOperation(ctx, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE, true)
		require.ErrorIs(t, err, ErrConflict)
		require.ErrorIs(t, client.FinishSessionDispatchCleanup(ctx, work, nil), ErrFailedPrecondition)
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()))
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()))
		revoked, err = client.RevokeSessionDispatch(ctx, session.Id, id, "input")
		require.NoError(t, err)
		require.False(t, revoked, "completed cleanup no longer retains the dispatch proof")
		_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("late"), task, id.String())
		require.ErrorIs(t, err, ErrFailedPrecondition, "completed cleanup must not revive the old dispatch")
		_, err = client.GetPendingSessionDispatchCleanup(ctx, "")
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
		// Advance durable deadlines without waiting for the production lease.
		_, err := client.db.Exec(ctx, `UPDATE session SET dispatch_expires_at = clock_timestamp() - INTERVAL '1 second' WHERE id = $1`, session.Id)
		require.NoError(t, err)
		candidate, err := client.GetPendingSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		revoked, err := client.RevokeSessionDispatch(ctx, session.Id, id, "input")
		require.NoError(t, err)
		require.True(t, revoked, "expiry must preserve proof that the input was never accepted")
		work, err := client.ClaimSessionDispatchCleanup(ctx, candidate)
		require.NoError(t, err)
		task := newSessionTask(uuid.NewString(), "input")
		task.ContextID = session.ContextId
		_, err = client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("late"), task, id.String())
		require.ErrorIs(t, err, ErrFailedPrecondition)
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()))
	})
}

func TestDispatchCleanupRejectsStalePreparation(t *testing.T) {
	t.Run("Given a replacement delivery, when old cleanup preparation finishes, assert that it cannot claim or retire the newer obligation", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		first := abandonedDispatchFixture(t, client, session)
		second := abandonedDispatchFixture(t, client, session)
		require.NotEqual(t, first.DispatchID, second.DispatchID)
		_, err := client.ClaimSessionDispatchCleanup(ctx, first)
		require.ErrorIs(t, err, ErrNotFound)
		revoked, err := client.RevokeSessionDispatch(ctx, session.Id, first.DispatchID, "input")
		require.NoError(t, err)
		require.False(t, revoked)
		work, err := client.ClaimSessionDispatchCleanup(ctx, second)
		require.NoError(t, err)
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()))
		third := abandonedDispatchFixture(t, client, session)
		require.ErrorIs(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()), ErrConflict)
		pending, err := client.GetPendingSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.Equal(t, third.DispatchID, pending.DispatchID)
		require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, uuid.New(), "next"), "unclaimed cleanup must not block sends")
	})
}

func TestDispatchCleanupRacesNewDelivery(t *testing.T) {
	t.Run("Given pending cleanup, when a new delivery and cleanup claim race, assert that exactly one acquires execution authority", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		for range 12 {
			candidate := abandonedDispatchFixture(t, client, session)
			start := make(chan struct{})
			claimed := make(chan *SessionDispatchCleanup, 1)
			claimErrors, sendErrors := make(chan error, 1), make(chan error, 1)
			next := uuid.New()
			go func() {
				<-start
				work, err := client.ClaimSessionDispatchCleanup(ctx, candidate)
				claimed <- work
				claimErrors <- err
			}()
			go func() {
				<-start
				sendErrors <- client.ReserveSessionDispatch(ctx, session.Id, next, "next")
			}()
			close(start)
			work, claimErr, sendErr := <-claimed, <-claimErrors, <-sendErrors
			if claimErr == nil {
				require.ErrorIs(t, sendErr, ErrDispatchBusy)
				require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()))
			} else {
				require.ErrorIs(t, claimErr, ErrNotFound)
				require.NoError(t, sendErr)
				_, err := client.RevokeSessionDispatch(ctx, session.Id, next, "next")
				require.NoError(t, err)
			}
		}
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
		candidate, err := client.GetPendingSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		work, err := client.ClaimSessionDispatchCleanup(ctx, candidate)
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
		candidate := abandonedDispatchFixture(t, client, session)
		id := uuid.New()
		require.NoError(t, client.ReserveSessionDispatch(ctx, session.Id, id, "accepted"))
		task := newSessionTask(uuid.NewString(), "accepted")
		task.ContextID = session.ContextId
		_, err := client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("accepted"), task, id.String())
		require.NoError(t, err)
		revoked, err := client.RevokeSessionDispatch(ctx, session.Id, id, "accepted")
		require.NoError(t, err)
		require.False(t, revoked)
		_, err = client.ClaimSessionDispatchCleanup(ctx, candidate)
		require.ErrorIs(t, err, ErrNotFound)
		_, err = client.GetPendingSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound)
	})
}

func TestDispatchCleanupCoordinatesTaskQuiescence(t *testing.T) {
	for _, taskFirst := range []bool{false, true} {
		name := "Given settled task work, when dispatch cleanup claims first, assert that one snapshot satisfies both obligations"
		if taskFirst {
			name = "Given abandoned dispatch work, when task quiescence claims first, assert that cleanup waits and later retires safely"
		}
		t.Run(name, func(t *testing.T) {
			client, session := dispatchCleanupFixture(t)
			ctx := t.Context()
			task := newSessionTask(uuid.NewString(), "completed")
			task.ContextID, task.Status.State = session.ContextId, a2a.TaskStateCompleted
			version, err := client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("completed"), task, "")
			require.NoError(t, err)
			require.NoError(t, client.SettleSessionTask(ctx, session.Id, string(task.ID), version))
			candidate := abandonedDispatchFixture(t, client, session)
			if taskFirst {
				normal, err := client.ClaimSessionQuiescence(ctx)
				require.NoError(t, err)
				_, err = client.ClaimSessionDispatchCleanup(ctx, candidate)
				require.ErrorIs(t, err, ErrNotFound)
				require.NoError(t, client.FinishSessionQuiescence(ctx, normal, dispatchCleanupSnapshot()))
			}
			work, err := client.ClaimSessionDispatchCleanup(ctx, candidate)
			require.NoError(t, err)
			_, err = client.ClaimSessionQuiescence(ctx)
			require.ErrorIs(t, err, ErrNotFound)
			require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()))
			_, err = client.ClaimSessionQuiescence(ctx)
			require.ErrorIs(t, err, ErrNotFound)
			_, err = client.GetPendingSessionDispatchCleanup(ctx, "")
			require.ErrorIs(t, err, ErrNotFound)
			_, afterVersion, err := client.GetVersionedSessionTask(ctx, session.Id, string(task.ID))
			require.NoError(t, err)
			require.Equal(t, version, afterVersion)
		})
	}
}

func TestDispatchCleanupCoordinatesCheckpoints(t *testing.T) {
	t.Run("Given a completed head with a snapshot and claimed cleanup, when a checkpoint is requested, assert that it waits for the new snapshot", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		task := newSessionTask(uuid.NewString(), "completed")
		task.ContextID, task.Status.State = session.ContextId, a2a.TaskStateCompleted
		require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, dispatchCleanupSnapshot()))
		candidate := abandonedDispatchFixture(t, client, session)
		work, err := client.ClaimSessionDispatchCleanup(ctx, candidate)
		require.NoError(t, err)

		checkpoint := &apiv1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: string(task.ID)}
		requestID := uuid.NewString()
		_, _, err = client.ReserveSessionCheckpoint(ctx, checkpoint, session.Creator, requestID)
		require.ErrorIs(t, err, ErrSnapshotPending, "an older valid snapshot must not bypass issued cleanup")

		const snapshotURI = "s3://snapshots/cleanup-checkpoint"
		snapshot := dispatchCleanupSnapshot()
		snapshot.URI = snapshotURI
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, work, snapshot))
		reserved, captured, err := client.ReserveSessionCheckpoint(ctx, checkpoint, session.Creator, requestID)
		require.NoError(t, err)
		require.Equal(t, apiv1.CheckpointState_CHECKPOINT_STATE_CREATING, reserved.State)
		require.Equal(t, snapshot, captured, "retry must capture the completed cleanup snapshot")
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

		_, err = client.GetPendingSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound)
		_, err = client.ClaimSessionDispatchCleanup(ctx, candidate)
		require.ErrorIs(t, err, ErrNotFound, "preparation before checkpoint creation must not grant runtime authority")

		const (
			checkpointTag       = "checkpoint-tag"
			retainedSnapshotURI = "s3://snapshots/checkpoint-retained"
		)
		_, err = client.FinalizeSessionCheckpoint(ctx, checkpoint.Id, checkpointTag, retainedSnapshotURI, "")
		require.NoError(t, err)
		pending, err := client.GetPendingSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.Equal(t, candidate.DispatchID, pending.DispatchID)
		work, err := client.ClaimSessionDispatchCleanup(ctx, pending)
		require.NoError(t, err)
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
		candidate := abandonedDispatchFixture(t, client, fork)
		work, err := client.ClaimSessionDispatchCleanup(ctx, candidate)
		require.NoError(t, err)
		snapshot := dispatchCleanupSnapshot()
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
	t.Run("Given issued dispatch cleanup, when the same actor is reverted after a confirmed crash, assert that stale cleanup cannot claim the recovered runtime", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		candidate := abandonedDispatchFixture(t, client, session)
		work, err := client.ClaimSessionDispatchCleanup(ctx, candidate)
		require.NoError(t, err)
		completeCrashedRecovery(t, client, session.Id, candidate.ActorUID)
		_, err = client.GetPendingSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound)
		next := abandonedDispatchFixture(t, client, session)
		_, err = client.ClaimSessionDispatchCleanup(ctx, candidate)
		require.ErrorIs(t, err, ErrNotFound)
		require.ErrorIs(t, client.FinishSessionDispatchCleanup(ctx, work, dispatchCleanupSnapshot()), ErrConflict)
		claimed, err := client.ClaimSessionDispatchCleanup(ctx, next)
		require.NoError(t, err)
		require.NoError(t, client.FinishSessionDispatchCleanup(ctx, claimed, dispatchCleanupSnapshot()))
	})
	t.Run("Given pending cleanup, when explicit suspension and resume complete, assert that the old obligation is retired", func(t *testing.T) {
		client, session := dispatchCleanupFixture(t)
		ctx := t.Context()
		candidate := abandonedDispatchFixture(t, client, session)
		_, err := finishSessionOperation(ctx, client, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND, "")
		require.NoError(t, err)
		_, err = finishSessionOperation(ctx, client, session.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME, "")
		require.NoError(t, err)
		_, err = client.GetPendingSessionDispatchCleanup(ctx, "")
		require.ErrorIs(t, err, ErrNotFound)
		_, err = client.ClaimSessionDispatchCleanup(ctx, candidate)
		require.ErrorIs(t, err, ErrNotFound)
	})
}

func TestDispatchCleanupCursorSkipsUnreadyActor(t *testing.T) {
	t.Run("Given two pending actors, when a worker leaves the first unclaimed, assert that its cursor reaches the next and a new pass revisits the first", func(t *testing.T) {
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
		candidate, err := client.GetPendingSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.Equal(t, first.Id, candidate.Session.Id)
		next, err := client.GetPendingSessionDispatchCleanup(ctx, candidate.Session.Id)
		require.NoError(t, err)
		require.Equal(t, second.Id, next.Session.Id)
		_, err = client.GetPendingSessionDispatchCleanup(ctx, next.Session.Id)
		require.ErrorIs(t, err, ErrNotFound)
		again, err := client.GetPendingSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.Equal(t, candidate.DispatchID, again.DispatchID)
		require.NoError(t, client.ReserveSessionDispatch(ctx, first.Id, uuid.New(), "new delivery"))
		next, err = client.GetPendingSessionDispatchCleanup(ctx, "")
		require.NoError(t, err)
		require.Equal(t, second.Id, next.Session.Id, "a live dispatch is ineligible without an extra retry timestamp")
	})
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
	work, err := client.GetPendingSessionDispatchCleanup(t.Context(), "")
	require.NoError(t, err)
	require.Equal(t, id, work.DispatchID)
	return work
}

func dispatchCleanupSnapshot() *SessionTaskSnapshot {
	return &SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/dispatch-cleanup", Fidelity: "SNAPSHOT_FIDELITY_VOLUMES"}
}
