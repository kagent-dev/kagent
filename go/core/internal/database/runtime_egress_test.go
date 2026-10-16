package database

import (
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

func TestRuntimeEgressRetainsSourceAndListsSuspendedActors(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	revision := RuntimeRevision{Revision: "agent", Namespace: "team-a", AgentName: "assistant", AgentUID: "old-uid", SourceSnapshot: []byte(`{}`), AgentCard: &a2apb.AgentCard{}, ActorTemplateAtespace: "team-a", ActorTemplateName: "runtime", PassthroughOrigins: []string{"https://model.example.com:443"}, EgressDestinations: []string{"https://model.example.com:443"}}
	require.NoError(t, client.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team-a", AgentName: "assistant", AgentUID: "old-uid", DesiredRevision: "agent"}))
	require.NoError(t, client.RecordRuntimeRevision(ctx, revision, true))
	instance, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), "request")
	require.NoError(t, err)
	op, err := client.BeginSessionOperation(ctx, instance.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
	require.NoError(t, err)
	executor := uuid.New()
	claimed, err := client.ClaimSessionOperation(ctx, instance.Id, op.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = client.FinishSessionOperation(ctx, instance.Id, op.ID, executor, "actor.example.com", "actor-uid", "")
	require.NoError(t, err)
	op, err = client.BeginSessionOperation(ctx, instance.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND)
	require.NoError(t, err)
	executor = uuid.New()
	claimed, err = client.ClaimSessionOperation(ctx, instance.Id, op.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	_, err = client.FinishSessionOperation(ctx, instance.Id, op.ID, executor, "", "", "")
	require.NoError(t, err)
	// A recreated Agent must not transfer authorization to an old pinned session.
	require.NoError(t, client.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team-a", AgentName: "assistant", AgentUID: "new-uid", DesiredRevision: "pending"}))
	pinned, err := client.GetRuntimeEgress(ctx, instance.PreparedRevision)
	require.NoError(t, err)
	require.Equal(t, "old-uid", pinned.SourceUID)
	require.Equal(t, "agent", pinned.Kind)
	require.Equal(t, revision.PassthroughOrigins, pinned.PassthroughOrigins)
	sandbox, options := sandboxFixture(t, client, "sandbox")
	created, _, err := client.CreateSandbox(ctx, sandbox, "sandbox", options)
	require.NoError(t, err)
	sandboxOp, err := client.BeginSandboxOperation(ctx, created.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
	require.NoError(t, err)
	claimed, err = client.ClaimSandboxOperation(ctx, created.Id, sandboxOp.ID, uuid.New())
	require.NoError(t, err)
	require.True(t, claimed)
	pinned, err = client.GetRuntimeEgress(ctx, created.PreparedRevision)
	require.NoError(t, err)
	require.Equal(t, "sandbox", pinned.Kind)
	require.Equal(t, "scratch", pinned.SourceName)
	require.Equal(t, "uid", pinned.SourceUID)
	for _, test := range []struct {
		source RuntimeEgressSource
		want   string
	}{
		{RuntimeEgressSource{Kind: "agent", Namespace: "team-a", Name: "assistant"}, instance.Id},
		{RuntimeEgressSource{Kind: "sandbox", Namespace: "team-a", Name: "scratch"}, created.Id},
		{RuntimeEgressSource{Kind: "agent", Namespace: "other", Name: "assistant"}, ""},
		{RuntimeEgressSource{Kind: "agent", Namespace: "team-a", Name: "unrelated"}, ""},
	} {
		targets, err := client.ListRuntimeEgressTargets(ctx, test.source, "", 10)
		require.NoError(t, err)
		if test.want == "" {
			require.Empty(t, targets)
		} else {
			require.Len(t, targets, 1)
			require.Equal(t, test.want, targets[0].ID)
		}
	}

	first, err := client.ListRuntimeEgressTargets(ctx, RuntimeEgressSource{}, "", 1)
	require.NoError(t, err)
	require.Len(t, first, 1)
	second, err := client.ListRuntimeEgressTargets(ctx, RuntimeEgressSource{}, first[0].ID, 1)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.ElementsMatch(t, []string{instance.Id, created.Id}, []string{first[0].ID, second[0].ID})
	last, err := client.ListRuntimeEgressTargets(ctx, RuntimeEgressSource{}, second[0].ID, 1)
	require.NoError(t, err)
	require.Empty(t, last)
	_, err = client.BeginSessionOperation(ctx, instance.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	require.NoError(t, err)
	remaining, err := client.ListRuntimeEgressTargets(ctx, RuntimeEgressSource{}, "", 10)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.Equal(t, created.Id, remaining[0].ID)
}
