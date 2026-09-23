package database

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func checkpointNextTurn(t *testing.T, client *Client, instance *apiv1alpha1.AgentInstance) *apiv1alpha1.Checkpoint {
	t.Helper()
	ctx := t.Context()
	task := newAgentInstanceTask(uuid.NewString(), uuid.NewString())
	task.ContextID = instance.ContextId
	_, _, err := client.CreateAgentInstanceTask(ctx, instance.Id, []byte(task.ID), task)
	require.NoError(t, err)
	task.Status.State = a2a.TaskStateCompleted
	require.NoError(t, client.StoreAgentInstanceTaskEvent(ctx, instance.Id, task, task,
		&AgentInstanceTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/" + string(task.ID), ContentScope: "DATA"}))
	checkpoint, _, err := client.ReserveAgentInstanceCheckpoint(ctx,
		&apiv1alpha1.Checkpoint{Id: uuid.NewString(), AgentInstanceId: instance.Id}, "alice", uuid.NewString())
	require.NoError(t, err)
	checkpoint, err = client.FinalizeAgentInstanceCheckpoint(ctx, checkpoint.Id, "tag-"+checkpoint.Id, "s3://tags/"+checkpoint.Id, "")
	require.NoError(t, err)
	return checkpoint
}

func TestAgentHistoryConstraints(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	source, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", "source"), uuid.NewString())
	require.NoError(t, err)
	source, err = markAgentInstanceReady(ctx, client, source.Id, "source.example")
	require.NoError(t, err)
	checkpoint := checkpointNextTurn(t, client, source)
	boundary, err := readCheckpoint(ctx, client.db, checkpoint.Id, "alice", nil)
	require.NoError(t, err)
	for _, tt := range []struct {
		name      string
		owner     string
		contextID string
	}{
		{"different owner", "mallory", source.ContextId},
		{"different context", "alice", uuid.NewString()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Deliberately bypass the store to prove PostgreSQL rejects invalid ancestry.
			_, err := client.db.Exec(ctx, `
				INSERT INTO agent_history (id, instance_id, user_id, context_id, parent_history_id, parent_history_sequence)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, uuid.New(), uuid.New(), tt.owner, tt.contextID, boundary.SourceHistoryID, boundary.HistorySequence)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, "23503", pgErr.Code)
			require.Equal(t, "agent_history_parent_binding_fkey", pgErr.ConstraintName)
		})
	}
	for _, column := range []string{"id", "user_id", "context_id"} {
		t.Run("instance binding/"+column, func(t *testing.T) {
			_, err := client.db.Exec(ctx, "UPDATE agent_instance SET "+column+" = $1 WHERE id = $2", uuid.NewString(), source.Id)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, "23503", pgErr.Code)
			require.Equal(t, "agent_instance_context_binding_fkey", pgErr.ConstraintName)
		})
	}
	require.NoError(t, client.DeleteAgentInstance(ctx, source.Id))
	_, _, err = client.CreateAgentInstance(ctx, newAgentInstanceRequest(source.Id, "assistant", "kagent", "replacement"), uuid.NewString())
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "23505", pgErr.Code, "a deleted instance ID must not be rebound to another history")
}

func TestForkAgentInstanceRejectsInvalidBoundary(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	newCheckpoint := func() *apiv1alpha1.Checkpoint {
		t.Helper()
		instance, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", "source"), uuid.NewString())
		require.NoError(t, err)
		instance, err = markAgentInstanceReady(ctx, client, instance.Id, "source.example")
		require.NoError(t, err)
		return checkpointNextTurn(t, client, instance)
	}
	source, other := newCheckpoint(), newCheckpoint()
	for _, tt := range []struct {
		name     string
		sequence uint64
		headTask string
		wantErr  string
	}{
		{"missing event", math.MaxInt64, source.HeadTaskId, "checkpoint history boundary is missing"},
		{"another history event", other.HistorySequence, other.HeadTaskId, "checkpoint history boundary is missing"},
		{"wrong head task", source.HistorySequence, uuid.NewString(), "checkpoint runtime boundary is inconsistent"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Corrupt both the payload and indexes so the test reaches event validation,
			// rather than failing the independent protobuf/index consistency check.
			invalid := proto.Clone(source).(*apiv1alpha1.Checkpoint)
			invalid.HistorySequence, invalid.HeadTaskId = tt.sequence, tt.headTask
			data, err := proto.Marshal(invalid)
			require.NoError(t, err)
			_, err = client.db.Exec(ctx, `
				UPDATE agent_instance_checkpoint SET history_sequence = $2, head_task_id = $3, data = $4 WHERE id = $1
			`, source.Id, int64(invalid.HistorySequence), invalid.HeadTaskId, data)
			require.NoError(t, err)
			forkID, requestID := uuid.NewString(), uuid.NewString()
			fork, created, err := client.ForkAgentInstance(ctx, source.Id, "alice", requestID, forkID)
			require.ErrorContains(t, err, tt.wantErr)
			require.Nil(t, fork)
			require.False(t, created)
			_, err = client.GetAgentInstance(ctx, forkID, "alice")
			require.ErrorIs(t, err, ErrNotFound)
			var historyExists bool
			require.NoError(t, client.db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM agent_history WHERE instance_id = $1)", forkID).Scan(&historyExists))
			require.False(t, historyExists, "failed validation must roll back the durable lineage")

			// Repair the boundary and retry the same request/instance IDs: no failed
			// reservation or history may prevent the valid fork from being created.
			data, err = proto.Marshal(source)
			require.NoError(t, err)
			_, err = client.db.Exec(ctx, `
				UPDATE agent_instance_checkpoint SET history_sequence = $2, head_task_id = $3, data = $4 WHERE id = $1
			`, source.Id, int64(source.HistorySequence), source.HeadTaskId, data)
			require.NoError(t, err)
			fork, created, err = client.ForkAgentInstance(ctx, source.Id, "alice", requestID, forkID)
			require.NoError(t, err)
			require.True(t, created)
			require.Equal(t, forkID, fork.Id)
			listed, err := client.ListAgentInstanceCheckpoints(ctx, forkID, "alice", "", 100)
			require.NoError(t, err)
			require.Equal(t, []string{source.Id}, listedCheckpointIDs(listed))
		})
	}
}

func TestCheckpointDeletionSerializesWithFork(t *testing.T) {
	for _, sameCheckpoint := range []bool{true, false} {
		for _, forkFirst := range []bool{true, false} {
			t.Run(fmt.Sprintf("same_checkpoint=%t/fork_first=%t", sameCheckpoint, forkFirst), func(t *testing.T) {
				pool := setupTestDB(t)
				client := NewClient(pool)
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
				source, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", "source"), uuid.NewString())
				require.NoError(t, err)
				source, err = markAgentInstanceReady(ctx, client, source.Id, "source.example")
				require.NoError(t, err)
				deletedCheckpoint := checkpointNextTurn(t, client, source)
				forkCheckpoint := deletedCheckpoint
				if !sameCheckpoint {
					forkCheckpoint = checkpointNextTurn(t, client, source)
				}
				// Pause the first operation while it holds the history lock. With different
				// checkpoints, no checkpoint-row lock can serialize these operations for us.
				barrier := &queryBarrier{
					query: "FROM agent_history WHERE id = $1 FOR NO KEY UPDATE", afterQuery: true,
					reached: make(chan struct{}), resume: make(chan struct{}),
				}
				var resume sync.Once
				defer resume.Do(func() { close(barrier.resume) })
				config := pool.Config()
				config.ConnConfig.Tracer = barrier
				blockedPool, err := pgxpool.NewWithConfig(ctx, config)
				require.NoError(t, err)
				t.Cleanup(blockedPool.Close)
				forking, deleting := client, NewClient(blockedPool)
				if forkFirst {
					forking, deleting = deleting, forking
				}
				forkID := uuid.NewString()
				forked, deleted := make(chan error, 1), make(chan error, 1)
				fork := func() {
					_, _, err := forking.ForkAgentInstance(ctx, forkCheckpoint.Id, "alice", uuid.NewString(), forkID)
					forked <- err
				}
				deleteCheckpoint := func() {
					_, _, err := deleting.BeginDeleteAgentInstanceCheckpoint(ctx, deletedCheckpoint.Id, "alice")
					deleted <- err
				}
				if forkFirst {
					go fork()
				} else {
					go deleteCheckpoint()
				}
				select {
				case <-barrier.reached:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if forkFirst {
					go deleteCheckpoint()
				} else {
					go fork()
				}
				waitingQuery := "%FROM agent_history WHERE id = $1 FOR NO KEY UPDATE%"
				if sameCheckpoint {
					waitingQuery = "%FROM agent_instance_checkpoint%FOR UPDATE%"
				}
				require.Eventually(t, func() bool {
					var waiting bool
					err := pool.QueryRow(ctx, `SELECT EXISTS (
						SELECT 1 FROM pg_stat_activity WHERE datname = current_database()
						AND query LIKE $1 AND cardinality(pg_blocking_pids(pid)) > 0
					)`, waitingQuery).Scan(&waiting)
					return err == nil && waiting
				}, 5*time.Second, 10*time.Millisecond)
				resume.Do(func() { close(barrier.resume) })
				forkErr, deleteErr := <-forked, <-deleted
				if forkFirst {
					require.NoError(t, forkErr)
					require.ErrorIs(t, deleteErr, ErrNotFound)
					checkpoint, err := client.GetAgentInstanceCheckpoint(ctx, deletedCheckpoint.Id, "alice")
					require.NoError(t, err)
					require.Equal(t, apiv1alpha1.CheckpointState_CHECKPOINT_STATE_READY, checkpoint.State)
					listed, err := client.ListAgentInstanceCheckpoints(ctx, forkID, "alice", "", 100)
					require.NoError(t, err)
					require.Contains(t, listedCheckpointIDs(listed), deletedCheckpoint.Id)
				} else {
					require.NoError(t, deleteErr)
					if sameCheckpoint {
						require.ErrorIs(t, forkErr, ErrNotFound)
						_, err := client.GetAgentInstance(ctx, forkID, "alice")
						require.ErrorIs(t, err, ErrNotFound)
					} else {
						require.NoError(t, forkErr)
						listed, err := client.ListAgentInstanceCheckpoints(ctx, forkID, "alice", "", 100)
						require.NoError(t, err)
						require.Equal(t, []string{forkCheckpoint.Id}, listedCheckpointIDs(listed))
					}
					// Starting deletion first permits cleanup retries even after a later fork.
					_, _, err := client.BeginDeleteAgentInstanceCheckpoint(ctx, deletedCheckpoint.Id, "alice")
					require.NoError(t, err)
					require.NoError(t, client.DeleteAgentInstanceCheckpoint(ctx, deletedCheckpoint.Id, "alice"))
					_, err = client.GetAgentInstanceCheckpoint(ctx, deletedCheckpoint.Id, "alice")
					require.ErrorIs(t, err, ErrNotFound)
				}
			})
		}
	}
}

func listedCheckpointIDs(checkpoints []*apiv1alpha1.Checkpoint) []string {
	ids := make([]string, len(checkpoints))
	for i, checkpoint := range checkpoints {
		ids[i] = checkpoint.Id
	}
	return ids
}

func TestCheckpointLineage(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	source, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", "source"), uuid.NewString())
	require.NoError(t, err)
	source, err = markAgentInstanceReady(ctx, client, source.Id, "source.example")
	require.NoError(t, err)

	fork := func(checkpoint *apiv1alpha1.Checkpoint) *apiv1alpha1.AgentInstance {
		t.Helper()
		instance, created, err := client.ForkAgentInstance(ctx, checkpoint.Id, "alice", uuid.NewString(), uuid.NewString())
		require.NoError(t, err)
		require.True(t, created)
		instance, err = markAgentInstanceReady(ctx, client, instance.Id, "fork.example")
		require.NoError(t, err)
		return instance
	}
	assertListed := func(instanceID string, want ...*apiv1alpha1.Checkpoint) {
		t.Helper()
		got, err := client.ListAgentInstanceCheckpoints(ctx, instanceID, "alice", "", 100)
		require.NoError(t, err)
		require.Len(t, got, len(want))
		for _, checkpoint := range want {
			require.True(t, slices.ContainsFunc(got, func(candidate *apiv1alpha1.Checkpoint) bool {
				return proto.Equal(checkpoint, candidate)
			}), "missing checkpoint %s with its original provenance", checkpoint.Id)
		}
	}

	deleting := checkpointNextTurn(t, client, source)
	c1 := checkpointNextTurn(t, client, source)
	c2 := checkpointNextTurn(t, client, source)
	_, _, err = client.BeginDeleteAgentInstanceCheckpoint(ctx, deleting.Id, "alice")
	require.NoError(t, err)
	branch := fork(c2)
	assertListed(branch.Id, c1, c2)
	// A fork created after deletion begins must not prevent its cleanup from retrying.
	_, _, err = client.BeginDeleteAgentInstanceCheckpoint(ctx, deleting.Id, "alice")
	require.NoError(t, err)
	require.NoError(t, client.DeleteAgentInstanceCheckpoint(ctx, deleting.Id, "alice"))
	c3 := checkpointNextTurn(t, client, source)
	c4 := checkpointNextTurn(t, client, branch)
	nested := fork(c4)
	c5 := checkpointNextTurn(t, client, branch)
	assertListed(source.Id, c1, c2, c3)
	assertListed(branch.Id, c1, c2, c4, c5)
	assertListed(nested.Id, c1, c2, c4)

	assertPages := func(t *testing.T) {
		t.Helper()
		// Paging merges local and inherited checkpoints without repeating local candidates.
		for _, tt := range []struct {
			name       string
			instanceID string
			wantIDs    []string
		}{
			{"root", source.Id, []string{c1.Id, c2.Id, c3.Id}},
			{"mixed", branch.Id, []string{c1.Id, c2.Id, c4.Id, c5.Id}},
			{"inherited", nested.Id, []string{c1.Id, c2.Id, c4.Id}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				slices.Sort(tt.wantIDs)
				for _, pageSize := range []int{1, 2, 10} {
					var afterID string
					var gotIDs []string
					for {
						page, err := client.ListAgentInstanceCheckpoints(ctx, tt.instanceID, "alice", afterID, pageSize)
						require.NoError(t, err)
						require.LessOrEqual(t, len(page), pageSize)
						if len(page) == 0 {
							break
						}
						for _, checkpoint := range page {
							require.Greater(t, checkpoint.Id, afterID)
							gotIDs = append(gotIDs, checkpoint.Id)
							afterID = checkpoint.Id
						}
					}
					require.Equal(t, tt.wantIDs, gotIDs)
				}
			})
		}
	}
	t.Run("before_deletion", assertPages)
	unauthorized, err := client.ListAgentInstanceCheckpoints(ctx, nested.Id, "mallory", "", 100)
	require.NoError(t, err)
	require.Empty(t, unauthorized)
	assertListed(uuid.NewString())

	require.NoError(t, client.DeleteAgentInstance(ctx, source.Id))
	require.NoError(t, client.DeleteAgentInstance(ctx, branch.Id))
	assertListed(nested.Id, c1, c2, c4)
	assertListed(source.Id, c1, c2, c3)
	assertListed(branch.Id, c1, c2, c4, c5)
	t.Run("after_deletion", assertPages)
	unauthorized, err = client.ListAgentInstanceCheckpoints(ctx, branch.Id, "mallory", "", 100)
	require.NoError(t, err)
	require.Empty(t, unauthorized)
	// No fork starts at c1 yet; it is retained because it precedes c2 in the inherited history.
	_, _, err = client.BeginDeleteAgentInstanceCheckpoint(ctx, c1.Id, "alice")
	require.ErrorIs(t, err, ErrNotFound)
	// A checkpoint inherited by a fork remains independently usable as a fork source.
	earlier := fork(c1)
	assertListed(earlier.Id, c1)
	require.NoError(t, client.DeleteAgentInstance(ctx, earlier.Id))
	require.NoError(t, client.DeleteAgentInstance(ctx, nested.Id))
	assertListed(nested.Id, c1, c2, c4)

	// Retained histories protect the entire inherited prefix, even after instance deletion.
	for _, checkpoint := range []*apiv1alpha1.Checkpoint{c1, c2, c4} {
		_, _, err := client.BeginDeleteAgentInstanceCheckpoint(ctx, checkpoint.Id, "alice")
		require.ErrorIs(t, err, ErrNotFound)
		got, err := client.GetAgentInstanceCheckpoint(ctx, checkpoint.Id, "alice")
		require.NoError(t, err)
		require.True(t, proto.Equal(checkpoint, got))
	}
	for _, checkpoint := range []*apiv1alpha1.Checkpoint{c3, c5} {
		_, _, err := client.BeginDeleteAgentInstanceCheckpoint(ctx, checkpoint.Id, "alice")
		require.NoError(t, err)
		require.NoError(t, client.DeleteAgentInstanceCheckpoint(ctx, checkpoint.Id, "alice"))
	}
}
