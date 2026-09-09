package database

import (
	"slices"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestCheckpointLineage(t *testing.T) {
	client := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	source, _, err := client.CreateAgentInstance(ctx, newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", "source"), uuid.NewString())
	require.NoError(t, err)
	source, err = markAgentInstanceReady(ctx, client, source.Id, "source.example")
	require.NoError(t, err)

	checkpointNextTurn := func(instance *apiv1alpha1.AgentInstance) *apiv1alpha1.Checkpoint {
		t.Helper()
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

	deleting := checkpointNextTurn(source)
	c1 := checkpointNextTurn(source)
	c2 := checkpointNextTurn(source)
	_, _, err = client.BeginDeleteAgentInstanceCheckpoint(ctx, deleting.Id, "alice")
	require.NoError(t, err)
	branch := fork(c2)
	// A fork created after deletion begins must not prevent its cleanup from retrying.
	_, _, err = client.BeginDeleteAgentInstanceCheckpoint(ctx, deleting.Id, "alice")
	require.NoError(t, err)
	require.NoError(t, client.DeleteAgentInstanceCheckpoint(ctx, deleting.Id, "alice"))
	c3 := checkpointNextTurn(source)
	c4 := checkpointNextTurn(branch)
	nested := fork(c4)
	c5 := checkpointNextTurn(branch)
	assertListed(source.Id, c1, c2, c3)
	assertListed(branch.Id, c1, c2, c4, c5)
	assertListed(nested.Id, c1, c2, c4)

	// Paging uses checkpoint IDs across every ancestor, without duplicates.
	wantIDs := []string{c1.Id, c2.Id, c4.Id}
	slices.Sort(wantIDs)
	var afterID string
	for _, id := range wantIDs {
		page, err := client.ListAgentInstanceCheckpoints(ctx, nested.Id, "alice", afterID, 1)
		require.NoError(t, err)
		require.Len(t, page, 1)
		require.Equal(t, id, page[0].Id)
		afterID = id
	}
	page, err := client.ListAgentInstanceCheckpoints(ctx, nested.Id, "alice", afterID, 1)
	require.NoError(t, err)
	require.Empty(t, page)
	unauthorized, err := client.ListAgentInstanceCheckpoints(ctx, nested.Id, "mallory", "", 100)
	require.NoError(t, err)
	require.Empty(t, unauthorized)
	assertListed(uuid.NewString())

	require.NoError(t, client.DeleteAgentInstance(ctx, source.Id))
	require.NoError(t, client.DeleteAgentInstance(ctx, branch.Id))
	assertListed(nested.Id, c1, c2, c4)
	// No fork starts at c1 yet; it is retained because it precedes c2 in the inherited history.
	_, _, err = client.BeginDeleteAgentInstanceCheckpoint(ctx, c1.Id, "alice")
	require.ErrorIs(t, err, ErrNotFound)
	// A checkpoint inherited by a fork remains independently usable as a fork source.
	earlier := fork(c1)
	assertListed(earlier.Id, c1)
	require.NoError(t, client.DeleteAgentInstance(ctx, earlier.Id))
	require.NoError(t, client.DeleteAgentInstance(ctx, nested.Id))

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
