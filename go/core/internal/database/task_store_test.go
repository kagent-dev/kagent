package database

import (
	"crypto/sha256"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
)

func TestTaskEventsRequireTaskIdentity(t *testing.T) {
	client := NewClient(setupTestDB(t))
	agentInstanceFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	_, task := waitingTaskFixture(t, client)
	_, err := client.db.Exec(t.Context(), "UPDATE agent_instance_task_event SET task_id = NULL WHERE task_id = $1", string(task.ID))
	require.ErrorContains(t, err, "violates not-null constraint")
}

func TestRuntimeTaskSaveRetriesAndVersions(t *testing.T) {
	client := NewClient(setupTestDB(t))
	agentInstanceFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	instance, waiting := waitingTaskFixture(t, client)
	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("PostgreSQL"))
	reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
	resumeRuntimeTask(t, client, instance.Id, reply)
	task, initialVersion, err := client.GetVersionedAgentInstanceTask(t.Context(), instance.Id, string(waiting.ID))
	require.NoError(t, err)
	require.Positive(t, initialVersion)
	require.Len(t, task.History, 3)

	task.Status = a2a.TaskStatus{State: a2a.TaskStateWorking}
	firstHash := sha256.Sum256([]byte("working"))
	firstVersion, err := client.UpdateAgentInstanceTask(t.Context(), instance.Id, initialVersion, firstHash[:], task, task, "")
	require.NoError(t, err)
	require.Greater(t, firstVersion, initialVersion)

	completed := *task
	completed.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted}
	secondHash := sha256.Sum256([]byte("completed"))
	secondVersion, err := client.UpdateAgentInstanceTask(t.Context(), instance.Id, firstVersion, secondHash[:], &completed, &completed, "")
	require.NoError(t, err)
	require.Greater(t, secondVersion, firstVersion)

	// A response lost before the next update remains recoverable afterward.
	replayedVersion, err := client.UpdateAgentInstanceTask(t.Context(), instance.Id, initialVersion, firstHash[:], task, task, "")
	require.NoError(t, err)
	require.Equal(t, firstVersion, replayedVersion)
	_, err = client.UpdateAgentInstanceTask(t.Context(), instance.Id, initialVersion, secondHash[:], &completed, &completed, "")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.UpdateAgentInstanceTask(t.Context(), instance.Id, secondVersion, firstHash[:], task, task, "")
	require.ErrorIs(t, err, ErrFailedPrecondition)
	stored, version, err := client.GetVersionedAgentInstanceTask(t.Context(), instance.Id, string(task.ID))
	require.NoError(t, err)
	require.Equal(t, secondVersion, version)
	require.Equal(t, a2a.TaskStateCompleted, stored.Status.State)
	require.Len(t, stored.History, 3)
}

func TestConcurrentRuntimeTaskSaves(t *testing.T) {
	client := NewClient(setupTestDB(t))
	agentInstanceFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	instance, waiting := waitingTaskFixture(t, client)
	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("PostgreSQL"))
	reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
	resumeRuntimeTask(t, client, instance.Id, reply)
	task, version, err := client.GetVersionedAgentInstanceTask(t.Context(), instance.Id, string(waiting.ID))
	require.NoError(t, err)
	start, results := make(chan struct{}), make(chan error, 2)
	for _, text := range []string{"one", "two"} {
		go func() {
			update := *task
			update.Status = a2a.TaskStatus{State: a2a.TaskStateWorking, Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart(text))}
			hash := sha256.Sum256([]byte(text))
			<-start
			_, err := client.UpdateAgentInstanceTask(t.Context(), instance.Id, version, hash[:], &update, &update, "")
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
	client := NewClient(setupTestDB(t))
	agentInstanceFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	instance, waiting := waitingTaskFixture(t, client)
	task, version, err := client.GetVersionedAgentInstanceTask(t.Context(), instance.Id, string(waiting.ID))
	require.NoError(t, err)
	hash := sha256.Sum256([]byte("update"))
	_, _, err = client.GetVersionedAgentInstanceTask(t.Context(), uuid.NewString(), string(task.ID))
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.UpdateAgentInstanceTask(t.Context(), uuid.NewString(), version, hash[:], task, task, "")
	require.ErrorIs(t, err, ErrNotFound)
	wrongScope := *task
	wrongScope.ContextID = uuid.NewString()
	_, err = client.UpdateAgentInstanceTask(t.Context(), instance.Id, version, hash[:], &wrongScope, &wrongScope, "")
	require.ErrorIs(t, err, ErrFailedPrecondition)

	// Cancellation is allowed while waiting, but malformed event identity must
	// roll back the entire save, including any archived status message.
	canceled := *task
	canceled.Status = a2a.TaskStatus{State: a2a.TaskStateCanceled}
	_, err = client.UpdateAgentInstanceTask(t.Context(), instance.Id, version, hash[:], &canceled, &wrongScope, "")
	require.Error(t, err)
	stored, after, err := client.GetVersionedAgentInstanceTask(t.Context(), instance.Id, string(task.ID))
	require.NoError(t, err)
	require.Equal(t, version, after)
	require.Equal(t, a2a.TaskStateInputRequired, stored.Status.State)
	require.Len(t, stored.History, len(task.History))
}

func TestRuntimeTaskCreateRetries(t *testing.T) {
	client := NewClient(setupTestDB(t))
	agentInstanceFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	instance, _ := waitingTaskFixture(t, client)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("new task"))
	message.ContextID = instance.ContextId
	task := a2a.NewSubmittedTask(message, message)
	hash := sha256.Sum256([]byte("create"))
	version, err := client.CreateRuntimeTask(t.Context(), instance.Id, hash[:], task, "")
	require.NoError(t, err)
	retry, err := client.CreateRuntimeTask(t.Context(), instance.Id, hash[:], task, "")
	require.NoError(t, err)
	require.Equal(t, version, retry)
	different := sha256.Sum256([]byte("different create"))
	_, err = client.CreateRuntimeTask(t.Context(), instance.Id, different[:], task, "")
	require.ErrorIs(t, err, ErrIdempotencyConflict)
	working := *task
	working.Status.State = a2a.TaskStateWorking
	_, err = client.UpdateAgentInstanceTask(t.Context(), instance.Id, version, different[:], &working, &working, "")
	require.NoError(t, err)
	retry, err = client.CreateRuntimeTask(t.Context(), instance.Id, hash[:], task, "")
	require.NoError(t, err)
	require.Equal(t, version, retry)
}

// resumeRuntimeTask models the SDK's ordinary read, append-input, and working save.
func resumeRuntimeTask(t *testing.T, client *Client, instanceID string, reply *a2a.Message) (*a2a.Task, int64) {
	t.Helper()
	task, version, err := client.GetVersionedAgentInstanceTask(t.Context(), instanceID, string(reply.TaskID))
	require.NoError(t, err)
	if task.Status.Message != nil {
		task.History = append(task.History, task.Status.Message)
	}
	task.History = append(task.History, reply)
	hash := sha256.Sum256([]byte("input " + reply.ID))
	version, err = client.UpdateAgentInstanceTask(t.Context(), instanceID, version, hash[:], task, task, "")
	require.NoError(t, err)
	task.Status = a2a.TaskStatus{State: a2a.TaskStateWorking}
	hash = sha256.Sum256([]byte("start " + reply.ID))
	version, err = client.UpdateAgentInstanceTask(t.Context(), instanceID, version, hash[:], task, task, "")
	require.NoError(t, err)
	return task, version
}

func TestRuntimeCompletionDoesNotWaitForSnapshot(t *testing.T) {
	client := NewClient(setupTestDB(t))
	agentInstanceFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	instance, waiting := waitingTaskFixture(t, client)
	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("PostgreSQL"))
	reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
	current, initialVersion := resumeRuntimeTask(t, client, instance.Id, reply)
	finished := *current
	finished.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted, Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("done"))}
	hash := sha256.Sum256([]byte("complete"))
	version, err := client.UpdateAgentInstanceTask(t.Context(), instance.Id, initialVersion, hash[:], &finished, &finished, "")
	require.NoError(t, err)
	private, privateVersion, err := client.GetVersionedAgentInstanceTask(t.Context(), instance.Id, string(waiting.ID))
	require.NoError(t, err)
	require.Equal(t, version, privateVersion)
	require.Equal(t, a2a.TaskStateCompleted, private.Status.State)
	public, err := client.GetAgentInstanceTask(t.Context(), instance.Id, string(waiting.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateWorking, public.Status.State)
	_, err = client.GetSettledAgentInstanceTask(t.Context(), instance.Id, string(waiting.ID), nil)
	require.ErrorIs(t, err, ErrConflict)
	require.ErrorIs(t, deleteInstance(t.Context(), client, instance.Id), ErrFailedPrecondition)
	_, err = client.ClaimInstanceQuiescence(t.Context())
	require.ErrorIs(t, err, ErrNotFound) // The native cleanup callback has not finished.
	require.NoError(t, client.SettleAgentInstanceTask(t.Context(), instance.Id, string(waiting.ID), version))
	public, err = client.GetSettledAgentInstanceTask(t.Context(), instance.Id, string(waiting.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, public.Status.State)
	require.Len(t, public.History, 3)
	work, err := client.ClaimInstanceQuiescence(t.Context())
	require.NoError(t, err)
	require.Equal(t, version, work.Version)
	_, err = client.ClaimInstanceQuiescence(t.Context())
	require.ErrorIs(t, err, ErrNotFound) // Claims cannot expire into a second suspend.
	require.Error(t, client.FinishInstanceQuiescence(t.Context(), work, nil))
	// Even an unfinished or failed snapshot never hides the completed task.
	public, err = client.GetSettledAgentInstanceTask(t.Context(), instance.Id, string(waiting.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, public.Status.State)
	require.ErrorIs(t, deleteInstance(t.Context(), client, instance.Id), ErrFailedPrecondition)

	fresh := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("next"))
	fresh.ContextID = instance.ContextId
	_, err = client.CreateRuntimeTask(t.Context(), instance.Id, hash[:], a2a.NewSubmittedTask(fresh, fresh), "")
	require.ErrorIs(t, err, ErrFailedPrecondition)
	snapshot := &AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "s3://snapshot/exact", ContentScope: "DATA"}
	require.NoError(t, client.FinishInstanceQuiescence(t.Context(), work, snapshot))
	require.NoError(t, client.FinishInstanceQuiescence(t.Context(), work, snapshot))
	require.NoError(t, client.SettleAgentInstanceTask(t.Context(), instance.Id, string(waiting.ID), version))
	public, err = client.GetAgentInstanceTask(t.Context(), instance.Id, string(waiting.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateCompleted, public.Status.State)
	require.Len(t, public.History, 3)
	_, err = client.ClaimInstanceQuiescence(t.Context())
	require.ErrorIs(t, err, ErrNotFound)
}

func TestRuntimeForkRetainsOnlyTheCheckpointBoundary(t *testing.T) {
	client := NewClient(setupTestDB(t))
	agentInstanceFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	source, waiting := waitingTaskFixture(t, client)
	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("PostgreSQL"))
	reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
	completed, initialVersion := resumeRuntimeTask(t, client, source.Id, reply)
	completed.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted}
	hash := sha256.Sum256([]byte("checkpoint boundary"))
	version, err := client.UpdateAgentInstanceTask(t.Context(), source.Id, initialVersion, hash[:], completed, completed, "")
	require.NoError(t, err)
	_, _, err = client.ReserveAgentInstanceCheckpoint(t.Context(), &apiv1alpha1.Checkpoint{Id: uuid.NewString(), AgentInstanceId: source.Id, HeadTaskId: string(completed.ID)}, "alice", uuid.NewString())
	require.ErrorIs(t, err, ErrFailedPrecondition)
	require.NoError(t, client.SettleAgentInstanceTask(t.Context(), source.Id, string(completed.ID), version))
	boundary, err := client.ClaimInstanceQuiescence(t.Context())
	require.NoError(t, err)
	require.NoError(t, client.FinishInstanceQuiescence(t.Context(), boundary, &AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "turn-N", ContentScope: "DATA"}))
	checkpoint, _, err := client.ReserveAgentInstanceCheckpoint(t.Context(), &apiv1alpha1.Checkpoint{Id: uuid.NewString(), AgentInstanceId: source.Id, HeadTaskId: string(completed.ID)}, "alice", uuid.NewString())
	require.NoError(t, err)
	_, err = client.FinalizeAgentInstanceCheckpoint(t.Context(), checkpoint.Id, "tag-N", "retained-N", "")
	require.NoError(t, err)
	newInput := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("source after checkpoint"))
	newInput.ContextID = source.ContextId
	later := a2a.NewSubmittedTask(newInput, newInput)
	_, err = client.CreateRuntimeTask(t.Context(), source.Id, hash[:], later, "")
	require.NoError(t, err)
	fork, _, err := client.ForkAgentInstance(t.Context(), checkpoint.Id, "alice", uuid.NewString(), uuid.NewString())
	require.NoError(t, err)
	fork, err = markAgentInstanceReady(t.Context(), client, fork.Id, "fork.example")
	require.NoError(t, err)
	forkTasks, _, err := client.ListAgentInstanceTasks(t.Context(), fork.Id, "", a2a.TaskStateUnspecified, nil, 10, nil)
	require.NoError(t, err)
	require.Len(t, forkTasks, 1)
	inherited, forkVersion, err := client.GetVersionedAgentInstanceTask(t.Context(), fork.Id, string(forkTasks[0].ID))
	require.NoError(t, err)
	require.Equal(t, fork.Id, inherited.ContextID)
	require.NotEqual(t, completed.ID, inherited.ID)
	require.Equal(t, a2a.TaskStateCompleted, inherited.Status.State)
	require.NotEqual(t, version, forkVersion)
	// Public task IDs resolve to exactly one conversation, including copies
	// whose historical positions are equal to their source's positions.
	for _, pair := range []struct{ taskID, instanceID string }{
		{string(completed.ID), source.Id}, {string(inherited.ID), fork.Id},
	} {
		owner, err := client.AgentInstanceForTask(t.Context(), pair.taskID)
		require.NoError(t, err)
		require.Equal(t, pair.instanceID, owner)
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
	_, _, err = client.GetVersionedAgentInstanceTask(t.Context(), fork.Id, string(later.ID))
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.UpdateAgentInstanceTask(t.Context(), fork.Id, initialVersion, hash[:], inherited, inherited, "")
	require.ErrorIs(t, err, ErrConflict, "source mutation receipts must not grant fork writes")
	newInput.ID = "fork-input"
	newInput.ContextID = fork.Id
	newInput.Parts = a2a.ContentParts{a2a.NewTextPart("independent fork")}
	forkInput := a2a.NewSubmittedTask(newInput, newInput)
	_, err = client.CreateRuntimeTask(t.Context(), fork.Id, hash[:], forkInput, "")
	require.NoError(t, err)
	_, err = client.GetAgentInstanceTask(t.Context(), source.Id, string(forkInput.ID), nil)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestNewExecutionRacesIdleClaim(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	for range 12 {
		instance, waiting := waitingTaskFixture(t, client)
		reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("continue"))
		reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
		task, version := resumeRuntimeTask(t, client, instance.Id, reply)
		task.Status.State = a2a.TaskStateCompleted
		version, err := client.UpdateAgentInstanceTask(ctx, instance.Id, version, taskMutationHash("finish"), task, task, "")
		require.NoError(t, err)
		require.NoError(t, client.SettleAgentInstanceTask(ctx, instance.Id, string(task.ID), version))

		start := make(chan struct{})
		claimed := make(chan *InstanceQuiescence, 1)
		claimErrors, writeErrors := make(chan error, 1), make(chan error, 1)
		go func() {
			<-start
			work, err := client.ClaimInstanceQuiescence(ctx)
			claimed <- work
			claimErrors <- err
		}()
		next := newAgentInstanceTask(uuid.NewString(), "next-message")
		next.ContextID = instance.ContextId
		dispatchID := uuid.New()
		go func() {
			<-start
			err := client.ReserveAgentInstanceDispatch(ctx, instance.Id, dispatchID, "")
			writeErrors <- err
		}()
		close(start)
		work, claimErr, writeErr := <-claimed, <-claimErrors, <-writeErrors
		if claimErr == nil {
			require.ErrorIs(t, writeErr, ErrDispatchBusy)
			require.NoError(t, client.FinishInstanceQuiescence(ctx, work, &AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "snapshot", ContentScope: "DATA"}))
			require.NoError(t, client.ReserveAgentInstanceDispatch(ctx, instance.Id, dispatchID, ""))
		} else {
			require.ErrorIs(t, claimErr, ErrNotFound)
			require.NoError(t, writeErr)
		}
		_, err = client.ClaimInstanceQuiescence(ctx)
		require.ErrorIs(t, err, ErrNotFound, "dispatch must protect the network gap before persistence")
		_, err = client.CreateRuntimeTask(ctx, instance.Id, taskMutationHash("next"), next, dispatchID.String())
		require.NoError(t, err)
		// A lost cleanup acknowledgement cannot requeue an obsolete suspension.
		require.NoError(t, client.SettleAgentInstanceTask(ctx, instance.Id, string(task.ID), version))
		_, err = client.ClaimInstanceQuiescence(ctx)
		require.ErrorIs(t, err, ErrNotFound)
		visible, err := client.GetSettledAgentInstanceTask(ctx, instance.Id, string(task.ID), nil)
		require.NoError(t, err)
		require.Equal(t, a2a.TaskStateCompleted, visible.Status.State)
	}
}

func TestRuntimeTaskLookupRejectsAmbiguousMessageIDs(t *testing.T) {
	client := NewClient(setupTestDB(t))
	agentInstanceFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	instance, waiting := waitingTaskFixture(t, client)
	messageID := waiting.History[0].ID
	found, err := client.GetAgentInstanceTaskByMessage(t.Context(), instance.Id, "", messageID)
	require.NoError(t, err)
	require.Equal(t, waiting.ID, found.ID)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("another task"))
	message.ID, message.ContextID = messageID, instance.ContextId
	task := a2a.NewSubmittedTask(message, message)
	_, err = client.CreateRuntimeTask(t.Context(), instance.Id, taskMutationHash("another task"), task, "")
	require.NoError(t, err)
	_, err = client.GetAgentInstanceTaskByMessage(t.Context(), instance.Id, "", messageID)
	require.ErrorIs(t, err, ErrConflict)
	found, err = client.GetAgentInstanceTaskByMessage(t.Context(), instance.Id, string(task.ID), messageID)
	require.NoError(t, err)
	require.Equal(t, task.ID, found.ID)
	_, err = client.GetAgentInstanceTaskByMessage(t.Context(), uuid.NewString(), "", messageID)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestDispatchFencesIdleWorkAndLateAcceptance(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	instance, waiting := waitingTaskFixture(t, client)
	dispatchID := uuid.New()
	require.NoError(t, client.ReserveAgentInstanceDispatch(ctx, instance.Id, dispatchID, ""))
	_, err := client.ClaimInstanceQuiescence(ctx)
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, client.ReserveAgentInstanceDispatch(ctx, instance.Id, uuid.New(), ""), ErrDispatchBusy)
	revoked, err := client.RevokeAgentInstanceDispatch(ctx, instance.Id, dispatchID, "new-input")
	require.NoError(t, err)
	require.True(t, revoked)
	_, version, err := client.GetVersionedAgentInstanceTask(ctx, instance.Id, string(waiting.ID))
	require.NoError(t, err)
	resumed := *waiting
	resumed.Status.State = a2a.TaskStateWorking
	_, err = client.UpdateAgentInstanceTask(ctx, instance.Id, version, taskMutationHash("late"), &resumed, &resumed, dispatchID.String())
	require.ErrorIs(t, err, ErrFailedPrecondition)

	dispatchID = uuid.New()
	require.NoError(t, client.ReserveAgentInstanceDispatch(ctx, instance.Id, dispatchID, ""))
	// SDKs may first append input while retaining the waiting state.
	version, err = client.UpdateAgentInstanceTask(ctx, instance.Id, version, taskMutationHash("input-only"), waiting, waiting, dispatchID.String())
	require.NoError(t, err)
	_, err = client.ClaimInstanceQuiescence(ctx)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.UpdateAgentInstanceTask(ctx, instance.Id, version, taskMutationHash("active"), &resumed, &resumed, dispatchID.String())
	require.NoError(t, err)
	revoked, err = client.RevokeAgentInstanceDispatch(ctx, instance.Id, dispatchID, "new-input")
	require.NoError(t, err)
	require.False(t, revoked, "accepted work must never be reported retryable")
	require.ErrorIs(t, client.ReserveAgentInstanceDispatch(ctx, instance.Id, uuid.New(), ""), ErrConflict)
}

func TestDispatchExpiryRejectsLateSave(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	instance, waiting := waitingTaskFixture(t, client)
	id := uuid.New()
	require.NoError(t, client.ReserveAgentInstanceDispatch(ctx, instance.Id, id, ""))
	_, err := client.db.Exec(ctx, `UPDATE agent_instance SET dispatch_expires_at = clock_timestamp() - INTERVAL '1 second' WHERE id = $1`, instance.Id)
	require.NoError(t, err)
	next := newAgentInstanceTask(uuid.NewString(), "next-message")
	next.ContextID = instance.ContextId
	_, err = client.CreateRuntimeTask(ctx, instance.Id, taskMutationHash("expired"), next, id.String())
	require.ErrorIs(t, err, ErrFailedPrecondition)
	newID := uuid.New()
	require.NoError(t, client.ReserveAgentInstanceDispatch(ctx, instance.Id, newID, ""))
	revoked, err := client.RevokeAgentInstanceDispatch(ctx, instance.Id, id, "next-message")
	require.NoError(t, err)
	require.False(t, revoked)
	_, err = client.CreateRuntimeTask(ctx, instance.Id, taskMutationHash("stale"), next, id.String())
	require.ErrorIs(t, err, ErrFailedPrecondition)
	_, err = client.CreateRuntimeTask(ctx, instance.Id, taskMutationHash("current"), next, newID.String())
	require.NoError(t, err)
	stored, err := client.GetAgentInstanceTask(ctx, instance.Id, string(waiting.ID), nil)
	require.NoError(t, err)
	require.Equal(t, a2a.TaskStateInputRequired, stored.Status.State)
}

func TestRevokedContinuationWithSavedInputIsNotRetryable(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	instance, waiting := waitingTaskFixture(t, client)
	id := uuid.New()
	require.NoError(t, client.ReserveAgentInstanceDispatch(ctx, instance.Id, id, ""))
	_, version, err := client.GetVersionedAgentInstanceTask(ctx, instance.Id, string(waiting.ID))
	require.NoError(t, err)
	input := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("continue"))
	input.TaskID, input.ContextID = waiting.ID, waiting.ContextID
	waiting.History = append(waiting.History, input)
	_, err = client.UpdateAgentInstanceTask(ctx, instance.Id, version, taskMutationHash("input"), waiting, input, id.String())
	require.NoError(t, err)
	notAccepted, err := client.RevokeAgentInstanceDispatch(ctx, instance.Id, id, input.ID)
	require.NoError(t, err)
	require.False(t, notAccepted, "revocation must not make persisted input retryable")
}

func TestCheckpointPinsExpectedTaskWhileSnapshotIsPending(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	instance, waiting := waitingTaskFixture(t, client)
	reply := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("continue"))
	reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
	task, version := resumeRuntimeTask(t, client, instance.Id, reply)
	task.Status.State = a2a.TaskStateCompleted
	version, err := client.UpdateAgentInstanceTask(ctx, instance.Id, version, taskMutationHash("finish"), task, task, "")
	require.NoError(t, err)
	require.NoError(t, client.SettleAgentInstanceTask(ctx, instance.Id, string(task.ID), version))
	request := &apiv1alpha1.Checkpoint{Id: uuid.NewString(), AgentInstanceId: instance.Id, HeadTaskId: string(task.ID)}
	_, _, err = client.ReserveAgentInstanceCheckpoint(ctx, request, "alice", "checkpoint")
	require.ErrorIs(t, err, ErrSnapshotPending)
	work, err := client.ClaimInstanceQuiescence(ctx)
	require.NoError(t, err)
	require.NoError(t, client.FinishInstanceQuiescence(ctx, work, &AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "snapshot", ContentScope: "DATA"}))
	saved, _, err := client.ReserveAgentInstanceCheckpoint(ctx, request, "alice", "saved")
	require.NoError(t, err)
	_, err = client.FinalizeAgentInstanceCheckpoint(ctx, saved.Id, "tag", "retained-snapshot", "")
	require.NoError(t, err)
	next := newAgentInstanceTask(uuid.NewString(), "next-message")
	next.ContextID = instance.ContextId
	_, err = client.CreateRuntimeTask(ctx, instance.Id, taskMutationHash("next"), next, "")
	require.NoError(t, err)
	_, _, err = client.ReserveAgentInstanceCheckpoint(ctx, request, "alice", "checkpoint")
	require.ErrorIs(t, err, ErrCheckpointAdvanced)
	replayed, _, err := client.ReserveAgentInstanceCheckpoint(ctx, request, "alice", "saved")
	require.NoError(t, err)
	require.Equal(t, saved.Id, replayed.Id)
	request.HeadTaskId = string(next.ID)
	_, _, err = client.ReserveAgentInstanceCheckpoint(ctx, request, "alice", "saved")
	require.ErrorIs(t, err, ErrIdempotencyConflict)
}

func TestInitialMessageReservationDeduplicatesAfterAcceptance(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	instance, err = markAgentInstanceReady(ctx, client, instance.Id, "runtime")
	require.NoError(t, err)
	dispatch := uuid.New()
	require.NoError(t, client.ReserveAgentInstanceDispatch(ctx, instance.Id, dispatch, "first-message"))
	require.ErrorIs(t, client.ReserveAgentInstanceDispatch(ctx, instance.Id, uuid.New(), "first-message"), ErrDispatchBusy)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("hello"))
	message.ID, message.ContextID = "first-message", instance.Id
	task := a2a.NewSubmittedTask(message, message)
	_, err = client.CreateRuntimeTask(ctx, instance.Id, taskMutationHash("first"), task, dispatch.String())
	require.NoError(t, err)
	require.ErrorIs(t, client.ReserveAgentInstanceDispatch(ctx, instance.Id, uuid.New(), "first-message"), ErrMessageAccepted)
	require.ErrorIs(t, client.ReserveAgentInstanceDispatch(ctx, instance.Id, uuid.New(), "different-message"), ErrConflict)
	recovered, err := client.GetAgentInstanceTaskByMessage(ctx, instance.Id, "", "first-message")
	require.NoError(t, err)
	require.Equal(t, task.ID, recovered.ID)
	// Two concurrent retries both observe acceptance, never a new dispatch.
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- client.ReserveAgentInstanceDispatch(ctx, instance.Id, uuid.New(), "first-message") }()
	}
	for range 2 {
		require.ErrorIs(t, <-results, ErrMessageAccepted)
	}
}

func TestCheckpointRejectsOlderPendingTask(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	instance, _ := waitingTaskFixture(t, client)
	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("another task"))
	message.ContextID = instance.Id
	task := a2a.NewSubmittedTask(message, message)
	require.NoError(t, saveRuntimeTask(t, client, instance.Id, task, task, nil))
	task.Status.State = a2a.TaskStateCompleted
	require.NoError(t, saveRuntimeTask(t, client, instance.Id, task, task, &AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "snapshot", ContentScope: "DATA"}))
	_, _, err := client.ReserveAgentInstanceCheckpoint(ctx, &apiv1alpha1.Checkpoint{
		Id: uuid.NewString(), AgentInstanceId: instance.Id, HeadTaskId: string(task.ID),
	}, "alice", uuid.NewString())
	require.ErrorIs(t, err, ErrFailedPrecondition)
	require.ErrorContains(t, err, "all tasks to be terminal")
}
