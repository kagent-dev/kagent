package database

import (
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/scheduledrun"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

func createTestSchedule(t *testing.T, c *Client) (*apiv1alpha1.ScheduledRun, []byte) {
	t.Helper()
	agentInstanceFixture(t, c, t.Context(), "team-a", "scheduled-revision", "report", "runtime")
	hash := sha256.Sum256([]byte("original request"))
	request := &apiv1alpha1.ScheduledRun{
		Creator:       "alice",
		Harness:       &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "runtime"},
		AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "report"},
		Config:        scheduledrun.Normalize(&apiv1alpha1.ScheduledRunConfig{Schedule: "* * * * *", Prompt: "original prompt"}),
	}
	result, err := c.CreateScheduledRun(t.Context(), request, "create", hash[:])
	require.NoError(t, err)
	return result, hash[:]
}

func TestScheduledExecutionLeasesFenceExpiredWorkers(t *testing.T) {
	db := setupTestDB(t)
	c := NewClient(db)
	schedule, _ := createTestSchedule(t, c)
	execution, err := c.TriggerScheduledRun(t.Context(), schedule.Id, schedule.Creator, "lease")
	require.NoError(t, err)
	leases := make(chan LeasedScheduledRunExecution, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			batch, err := c.LeaseScheduledRunExecutions(t.Context(), 1)
			if err != nil {
				t.Error(err)
				return
			}
			for _, lease := range batch {
				leases <- lease
			}
		})
	}
	wg.Wait()
	close(leases)
	require.Len(t, leases, 1, "only one controller can lease a firing")
	oldLease := <-leases
	require.Equal(t, execution.Id, oldLease.Execution.Id)
	// Move only the lease clock to simulate a controller dying. The firing was
	// created and all lifecycle transitions are exercised through store APIs.
	_, err = db.Exec(t.Context(), `UPDATE scheduled_run_execution SET next_attempt_at = clock_timestamp() - interval '1 second' WHERE id = $1`, execution.Id)
	require.NoError(t, err)
	oldLease.Execution.State = apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_FAILED
	require.ErrorIs(t, c.UpdateScheduledRunExecution(t.Context(), oldLease.Lease, ScheduledRunExecutionProgress{State: oldLease.Execution.State}), ErrScheduledRunConflict)
	batch, err := c.LeaseScheduledRunExecutions(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, batch, 1)
	require.NotEqual(t, oldLease.Lease.Token, batch[0].Lease.Token)
	require.ErrorIs(t, c.UpdateScheduledRunExecution(t.Context(), oldLease.Lease, ScheduledRunExecutionProgress{State: oldLease.Execution.State}), ErrScheduledRunConflict)
	batch[0].Execution.State = apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_FAILED
	batch[0].Execution.FailureReason = "Controller denied execution"
	// A worker snapshot is not a write model: only explicit progress is saved.
	batch[0].Execution.Id = "replaced"
	batch[0].Execution.Prompt = "changed"
	batch[0].Execution.Deadline = nil
	batch[0].Execution.Creator = "mallory"
	batch[0].Execution.AgentInstanceId = "replaced"
	require.NoError(t, c.UpdateScheduledRunExecution(t.Context(), batch[0].Lease, ScheduledRunExecutionProgress{State: batch[0].Execution.State, FailureReason: batch[0].Execution.FailureReason}))
	finished, err := c.GetScheduledRunExecution(t.Context(), execution.Id, schedule.Creator)
	require.NoError(t, err)
	require.NotNil(t, finished.CompletedAt)
	require.Equal(t, execution.Id, finished.Id)
	require.Equal(t, execution.Prompt, finished.Prompt)
	require.True(t, proto.Equal(execution.Deadline, finished.Deadline))
	require.Equal(t, execution.Creator, finished.Creator)
	require.Empty(t, finished.AgentInstanceId)
	require.Equal(t, batch[0].Execution.FailureReason, finished.FailureReason)
	batch, err = c.LeaseScheduledRunExecutions(t.Context(), 1)
	require.NoError(t, err)
	require.Empty(t, batch, "completed work is never leased again")
}

func TestScheduledRunRequestsSurviveEditAndDeletion(t *testing.T) {
	c := NewClient(setupTestDB(t))
	schedule, hash := createTestSchedule(t, c)
	execution, err := c.TriggerScheduledRun(t.Context(), schedule.Id, "alice", "manual")
	require.NoError(t, err)
	require.Equal(t, 15*time.Minute, execution.Deadline.AsTime().Sub(execution.CreatedAt.AsTime()))
	config := proto.CloneOf(schedule.Config)
	config.Prompt, config.Paused = "new prompt", true
	updated, err := c.UpdateScheduledRun(t.Context(), schedule.Id, "alice", schedule.Etag, config)
	require.NoError(t, err)
	require.Nil(t, updated.NextExecutionTime)
	require.NotEqual(t, schedule.Etag, updated.Etag)
	_, err = c.UpdateScheduledRun(t.Context(), schedule.Id, "alice", schedule.Etag, config)
	require.ErrorIs(t, err, ErrScheduledRunConflict)

	// Pausing stops cron, but does not forbid an explicit manual run.
	manual, err := c.TriggerScheduledRun(t.Context(), schedule.Id, "alice", "while-paused")
	require.NoError(t, err)
	require.Equal(t, "new prompt", manual.Prompt)
	deleted, err := c.DeleteScheduledRun(t.Context(), schedule.Id, "alice")
	require.NoError(t, err)
	require.NotNil(t, deleted.DeletedAt)
	again, err := c.DeleteScheduledRun(t.Context(), schedule.Id, "alice")
	require.NoError(t, err)
	require.True(t, proto.Equal(deleted, again))

	replayed, err := c.TriggerScheduledRun(t.Context(), schedule.Id, "alice", "manual")
	require.NoError(t, err)
	require.True(t, proto.Equal(execution, replayed))
	require.Equal(t, "original prompt", replayed.Prompt)
	_, err = c.TriggerScheduledRun(t.Context(), schedule.Id, "alice", "new-request")
	require.ErrorIs(t, err, ErrScheduledRunDeleted)
	_, err = c.UpdateScheduledRun(t.Context(), schedule.Id, "alice", deleted.Etag, config)
	require.ErrorIs(t, err, ErrScheduledRunDeleted)

	found, err := c.FindScheduledRunRequest(t.Context(), "alice", "create", hash)
	require.NoError(t, err)
	require.True(t, proto.Equal(deleted, found))
	changedHash := sha256.Sum256([]byte("other input"))
	_, err = c.FindScheduledRunRequest(t.Context(), "alice", "create", changedHash[:])
	require.ErrorIs(t, err, ErrIdempotencyConflict)
	recreated, err := c.CreateScheduledRun(t.Context(), schedule, "create", hash)
	require.NoError(t, err)
	require.True(t, proto.Equal(deleted, recreated))

	history, err := c.ListScheduledRunExecutions(t.Context(), ScheduledRunExecutionQuery{ScheduledRunQuery: ScheduledRunQuery{Creator: "alice", Limit: 1}, ScheduledRunID: schedule.Id})
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, manual.Id, history[0].Id)
	page, err := c.ListScheduledRunExecutions(t.Context(), ScheduledRunExecutionQuery{ScheduledRunQuery: ScheduledRunQuery{Creator: "alice", AfterID: manual.Id, Limit: 1}, ScheduledRunID: schedule.Id})
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, execution.Id, page[0].Id)
	for _, creator := range []string{"alice", "bob"} {
		rows, err := c.ListScheduledRuns(t.Context(), ScheduledRunQuery{Creator: creator, Limit: 50})
		require.NoError(t, err)
		require.Empty(t, rows)
	}
	_, err = c.GetScheduledRunExecution(t.Context(), execution.Id, "bob")
	require.ErrorIs(t, err, ErrNotFound)
	_, err = c.TriggerScheduledRun(t.Context(), schedule.Id, "bob", "manual")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestScheduledRunConcurrentReservation(t *testing.T) {
	db := setupTestDB(t)
	c := NewClient(db)
	schedule, _ := createTestSchedule(t, c)
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	for range 12 {
		wg.Go(func() {
			run, err := c.TriggerScheduledRun(t.Context(), schedule.Id, "alice", "same-request")
			if err != nil {
				t.Error(err)
				return
			}
			ids <- run.Id
		})
	}
	wg.Wait()
	close(ids)
	unique := map[string]bool{}
	for id := range ids {
		unique[id] = true
	}
	require.Len(t, unique, 1)

	_, err := db.Exec(t.Context(), "UPDATE scheduled_run SET next_execution_time = clock_timestamp() - interval '1 second' WHERE id = $1", schedule.Id)
	require.NoError(t, err)
	due := make(chan *apiv1alpha1.ScheduledRunExecution, 12)
	for range 12 {
		wg.Go(func() {
			runs, err := c.ReserveDueScheduledRuns(t.Context(), 100)
			if err != nil {
				t.Error(err)
				return
			}
			for _, run := range runs {
				due <- run
			}
		})
	}
	wg.Wait()
	close(due)
	var runs []*apiv1alpha1.ScheduledRunExecution
	for run := range due {
		runs = append(runs, run)
	}
	require.Len(t, runs, 1)
	require.NotNil(t, runs[0].GetScheduledTime())
	require.False(t, unique[runs[0].Id])

	_, err = db.Exec(t.Context(), "UPDATE scheduled_run SET next_execution_time = clock_timestamp() - interval '2 minutes' WHERE id = $1", schedule.Id)
	require.NoError(t, err)
	skipped, err := c.ReserveDueScheduledRuns(t.Context(), 100)
	require.NoError(t, err)
	require.Empty(t, skipped)
	advanced, err := c.GetScheduledRun(t.Context(), schedule.Id, "alice")
	require.NoError(t, err)
	require.True(t, advanced.NextExecutionTime.AsTime().After(time.Now()))
}

func TestScheduledRunConcurrentUpdateAndDelete(t *testing.T) {
	c := NewClient(setupTestDB(t))
	schedule, _ := createTestSchedule(t, c)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			_, err := c.UpdateScheduledRun(t.Context(), schedule.Id, "alice", schedule.Etag, schedule.Config)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	var successes, conflicts int
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrScheduledRunConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	var accepted *apiv1alpha1.ScheduledRunExecution
	wg.Go(func() {
		var err error
		accepted, err = c.TriggerScheduledRun(t.Context(), schedule.Id, "alice", "racing-delete")
		if err != nil && !errors.Is(err, ErrScheduledRunDeleted) {
			t.Error(err)
		}
	})
	wg.Go(func() {
		_, err := c.DeleteScheduledRun(t.Context(), schedule.Id, "alice")
		if err != nil {
			t.Error(err)
		}
	})
	wg.Wait()
	if accepted != nil {
		replayed, err := c.TriggerScheduledRun(t.Context(), schedule.Id, "alice", "racing-delete")
		require.NoError(t, err)
		require.Equal(t, accepted.Id, replayed.Id)
	}
}

func TestScheduledRunExecutionConstraints(t *testing.T) {
	db := setupTestDB(t)
	c := NewClient(db)
	schedule, _ := createTestSchedule(t, c)
	execution, err := c.TriggerScheduledRun(t.Context(), schedule.Id, "alice", "manual")
	require.NoError(t, err)
	for _, update := range []string{
		"manual_request_id = NULL",
		"scheduled_time = clock_timestamp()", "state = 'UNKNOWN'", "state = 'RUNNING'",
	} {
		_, err := db.Exec(t.Context(), "UPDATE scheduled_run_execution SET "+update+" WHERE id = $1", execution.Id)
		require.Error(t, err, update)
	}
	loaded, err := c.GetScheduledRunExecution(t.Context(), execution.Id, "alice")
	require.NoError(t, err)
	require.True(t, proto.Equal(execution, loaded))
}

func TestScheduledExecutionSurvivesInstanceDeletion(t *testing.T) {
	db := setupTestDB(t)
	c := NewClient(db)
	schedule, _ := createTestSchedule(t, c)
	execution, err := c.TriggerScheduledRun(t.Context(), schedule.Id, "alice", "manual")
	require.NoError(t, err)
	require.Empty(t, execution.AgentInstanceId)
	require.Equal(t, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_PENDING, execution.State)
	// Concurrent worker retries must atomically reserve one instance and its link.
	ids := make(chan string, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			linked, err := c.ReserveScheduledRunExecutionInstance(t.Context(), execution.Id, "alice")
			if err != nil {
				t.Error(err)
				return
			}
			ids <- linked.AgentInstanceId
		})
	}
	wg.Wait()
	close(ids)
	unique := map[string]bool{}
	for id := range ids {
		unique[id] = true
	}
	require.Len(t, unique, 1)
	linked, err := c.GetScheduledRunExecution(t.Context(), execution.Id, "alice")
	require.NoError(t, err)
	require.NotEmpty(t, linked.AgentInstanceId)
	require.NotEqual(t, execution.Id, linked.AgentInstanceId)
	instance, err := c.GetAgentInstance(t.Context(), linked.AgentInstanceId, "alice")
	require.NoError(t, err)
	require.Equal(t, "scheduled-revision", instance.PreparedRevision)
	require.NoError(t, c.DeleteAgentInstance(t.Context(), instance.Id))
	// Instance deletion follows the ordinary hard-delete path.
	_, err = c.GetAgentInstance(t.Context(), instance.Id, "alice")
	require.ErrorIs(t, err, ErrNotFound)
	replayed, err := c.TriggerScheduledRun(t.Context(), schedule.Id, "alice", "manual")
	require.NoError(t, err)
	require.True(t, proto.Equal(linked, replayed))
	retried, err := c.ReserveScheduledRunExecutionInstance(t.Context(), execution.Id, "alice")
	require.NoError(t, err)
	require.True(t, proto.Equal(linked, retried))
	instances, err := c.ListAgentInstances(t.Context(), AgentInstanceQuery{UserID: "alice", Limit: 10})
	require.NoError(t, err)
	require.Empty(t, instances)
	history, err := c.ListScheduledRunExecutions(t.Context(), ScheduledRunExecutionQuery{ScheduledRunQuery: ScheduledRunQuery{Creator: "alice", Limit: 10}, ScheduledRunID: schedule.Id})
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.True(t, proto.Equal(linked, history[0]))
	_, err = c.ReserveScheduledRunExecutionInstance(t.Context(), execution.Id, "bob")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestScheduledExecutionWaitsForPreparedRevision(t *testing.T) {
	db := setupTestDB(t)
	c := NewClient(db)
	schedule, _ := createTestSchedule(t, c)
	require.NoError(t, c.RetireAgentTemplateHarnessPair(t.Context(), "team-a", "report", "runtime"))
	execution, err := c.TriggerScheduledRun(t.Context(), schedule.Id, "alice", "manual")
	require.NoError(t, err)
	_, err = c.ReserveScheduledRunExecutionInstance(t.Context(), execution.Id, "alice")
	require.ErrorIs(t, err, ErrScheduledRunTargetNotReady)
	loaded, err := c.GetScheduledRunExecution(t.Context(), execution.Id, "alice")
	require.NoError(t, err)
	require.True(t, proto.Equal(execution, loaded))
	instances, err := c.ListAgentInstances(t.Context(), AgentInstanceQuery{UserID: "alice", Limit: 10})
	require.NoError(t, err)
	require.Empty(t, instances)
	// Unready targets no longer roll back due reservations or block other schedules.
	_, err = db.Exec(t.Context(), "UPDATE scheduled_run SET next_execution_time = clock_timestamp() - interval '1 second' WHERE id = $1", schedule.Id)
	require.NoError(t, err)
	due, err := c.ReserveDueScheduledRuns(t.Context(), 100)
	require.NoError(t, err)
	require.Len(t, due, 1)
	require.Empty(t, due[0].AgentInstanceId)
	agentInstanceFixture(t, c, t.Context(), "team-a", "scheduled-revision-2", "report", "runtime")
	linked, err := c.ReserveScheduledRunExecutionInstance(t.Context(), execution.Id, "alice")
	require.NoError(t, err)
	instance, err := c.GetAgentInstance(t.Context(), linked.AgentInstanceId, "alice")
	require.NoError(t, err)
	require.Equal(t, "scheduled-revision-2", instance.PreparedRevision)
}

func TestScheduledExecutionExpiresBeforeInstanceCreation(t *testing.T) {
	c := NewClient(setupTestDB(t))
	schedule, _ := createTestSchedule(t, c)
	config := proto.CloneOf(schedule.Config)
	config.ExecutionTimeout = durationpb.New(time.Microsecond)
	_, err := c.UpdateScheduledRun(t.Context(), schedule.Id, "alice", schedule.Etag, config)
	require.NoError(t, err)
	execution, err := c.TriggerScheduledRun(t.Context(), schedule.Id, "alice", "expires")
	require.NoError(t, err)
	expired, err := c.ReserveScheduledRunExecutionInstance(t.Context(), execution.Id, "alice")
	require.NoError(t, err)
	require.Empty(t, expired.AgentInstanceId)
	require.Equal(t, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT, expired.State)
	replayed, err := c.TriggerScheduledRun(t.Context(), schedule.Id, "alice", "expires")
	require.NoError(t, err)
	require.True(t, proto.Equal(expired, replayed))
}

func TestScheduledRunRejectsCorruptPayloads(t *testing.T) {
	db := setupTestDB(t)
	c := NewClient(db)
	schedule, _ := createTestSchedule(t, c)
	execution, err := c.TriggerScheduledRun(t.Context(), schedule.Id, "alice", "manual")
	require.NoError(t, err)
	for _, data := range [][]byte{{0xff}, {}} {
		_, err := db.Exec(t.Context(), "UPDATE scheduled_run_execution SET data = $1 WHERE id = $2", data, execution.Id)
		require.NoError(t, err)
		_, err = c.GetScheduledRunExecution(t.Context(), execution.Id, "alice")
		require.Error(t, err)
		_, err = db.Exec(t.Context(), "UPDATE scheduled_run SET data = $1 WHERE id = $2", data, schedule.Id)
		require.NoError(t, err)
		_, err = c.GetScheduledRun(t.Context(), schedule.Id, "alice")
		require.Error(t, err)
	}
	// Valid protobuf bytes can still be missing required durable inputs.
	schedule.Config.Prompt = " "
	data, err := proto.Marshal(schedule)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), "UPDATE scheduled_run SET data = $1 WHERE id = $2", data, schedule.Id)
	require.NoError(t, err)
	_, err = c.GetScheduledRun(t.Context(), schedule.Id, "alice")
	require.Error(t, err)
}

func TestScheduledExecutionTaskIdentityCannotChange(t *testing.T) {
	db := setupTestDB(t)
	c := NewClient(db)
	schedule, _ := createTestSchedule(t, c)
	execution, err := c.TriggerScheduledRun(t.Context(), schedule.Id, "alice", "manual")
	require.NoError(t, err)
	linked, err := c.ReserveScheduledRunExecutionInstance(t.Context(), execution.Id, "alice")
	require.NoError(t, err)
	leases, err := c.LeaseScheduledRunExecutions(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, leases, 1)
	progress := ScheduledRunExecutionProgress{State: apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING, TaskID: execution.Id}
	require.NoError(t, c.UpdateScheduledRunExecution(t.Context(), leases[0].Lease, progress))
	require.ErrorIs(t, c.UpdateScheduledRunExecution(t.Context(), leases[0].Lease, progress), ErrScheduledRunConflict)
	_, err = db.Exec(t.Context(), "UPDATE scheduled_run_execution SET next_attempt_at = clock_timestamp() WHERE id = $1", execution.Id)
	require.NoError(t, err)
	leases, err = c.LeaseScheduledRunExecutions(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, leases, 1)
	progress.TaskID = "another-task"
	require.ErrorIs(t, c.UpdateScheduledRunExecution(t.Context(), leases[0].Lease, progress), ErrScheduledRunConflict)
	// Omitting the already persisted task preserves its identity.
	progress.TaskID = ""
	progress.State = apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED
	require.NoError(t, c.UpdateScheduledRunExecution(t.Context(), leases[0].Lease, progress))
	loaded, err := c.GetScheduledRunExecution(t.Context(), execution.Id, "alice")
	require.NoError(t, err)
	require.Equal(t, execution.Id, loaded.TaskId)
	require.Equal(t, linked.AgentInstanceId, loaded.AgentInstanceId)
	require.Equal(t, progress.State, loaded.State)
}
