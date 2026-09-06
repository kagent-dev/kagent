package database

import (
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestAgentInstanceTombstonePreservesIdentityAndReleasesRuntime(t *testing.T) {
	c := NewClient(setupTestDB(t))
	ctx := t.Context()
	agentInstanceFixture(t, c, ctx, "tombstone-revision", "assistant", "runtime")
	request := newAgentInstanceRequest(uuid.NewString(), "assistant", "runtime", "Remember me")
	instance, _, err := c.CreateAgentInstance(ctx, request, "create-request")
	require.NoError(t, err)
	ready, err := c.MarkAgentInstanceReady(ctx, instance.Id, "runtime.example")
	require.NoError(t, err)
	hash := []byte("share-token-hash")
	_, err = c.CreateAgentInstanceShare(ctx, dbpkg.AgentInstanceShare{ID: uuid.New(), Namespace: "team-a", InstanceID: uuid.MustParse(instance.Id), Permission: "READ_ONLY", TokenHash: hash})
	require.NoError(t, err)
	deleted, err := c.DeleteAgentInstance(ctx, instance.Id)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_DELETED, deleted.State)
	require.NotNil(t, deleted.DeletedAt)
	require.True(t, proto.Equal(deleted.DeletedAt, deleted.UpdatedAt))
	require.Empty(t, deleted.PreparedRevision)
	require.Empty(t, deleted.A2AAuthority)
	require.Equal(t, request.Name, deleted.Name)
	again, err := c.DeleteAgentInstance(ctx, instance.Id)
	require.NoError(t, err)
	require.True(t, proto.Equal(deleted, again))

	// Tombstones do not pin runtime revisions, but keep pair filters and retries working.
	agentInstanceFixture(t, c, ctx, "replacement-revision", "assistant", "runtime")
	require.NoError(t, c.DeleteUnreferencedRuntimeRevision(ctx, "tombstone-revision"))
	_, err = c.GetRuntimeRevision(ctx, "tombstone-revision")
	require.ErrorIs(t, err, dbpkg.ErrNotFound)
	replayed, created, err := c.CreateAgentInstance(ctx, request, "create-request")
	require.NoError(t, err)
	require.False(t, created)
	require.True(t, proto.Equal(deleted, replayed))
	changed := proto.CloneOf(request)
	changed.AgentTemplate.Name = "different"
	_, _, err = c.CreateAgentInstance(ctx, changed, "create-request")
	require.ErrorIs(t, err, dbpkg.ErrIdempotencyConflict)
	query := dbpkg.AgentInstanceQuery{Namespace: "team-a", UserID: "alice", AgentTemplate: "assistant", Harness: "runtime", Limit: 10}
	instances, err := c.ListAgentInstances(ctx, query)
	require.NoError(t, err)
	require.Empty(t, instances)
	query.IncludeDeleted = true
	instances, err = c.ListAgentInstances(ctx, query)
	require.NoError(t, err)
	require.Len(t, instances, 1)
	require.True(t, proto.Equal(deleted, instances[0]))
	query.UserID = "bob"
	instances, err = c.ListAgentInstances(ctx, query)
	require.NoError(t, err)
	require.Empty(t, instances)
	_, err = c.GetAgentInstance(ctx, "team-a", instance.Id, "bob")
	require.ErrorIs(t, err, dbpkg.ErrNotFound)

	_, err = c.GetAgentInstanceShareByTokenHash(ctx, hash)
	require.ErrorIs(t, err, dbpkg.ErrNotFound)
	_, err = c.UpdateAgentInstanceName(ctx, "team-a", instance.Id, "alice", "resurrect")
	require.ErrorIs(t, err, dbpkg.ErrNotFound)
	_, err = c.TransitionAgentInstance(ctx, ready, deleted.State, deleted.Operation)
	require.ErrorIs(t, err, dbpkg.ErrAgentInstanceConflict)
	_, _, err = c.CreateAgentInstanceTask(ctx, instance.Id, []byte("request"), newAgentInstanceTask("task", "message"))
	require.ErrorIs(t, err, dbpkg.ErrAgentInstanceTaskConflict)
	_, err = c.ReserveAgentInstanceCheckpoint(ctx, dbpkg.AgentInstanceCheckpoint{ID: uuid.New(), Namespace: "team-a", SourceInstanceID: uuid.MustParse(instance.Id), UserID: "alice", RequestID: "checkpoint"})
	require.ErrorIs(t, err, dbpkg.ErrAgentInstanceConflict)
}

func TestAgentInstanceDeleteSerializesWithSharesAndRetries(t *testing.T) {
	c := NewClient(setupTestDB(t))
	agentInstanceFixture(t, c, t.Context(), "revision", "assistant", "runtime")
	instance, _, err := c.CreateAgentInstance(t.Context(), newAgentInstanceRequest(uuid.NewString(), "assistant", "runtime", ""), "request")
	require.NoError(t, err)
	var wg sync.WaitGroup
	deleted := make(chan *apiv1alpha1.AgentInstance, 8)
	for i := range 8 {
		wg.Go(func() {
			result, err := c.DeleteAgentInstance(t.Context(), instance.Id)
			if err != nil {
				t.Error(err)
				return
			}
			deleted <- result
		})
		wg.Go(func() {
			_, err := c.CreateAgentInstanceShare(t.Context(), dbpkg.AgentInstanceShare{ID: uuid.New(), Namespace: "team-a", InstanceID: uuid.MustParse(instance.Id), Permission: "READ_ONLY", TokenHash: []byte{byte(i)}})
			if err != nil && !errors.Is(err, dbpkg.ErrNotFound) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	close(deleted)
	var first *apiv1alpha1.AgentInstance
	for instance := range deleted {
		if first == nil {
			first = instance
		}
		require.True(t, proto.Equal(first, instance))
	}
	require.NotNil(t, first)
	shares, err := c.ListAgentInstanceShares(t.Context(), "team-a", instance.Id, "alice", "", 100)
	require.NoError(t, err)
	require.Empty(t, shares)
	for i := range 8 {
		_, err := c.GetAgentInstanceShareByTokenHash(t.Context(), []byte{byte(i)})
		require.ErrorIs(t, err, dbpkg.ErrNotFound)
	}
}
