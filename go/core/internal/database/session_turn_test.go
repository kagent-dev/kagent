package database

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

func TestGetSessionTurnFollowsTaskLifecycle(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	_, err = markSessionReady(ctx, client, session.GetId(), "agent.example")
	require.NoError(t, err)
	requireTurn := func(want bool) {
		t.Helper()
		got, running, err := client.GetSessionTurn(ctx, session.GetId())
		require.NoError(t, err)
		require.Equal(t, session.GetId(), got.GetId())
		require.Equal(t, want, running)
	}
	requireTurn(false)

	task := newSessionTask("task", "message")
	task.ContextID = session.GetContextId()
	task.Status.State = a2a.TaskStateSubmitted
	version, err := client.CreateRuntimeTask(ctx, session.GetId(), taskMutationHash("submitted"), task, "")
	require.NoError(t, err)
	requireTurn(true)

	task.Status.State = a2a.TaskStateWorking
	version, err = client.UpdateSessionTask(ctx, session.GetId(), version, taskMutationHash("working"), task, task, "")
	require.NoError(t, err)
	requireTurn(true)

	// The projection stays working until native cleanup settles the boundary.
	task.Status.State = a2a.TaskStateCompleted
	version, err = client.UpdateSessionTask(ctx, session.GetId(), version, taskMutationHash("completed"), task, task, "")
	require.NoError(t, err)
	requireTurn(false)

	require.NoError(t, client.SettleSessionTask(ctx, session.GetId(), string(task.ID), version))
	requireTurn(false)
}

func TestGetSessionTurnEndsWhenDeletionBegins(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision", "assistant", "kagent")
	session, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	_, err = markSessionReady(ctx, client, session.GetId(), "agent.example")
	require.NoError(t, err)
	task := newSessionTask("task", "message")
	task.ContextID = session.GetContextId()
	_, err = client.CreateRuntimeTask(ctx, session.GetId(), taskMutationHash("working"), task, "")
	require.NoError(t, err)

	_, err = client.BeginSessionOperation(ctx, session.GetId(), apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	require.NoError(t, err)
	_, running, err := client.GetSessionTurn(ctx, session.GetId())
	require.NoError(t, err)
	require.False(t, running)

	require.NoError(t, deleteSession(ctx, client, session.GetId()))
	_, _, err = client.GetSessionTurn(ctx, session.GetId())
	require.ErrorIs(t, err, ErrNotFound)
}

func TestGetSessionTurnRejectsUnknownAndMalformedIDs(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	_, _, err := client.GetSessionTurn(t.Context(), uuid.NewString())
	require.ErrorIs(t, err, ErrNotFound)
	_, _, err = client.GetSessionTurn(t.Context(), "not-a-uuid")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNotFound)
}
