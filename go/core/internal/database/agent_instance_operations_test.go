package database

import (
	"testing"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
)

func TestInstanceOperationGenerationAndTombstone(t *testing.T) {
	client := NewClient(setupTestDB(t))
	agentInstanceFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(t.Context(), newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	create, err := client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE, create.Instance.Operation)
	creator := uuid.New()
	claimed, err := client.ClaimAgentInstanceOperation(t.Context(), instance.Id, create.ID, creator)
	require.NoError(t, err)
	require.True(t, claimed)
	ready, err := client.FinishAgentInstanceOperation(t.Context(), instance.Id, create.ID, creator, "runtime.example", "actor-uid", "")
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY, ready.State)
	_, err = client.GetAgentInstanceForRuntime(t.Context(), instance.Id, "actor-uid")
	require.NoError(t, err)
	_, err = client.GetAgentInstanceForRuntime(t.Context(), instance.Id, "replacement-uid")
	require.ErrorIs(t, err, ErrNotFound)

	suspend, err := client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND, suspend.Instance.Operation)
	executor := uuid.New()
	claimed, err = client.ClaimAgentInstanceOperation(t.Context(), instance.Id, suspend.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = client.UpdateAgentInstanceName(t.Context(), instance.Id, "alice", "renamed during suspend")
	require.NoError(t, err)
	suspended, err := client.FinishAgentInstanceOperation(t.Context(), instance.Id, suspend.ID, executor, "", "", "")
	require.NoError(t, err)
	require.Equal(t, "renamed during suspend", suspended.Name)

	resume, err := client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_RESUME)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_RESUME, resume.Instance.Operation)
	resumer := uuid.New()
	claimed, err = client.ClaimAgentInstanceOperation(t.Context(), instance.Id, resume.ID, resumer)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = client.FinishAgentInstanceOperation(t.Context(), instance.Id, resume.ID, resumer, "", "replacement-uid", "")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.FinishAgentInstanceOperation(t.Context(), instance.Id, resume.ID, resumer, "", "", "")
	require.NoError(t, err)
	// The lifecycle state is READY again; that does not restore the old authority.
	_, err = client.FinishAgentInstanceOperation(t.Context(), instance.Id, create.ID, creator, "obsolete.example", "actor-uid", "")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.GetAgentInstanceOperation(t.Context(), instance.Id, suspend.ID)
	require.ErrorIs(t, err, ErrConflict)

	deletion, err := client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE)
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE, deletion.Instance.Operation)
	deleter := uuid.New()
	claimed, err = client.ClaimAgentInstanceOperation(t.Context(), instance.Id, deletion.ID, deleter)
	require.NoError(t, err)
	require.True(t, claimed)
	deleted, err := client.FinishAgentInstanceOperation(t.Context(), instance.Id, deletion.ID, deleter, "", "", "")
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_DELETED, deleted.State)
	require.Empty(t, deleted.PreparedRevision)
	_, err = client.GetAgentInstanceByID(t.Context(), instance.Id)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.GetAgentInstanceOperation(t.Context(), instance.Id, create.ID)
	require.ErrorIs(t, err, ErrConflict)
	current, err := client.GetAgentInstanceOperation(t.Context(), instance.Id, deletion.ID)
	require.NoError(t, err)
	require.Equal(t, deleted.State, current.Instance.State)
	current, err = client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE)
	require.NoError(t, err)
	require.Equal(t, deletion.ID, current.ID)
	require.Equal(t, deleted.State, current.Instance.State)
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
	// Success requires a claim, even when there is no competing executor.
	_, err = client.FinishAgentInstanceOperation(t.Context(), instance.Id, first.ID, uuid.Nil, "", "", "")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.FinishAgentInstanceOperation(t.Context(), instance.Id, first.ID, uuid.Nil, "", "", "preparation unavailable")
	require.NoError(t, err)
	second, err := client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	claimed, err := client.ClaimAgentInstanceOperation(t.Context(), instance.Id, first.ID, uuid.New())
	require.NoError(t, err)
	require.False(t, claimed)
	_, err = client.FinishAgentInstanceOperation(t.Context(), instance.Id, first.ID, uuid.Nil, "", "", "late failure")
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
			claimed, err := client.ClaimAgentInstanceOperation(t.Context(), instance.Id, second.ID, executor)
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
	require.Equal(t, second.Instance.Operation, joined.Instance.Operation)
	_, err = client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE)
	require.ErrorIs(t, err, ErrConflict)
	require.ErrorIs(t, client.DeleteAgentInstance(t.Context(), instance.Id), ErrConflict)
	_, err = client.TransitionAgentInstance(t.Context(), instance, instance.State, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND)
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.FinishAgentInstanceOperation(t.Context(), instance.Id, second.ID, uuid.New(), "", "", "")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.FinishAgentInstanceOperation(t.Context(), instance.Id, second.ID, uuid.Nil, "", "", "cannot release issued work")
	require.ErrorIs(t, err, ErrConflict)
	// Even the claiming executor cannot release possibly issued work as a failure.
	_, err = client.FinishAgentInstanceOperation(t.Context(), instance.Id, second.ID, winner, "", "", "runtime outcome unknown")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.FinishAgentInstanceOperation(t.Context(), instance.Id, second.ID, winner, "", "", "")
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
	claimed, err := client.ClaimAgentInstanceOperation(t.Context(), instance.Id, creation.ID, uuid.New())
	require.NoError(t, err)
	require.False(t, claimed)
	_, err = client.GetAgentInstanceOperation(t.Context(), instance.Id, creation.ID)
	require.ErrorIs(t, err, ErrConflict)
	executor := uuid.New()
	claimed, err = client.ClaimAgentInstanceOperation(t.Context(), instance.Id, deletion.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = client.FinishAgentInstanceOperation(t.Context(), instance.Id, deletion.ID, executor, "", "", "")
	require.NoError(t, err)
	_, err = client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE)
	require.ErrorIs(t, err, ErrNotFound)
	_, err = client.GetAgentInstanceOperation(t.Context(), instance.Id, uuid.New())
	require.ErrorIs(t, err, ErrConflict)
}

func TestLifecycleABARejectsOldClaimAndCompletion(t *testing.T) {
	client := NewClient(setupTestDB(t))
	agentInstanceFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(t.Context(), newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	_, err = markAgentInstanceReady(t.Context(), client, instance.Id, "runtime.example")
	require.NoError(t, err)
	var first *InstanceOperation
	executor := uuid.New()
	for _, kind := range []apiv1alpha1.AgentInstanceOperation{
		apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND,
		apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_RESUME,
	} {
		operation, err := client.BeginAgentInstanceOperation(t.Context(), instance.Id, kind)
		require.NoError(t, err)
		if first == nil {
			first = operation
		}
		claimed, err := client.ClaimAgentInstanceOperation(t.Context(), instance.Id, operation.ID, executor)
		require.NoError(t, err)
		require.True(t, claimed)
		_, err = client.FinishAgentInstanceOperation(t.Context(), instance.Id, operation.ID, executor, "", "", "")
		require.NoError(t, err)
	}
	later, err := client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_SUSPEND)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, later.ID)
	require.Equal(t, first.Instance.State, later.Instance.State)
	require.Equal(t, first.Instance.Operation, later.Instance.Operation)
	claimed, err := client.ClaimAgentInstanceOperation(t.Context(), instance.Id, first.ID, uuid.New())
	require.NoError(t, err)
	require.False(t, claimed)
	_, err = client.FinishAgentInstanceOperation(t.Context(), instance.Id, first.ID, executor, "", "", "")
	require.ErrorIs(t, err, ErrConflict)
	_, err = client.GetAgentInstanceOperation(t.Context(), instance.Id, first.ID)
	require.ErrorIs(t, err, ErrConflict)
	claimed, err = client.ClaimAgentInstanceOperation(t.Context(), instance.Id, later.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
}

func TestLifecycleObservationUsesCurrentFields(t *testing.T) {
	client := NewClient(setupTestDB(t))
	agentInstanceFixture(t, client, t.Context(), "team-a", "revision", "assistant", "kagent")
	instance, _, err := client.CreateAgentInstance(t.Context(), newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	operation, err := client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE)
	require.NoError(t, err)
	executor := uuid.New()
	claimed, err := client.ClaimAgentInstanceOperation(t.Context(), instance.Id, operation.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = client.FinishAgentInstanceOperation(t.Context(), instance.Id, operation.ID, executor, "runtime.example", "actor-uid", "")
	require.NoError(t, err)
	renamed, err := client.UpdateAgentInstanceName(t.Context(), instance.Id, "alice", "current name")
	require.NoError(t, err)
	current, err := client.GetAgentInstanceOperation(t.Context(), instance.Id, operation.ID)
	require.NoError(t, err)
	require.Equal(t, renamed.Name, current.Instance.Name)
	// Already at target is successful without needing a previous Resume receipt.
	resumed, err := client.BeginAgentInstanceOperation(t.Context(), instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_RESUME)
	require.NoError(t, err)
	require.Equal(t, renamed.Name, resumed.Instance.Name)
	require.Equal(t, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED, resumed.Instance.Operation)
	claimed, err = client.ClaimAgentInstanceOperation(t.Context(), instance.Id, operation.ID, uuid.New())
	require.NoError(t, err)
	require.False(t, claimed, "completion must not become executable when its claim is cleared")
}
