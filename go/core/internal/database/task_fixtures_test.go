package database

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
)

// saveRuntimeTask runs the runtime persistence protocol for fixtures. Tests of
// versions, retries, or settlement call the individual store operations directly.
func saveRuntimeTask(t *testing.T, client *Client, instanceID string, task *a2a.Task, event a2a.Event, snapshot *AgentInstanceTaskSnapshot) error {
	t.Helper()
	ctx := t.Context()
	stored, version, err := client.GetVersionedAgentInstanceTask(ctx, instanceID, string(task.ID))
	digest := taskMutationHash(uuid.NewString())
	if errors.Is(err, ErrNotFound) {
		version, err = client.CreateRuntimeTask(ctx, instanceID, digest, task, "")
	} else if err == nil {
		version, err = client.UpdateAgentInstanceTask(ctx, instanceID, version, digest, task, event, "")
	}
	if err != nil {
		return err
	}
	boundary := task.Status.State.Terminal() || ((task.Status.State == a2a.TaskStateInputRequired || task.Status.State == a2a.TaskStateAuthRequired) && (stored == nil || stored.Status.State != task.Status.State))
	if !boundary {
		return nil
	}
	if err := client.SettleAgentInstanceTask(ctx, instanceID, string(task.ID), version); err != nil {
		return err
	}
	work, err := client.ClaimInstanceQuiescence(ctx)
	if err != nil {
		return err
	}
	require.Equal(t, instanceID, work.Instance.Id)
	require.Equal(t, string(task.ID), work.TaskID)
	return client.FinishInstanceQuiescence(ctx, work, snapshot)
}

// finishInstanceOperation simulates successful runtime work through the same
// lifecycle operations used by the service, preserving their checks and fencing.
func finishInstanceOperation(ctx context.Context, client *Client, id string, kind apiv1alpha1.AgentInstanceOperation, authority string) (*apiv1alpha1.AgentInstance, error) {
	work, err := client.BeginAgentInstanceOperation(ctx, id, kind)
	if err != nil {
		return nil, err
	}
	if work.Instance.Operation == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED {
		return work.Instance, nil
	}
	executor := uuid.New()
	claimed, err := client.ClaimAgentInstanceOperation(ctx, id, work.ID, executor)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, fmt.Errorf("fixture lifecycle operation already claimed: %w", ErrConflict)
	}
	actorUID := ""
	if kind == apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_CREATE {
		actorUID = "actor-" + id
	}
	return client.FinishAgentInstanceOperation(ctx, id, work.ID, executor, authority, actorUID, "")
}

func deleteInstance(ctx context.Context, client *Client, id string) error {
	_, err := finishInstanceOperation(ctx, client, id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE, "")
	return err
}

// taskMutationHash supplies a deterministic storage mutation digest to fixtures.
func taskMutationHash(value string) []byte {
	hash := sha256.Sum256([]byte(value))
	return hash[:]
}

func waitingTaskFixture(t *testing.T, client *Client) (*apiv1alpha1.AgentInstance, *a2a.Task) {
	t.Helper()
	instance, _, err := client.CreateAgentInstance(t.Context(), newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	instance, err = markAgentInstanceReady(t.Context(), client, instance.Id, "agent.example")
	require.NoError(t, err)
	task := newAgentInstanceTask("task", "initial")
	task.ContextID = instance.ContextId
	_, err = client.CreateRuntimeTask(t.Context(), instance.Id, taskMutationHash("initial request"), task, "")
	require.NoError(t, err)
	task.Status = a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Which database?"))}
	require.NoError(t, saveRuntimeTask(t, client, instance.Id, task, task, nil))
	return instance, task
}
