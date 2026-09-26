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
func saveRuntimeTask(t *testing.T, client *Client, sessionID string, task *a2a.Task, event a2a.Event, snapshot *SessionTaskSnapshot) error {
	t.Helper()
	ctx := t.Context()
	stored, version, err := client.GetVersionedSessionTask(ctx, sessionID, string(task.ID))
	digest := taskMutationHash(uuid.NewString())
	if errors.Is(err, ErrNotFound) {
		version, err = client.CreateRuntimeTask(ctx, sessionID, digest, task, "")
	} else if err == nil {
		version, err = client.UpdateSessionTask(ctx, sessionID, version, digest, task, event, "")
	}
	if err != nil {
		return err
	}
	boundary := task.Status.State.Terminal() || ((task.Status.State == a2a.TaskStateInputRequired || task.Status.State == a2a.TaskStateAuthRequired) && (stored == nil || stored.Status.State != task.Status.State))
	if !boundary {
		return nil
	}
	if err := client.SettleSessionTask(ctx, sessionID, string(task.ID), version); err != nil {
		return err
	}
	work, err := client.ClaimSessionQuiescence(ctx)
	if err != nil {
		return err
	}
	require.Equal(t, sessionID, work.Session.Id)
	require.Equal(t, string(task.ID), work.TaskID)
	return client.FinishSessionQuiescence(ctx, work, snapshot)
}

// finishSessionOperation simulates successful runtime work through the same
// lifecycle operations used by the service, preserving their checks and fencing.
func finishSessionOperation(ctx context.Context, client *Client, id string, kind apiv1alpha1.RuntimeOperation, authority string) (*apiv1alpha1.Session, error) {
	work, err := client.BeginSessionOperation(ctx, id, kind)
	if err != nil {
		return nil, err
	}
	if work.Session.Operation == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		return work.Session, nil
	}
	executor := uuid.New()
	claimed, err := client.ClaimSessionOperation(ctx, id, work.ID, executor)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, fmt.Errorf("fixture lifecycle operation already claimed: %w", ErrConflict)
	}
	actorUID := ""
	if kind == apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_CREATE {
		actorUID = "actor-" + id
	}
	return client.FinishSessionOperation(ctx, id, work.ID, executor, authority, actorUID, "")
}

func deleteSession(ctx context.Context, client *Client, id string) error {
	_, err := finishSessionOperation(ctx, client, id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_DELETE, "")
	return err
}

// taskMutationHash supplies a deterministic storage mutation digest to fixtures.
func taskMutationHash(value string) []byte {
	hash := sha256.Sum256([]byte(value))
	return hash[:]
}

func waitingTaskFixture(t *testing.T, client *Client) (*apiv1alpha1.Session, *a2a.Task) {
	t.Helper()
	session, _, err := client.CreateSession(t.Context(), newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	session, err = markSessionReady(t.Context(), client, session.Id, "agent.example")
	require.NoError(t, err)
	task := newSessionTask(uuid.NewString(), "initial")
	task.ContextID = session.ContextId
	_, err = client.CreateRuntimeTask(t.Context(), session.Id, taskMutationHash("initial request"), task, "")
	require.NoError(t, err)
	task.Status = a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Which database?"))}
	require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task, nil))
	return session, task
}
