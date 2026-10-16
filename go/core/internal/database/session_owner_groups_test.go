package database

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
)

// A session has no owner groups until a turn records them; each record
// replaces the last, and the Session a reader gets does not show them.
func TestSessionOwnerGroups(t *testing.T) {
	client, session := expirationFixture(t)
	ctx := t.Context()

	groups, err := client.GetSessionOwnerGroups(ctx, session.Id)
	require.NoError(t, err)
	require.Empty(t, groups)

	require.NoError(t, client.SetSessionOwnerGroups(ctx, session.Id, []string{"team-a", "cn=ops,dc=example"}))
	groups, err = client.GetSessionOwnerGroups(ctx, session.Id)
	require.NoError(t, err)
	require.Equal(t, []string{"team-a", "cn=ops,dc=example"}, groups)

	require.NoError(t, client.SetSessionOwnerGroups(ctx, session.Id, nil))
	groups, err = client.GetSessionOwnerGroups(ctx, session.Id)
	require.NoError(t, err)
	require.Empty(t, groups, "an owner's turn without groups clears them")

	_, err = client.GetSessionOwnerGroups(ctx, uuid.NewString())
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, client.SetSessionOwnerGroups(ctx, uuid.NewString(), []string{"team-a"}), ErrNotFound)
}

// A checkpoint names the Agent of the revision it was taken on, to its owner
// only.
func TestSessionCheckpointAgent(t *testing.T) {
	client, session := expirationFixture(t)
	ctx := t.Context()
	task := newSessionTask("task-1", "message-1")
	task.ContextID = session.ContextId
	_, err := client.CreateRuntimeTask(ctx, session.Id, taskMutationHash("message-request"), task, "")
	require.NoError(t, err)
	task.Status.State = a2a.TaskStateCompleted
	require.NoError(t, saveRuntimeTask(t, client, session.Id, task, task,
		&SessionTaskSnapshot{Atespace: "team-a", URI: "s3://snapshots/snapshot-1", ContentScope: "DATA"}))
	checkpoint, _, err := client.ReserveSessionCheckpoint(ctx, &apiv1alpha1.Checkpoint{Id: uuid.NewString(), SessionId: session.Id, HeadTaskId: "task-1"}, session.Creator, "checkpoint-request")
	require.NoError(t, err)

	agent, err := client.GetSessionCheckpointAgent(ctx, checkpoint.GetId(), session.Creator)
	require.NoError(t, err)
	require.Equal(t, "team-a", agent.GetNamespace())
	require.Equal(t, "assistant", agent.GetName())

	_, err = client.GetSessionCheckpointAgent(ctx, checkpoint.GetId(), "mallory")
	require.ErrorIs(t, err, ErrNotFound)
}
