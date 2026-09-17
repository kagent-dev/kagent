package database

import (
	"testing"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
)

func TestInstanceOperationRetainsExactOutcome(t *testing.T) {
	client := NewClient(setupTestDB(t))
	agentInstanceFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(t.Context(), newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	create, err := client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE)
	require.NoError(t, err)
	creator := uuid.New()
	claimed, err := client.ClaimAgentInstanceOperation(t.Context(), create.ID, creator)
	require.NoError(t, err)
	require.True(t, claimed)
	ready, err := client.FinishAgentInstanceOperation(t.Context(), create.ID, creator, "runtime.example", "")
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY, ready.State)

	suspend, err := client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND)
	require.NoError(t, err)
	executor := uuid.New()
	claimed, err = client.ClaimAgentInstanceOperation(t.Context(), suspend.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = client.UpdateAgentInstanceName(t.Context(), instance.Id, "alice", "renamed during suspend")
	require.NoError(t, err)
	suspended, err := client.FinishAgentInstanceOperation(t.Context(), suspend.ID, executor, "", "")
	require.NoError(t, err)
	require.Equal(t, "renamed during suspend", suspended.Name)

	resume, err := client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_RESUME)
	require.NoError(t, err)
	resumer := uuid.New()
	claimed, err = client.ClaimAgentInstanceOperation(t.Context(), resume.ID, resumer)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = client.FinishAgentInstanceOperation(t.Context(), resume.ID, resumer, "", "")
	require.NoError(t, err)
	// The lifecycle state is READY again; that does not restore the old authority.
	_, err = client.FinishAgentInstanceOperation(t.Context(), create.ID, creator, "obsolete.example", "")
	require.ErrorIs(t, err, ErrConflict)
	old, err := client.GetAgentInstanceOperation(t.Context(), suspend.ID)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_SUSPENDED, old.Result.State)

	deletion, err := client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE)
	require.NoError(t, err)
	deleter := uuid.New()
	claimed, err = client.ClaimAgentInstanceOperation(t.Context(), deletion.ID, deleter)
	require.NoError(t, err)
	require.True(t, claimed)
	deleted, err := client.FinishAgentInstanceOperation(t.Context(), deletion.ID, deleter, "", "")
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_DELETED, deleted.State)
	require.Empty(t, deleted.PreparedRevision)
	_, err = client.GetAgentInstanceByID(t.Context(), instance.Id)
	require.ErrorIs(t, err, ErrNotFound)
	old, err = client.GetAgentInstanceOperation(t.Context(), create.ID)
	require.NoError(t, err)
	require.Equal(t, "runtime.example", old.Result.A2AAuthority)
	require.Equal(t, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY, old.Result.State)
	old, err = client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE)
	require.NoError(t, err)
	require.Equal(t, deletion.ID, old.ID)
}

func TestInstanceOperationClaimsAndPreparationRecovery(t *testing.T) {
	client := NewClient(setupTestDB(t))
	agentInstanceFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(t.Context(), newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	instance, err = markAgentInstanceReady(t.Context(), client, instance.Id, "runtime.example")
	require.NoError(t, err)
	first, err := client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND)
	require.NoError(t, err)
	_, err = client.FinishAgentInstanceOperation(t.Context(), first.ID, uuid.Nil, "", "preparation unavailable")
	require.NoError(t, err)
	second, err := client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	claimed, err := client.ClaimAgentInstanceOperation(t.Context(), first.ID, uuid.New())
	require.NoError(t, err)
	require.False(t, claimed)
	_, err = client.FinishAgentInstanceOperation(t.Context(), first.ID, uuid.Nil, "", "late failure")
	require.ErrorIs(t, err, ErrConflict)

	type outcome struct {
		executor uuid.UUID
		claimed  bool
		err      error
	}
	results := make(chan outcome, 8)
	for range cap(results) {
		go func() {
			executor := uuid.New()
			claimed, err := client.ClaimAgentInstanceOperation(t.Context(), second.ID, executor)
			results <- outcome{executor, claimed, err}
		}()
	}
	var winner uuid.UUID
	for range cap(results) {
		result := <-results
		require.NoError(t, result.err)
		if result.claimed {
			require.Equal(t, uuid.Nil, winner, "only one replica may authorize runtime work")
			winner = result.executor
		}
	}
	require.NotEqual(t, uuid.Nil, winner)
	joined, err := client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND)
	require.NoError(t, err)
	require.Equal(t, winner, joined.ExecutorID)
	_, err = client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE)
	require.ErrorIs(t, err, ErrConflict)
	require.ErrorIs(t, client.DeleteAgentInstance(t.Context(), instance.Id), ErrConflict)
	_, err = client.TransitionAgentInstance(t.Context(), instance, instance.State, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND)
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.FinishAgentInstanceOperation(t.Context(), second.ID, uuid.New(), "", "")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.FinishAgentInstanceOperation(t.Context(), second.ID, uuid.Nil, "", "cannot release issued work")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.FinishAgentInstanceOperation(t.Context(), second.ID, winner, "", "")
	require.NoError(t, err)
}

func TestDeleteSupersedesOnlyUnissuedCreation(t *testing.T) {
	client := NewClient(setupTestDB(t))
	agentInstanceFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(t.Context(), newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	creation, err := client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE)
	require.NoError(t, err)
	deletion, err := client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE)
	require.NoError(t, err)
	claimed, err := client.ClaimAgentInstanceOperation(t.Context(), creation.ID, uuid.New())
	require.NoError(t, err)
	require.False(t, claimed)
	old, err := client.GetAgentInstanceOperation(t.Context(), creation.ID)
	require.NoError(t, err)
	require.Equal(t, "superseded by deletion", old.Failure)
	executor := uuid.New()
	claimed, err = client.ClaimAgentInstanceOperation(t.Context(), deletion.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = client.FinishAgentInstanceOperation(t.Context(), deletion.ID, executor, "", "")
	require.NoError(t, err)
	_, err = client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.GetAgentInstanceOperation(t.Context(), uuid.New())
	require.ErrorIs(t, err, ErrFailedPrecondition)
}
