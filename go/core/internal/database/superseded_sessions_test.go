package database

import (
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
)

func TestSessionsOnSupersededRevisionsAreListedByID(t *testing.T) {
	client, ctx := NewClient(setupTestDB(t), "public"), t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision-1", "assistant", "kagent")
	readySession := func() *apiv1alpha1.Session {
		t.Helper()
		session, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
		require.NoError(t, err)
		session, err = markSessionReady(ctx, client, session.Id, "agent.example")
		require.NoError(t, err)
		return session
	}
	ids := func(sessions []*apiv1alpha1.Session) []string {
		result := make([]string, 0, len(sessions))
		for _, session := range sessions {
			result = append(result, session.GetId())
		}
		return result
	}
	first, second := readySession(), readySession()
	if first.Id > second.Id {
		first, second = second, first
	}
	suspending := readySession()
	_, err := client.BeginSessionOperation(ctx, suspending.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND)
	require.NoError(t, err)

	listed, err := client.ListSessionsOnSupersededRevisions(ctx, "", 10)
	require.NoError(t, err)
	require.Empty(t, listed, "every session is on its agent's current revision")
	_, err = client.GetCurrentRuntimeRevision(ctx, "revision-1")
	require.NoError(t, err)

	// The agent is re-rendered: revision-2 becomes current.
	current := RuntimeRevision{
		Revision: "revision-2", Namespace: "team-a", AgentName: "assistant", AgentUID: "assistant-uid",
		SourceSnapshot: []byte("{}"), AgentCard: &a2apb.AgentCard{}, EgressDestinations: []string{},
		ActorTemplateAtespace: "team-a", ActorTemplateName: "revision-2-actor-template", ActorTemplateUID: "revision-2-actor-uid",
	}
	require.NoError(t, client.UpsertAgentDefinition(ctx, AgentDefinition{
		Namespace: "team-a", AgentName: "assistant", AgentUID: "assistant-uid", DesiredRevision: current.Revision,
	}))
	require.NoError(t, client.RecordRuntimeRevision(ctx, current, true))
	moved := readySession()
	require.Equal(t, "revision-2", moved.GetPreparedRevision(), "a new session takes the current revision")

	resolved, err := client.GetCurrentRuntimeRevision(ctx, "revision-1")
	require.NoError(t, err)
	require.Equal(t, "revision-2", resolved.Revision, "the current revision of the agent revision-1 was rendered for")
	require.Equal(t, current.ActorTemplateName, resolved.ActorTemplateName)

	listed, err = client.ListSessionsOnSupersededRevisions(ctx, "", 10)
	require.NoError(t, err)
	require.Equal(t, []string{first.Id, second.Id}, ids(listed), "READY sessions without an operation on the superseded revision, by id; the new session is on the current one")
	require.Equal(t, "revision-1", listed[0].GetPreparedRevision())

	listed, err = client.ListSessionsOnSupersededRevisions(ctx, "", 1)
	require.NoError(t, err)
	require.Equal(t, []string{first.Id}, ids(listed))
	listed, err = client.ListSessionsOnSupersededRevisions(ctx, first.Id, 1)
	require.NoError(t, err)
	require.Equal(t, []string{second.Id}, ids(listed), "the page after an id")

	dispatch := uuid.New()
	require.NoError(t, client.ReserveSessionDispatch(ctx, second.Id, dispatch, "next"))
	listed, err = client.ListSessionsOnSupersededRevisions(ctx, "", 10)
	require.NoError(t, err)
	require.Equal(t, []string{first.Id}, ids(listed), "a session with a turn reserved is left to the turn")
	_, err = client.RevokeSessionDispatch(ctx, second.Id, dispatch, "next")
	require.NoError(t, err)

	_, err = client.RepointSession(ctx, first.Id, "revision-1", current.Revision)
	require.NoError(t, err)
	listed, err = client.ListSessionsOnSupersededRevisions(ctx, "", 10)
	require.NoError(t, err)
	require.Equal(t, []string{second.Id}, ids(listed), "a moved session leaves the listing")

	// The agent retired: its sessions have no current revision to move to.
	require.NoError(t, client.RetireAgentIdentities(ctx, "team-a", "assistant", nil))
	listed, err = client.ListSessionsOnSupersededRevisions(ctx, "", 10)
	require.NoError(t, err)
	require.Empty(t, listed)
	_, err = client.GetCurrentRuntimeRevision(ctx, "revision-1")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestRepointSessionIsIdempotentAndYieldsToAnOperation(t *testing.T) {
	client, ctx := NewClient(setupTestDB(t), "public"), t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision-1", "assistant", "kagent")
	sessionFixture(t, client, ctx, "team-a", "revision-2", "assistant", "kagent")
	session, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	_, err = markSessionReady(ctx, client, session.Id, "agent.example")
	require.NoError(t, err)
	require.Equal(t, "revision-2", session.GetPreparedRevision(), "a new session takes the current revision")

	moved, err := client.RepointSession(ctx, session.Id, "revision-2", "revision-1")
	require.NoError(t, err)
	require.Equal(t, "revision-1", moved.GetPreparedRevision())
	stored, err := client.GetSessionByID(ctx, session.Id)
	require.NoError(t, err)
	require.Equal(t, "revision-1", stored.GetPreparedRevision(), "the column records the move")
	again, err := client.RepointSession(ctx, session.Id, "revision-2", "revision-1")
	require.NoError(t, err, "a repoint another caller recorded is accepted")
	require.Equal(t, "revision-1", again.GetPreparedRevision())
	require.Equal(t, moved.GetUpdatedAt().AsTime(), again.GetUpdatedAt().AsTime(), "and changes nothing")

	_, err = client.BeginSessionOperation(ctx, session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND)
	require.NoError(t, err)
	refused, err := client.RepointSession(ctx, session.Id, "revision-1", "revision-2")
	require.ErrorIs(t, err, ErrConflict, "a lifecycle operation that claimed the session wins")
	require.Equal(t, "revision-1", refused.GetPreparedRevision(), "the refusal reports the session as it is")
	require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND, refused.GetOperation())
}

func TestRepointSessionOperationRecordsOnlyTheClaimingResume(t *testing.T) {
	client, ctx := NewClient(setupTestDB(t), "public"), t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision-1", "assistant", "kagent")
	session, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	_, err = markSessionReady(ctx, client, session.Id, "agent.example")
	require.NoError(t, err)
	_, err = finishSessionOperation(ctx, client, session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND, "")
	require.NoError(t, err)
	sessionFixture(t, client, ctx, "team-a", "revision-2", "assistant", "kagent")

	operation, err := client.BeginSessionOperation(ctx, session.Id, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_RESUME)
	require.NoError(t, err)
	executor := uuid.New()
	require.ErrorIs(t, client.RepointSessionOperation(ctx, session.Id, operation.ID, executor, "revision-2"), ErrConflict, "only the claiming executor records a move")
	claimed, err := client.ClaimSessionOperation(ctx, session.Id, operation.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, client.RepointSessionOperation(ctx, session.Id, operation.ID, executor, "revision-2"))
	require.NoError(t, client.RepointSessionOperation(ctx, session.Id, operation.ID, executor, "revision-2"), "a repeated record is accepted")
	resumed, err := client.FinishSessionOperation(ctx, session.Id, operation.ID, executor, "", "", "")
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_READY, resumed.GetState())
	require.Equal(t, "revision-2", resumed.GetPreparedRevision(), "the session resumed on the current revision")
}
