package database

import (
	"slices"
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
)

func TestRuntimeEgressRetainsSourceAndListsSuspendedActors(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	revision := RuntimeRevision{Revision: "agent", Namespace: "team-a", AgentName: "assistant", AgentUID: "old-uid", SourceSnapshot: []byte(`{}`), AgentCard: &a2apb.AgentCard{}, ActorTemplateAtespace: "team-a", ActorTemplateName: "runtime", TokenPassthroughOrigins: []string{"https://model.example.com:443"}, EgressDestinations: []string{"https://model.example.com:443"}}
	_, upsertErr := client.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team-a", AgentName: "assistant", AgentUID: "old-uid", DesiredRevision: "agent"})
	require.NoError(t, upsertErr)
	require.NoError(t, client.RecordRuntimeRevision(ctx, revision))
	require.NoError(t, client.PromoteRuntimeRevision(ctx, revision.Revision))
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
	_, upsertErr = client.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team-a", AgentName: "assistant", AgentUID: "new-uid", DesiredRevision: "pending"})
	require.NoError(t, upsertErr)
	sandbox, options := sandboxFixture(t, client, "sandbox")
	created, _, err := client.CreateSandbox(ctx, sandbox, "sandbox", options)
	require.NoError(t, err)
	sandboxOp, err := client.BeginSandboxOperation(ctx, created.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
	require.NoError(t, err)
	claimed, err = client.ClaimSandboxOperation(ctx, created.Id, sandboxOp.ID, uuid.New())
	require.NoError(t, err)
	require.True(t, claimed)
	for _, test := range []struct {
		source RuntimeEgressSource
		want   string
	}{
		{RuntimeEgressSource{Kind: RuntimeEgressAgent, Namespace: "team-a", Name: "assistant"}, instance.Id},
		{RuntimeEgressSource{Kind: RuntimeEgressSandbox, Namespace: "team-a", Name: "scratch"}, created.Id},
		{RuntimeEgressSource{Kind: RuntimeEgressAgent, Namespace: "other", Name: "assistant"}, ""},
		{RuntimeEgressSource{Kind: RuntimeEgressAgent, Namespace: "team-a", Name: "unrelated"}, ""},
	} {
		targets, err := client.ListRuntimeEgressSourceTargets(ctx, test.source, "", 10)
		require.NoError(t, err)
		if test.want == "" {
			require.Empty(t, targets)
		} else {
			require.Len(t, targets, 1)
			require.Equal(t, test.want, targets[0].ID)
		}
	}
	for _, kind := range []RuntimeEgressSourceKind{"", "typo"} {
		_, err := client.ListRuntimeEgressSourceTargets(ctx, RuntimeEgressSource{Kind: kind, Namespace: "team-a", Name: "assistant"}, "", 10)
		require.Error(t, err)
	}
	first, err := client.ListRuntimeEgressTargets(ctx, "", 1)
	require.NoError(t, err)
	require.Len(t, first, 1)
	second, err := client.ListRuntimeEgressTargets(ctx, first[0].ID, 1)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.ElementsMatch(t, []string{instance.Id, created.Id}, []string{first[0].ID, second[0].ID})
	last, err := client.ListRuntimeEgressTargets(ctx, second[0].ID, 1)
	require.NoError(t, err)
	require.Empty(t, last)
	_, err = client.BeginSessionOperation(ctx, instance.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	require.NoError(t, err)
	remaining, err := client.ListRuntimeEgressSourceTargets(ctx, RuntimeEgressSource{Kind: RuntimeEgressAgent, Namespace: "team-a", Name: "assistant"}, "", 10)
	require.NoError(t, err)
	require.Empty(t, remaining)
}

func TestSourceGrantsDetermineEffectivePolicy(t *testing.T) {
	for _, kind := range []RuntimeEgressSourceKind{RuntimeEgressAgent, RuntimeEgressSandbox} {
		t.Run(string(kind), func(t *testing.T) {
			client := NewClient(setupTestDB(t), "public")
			ctx := t.Context()
			upsert := func(uid string, grants egress.Access) (bool, error) {
				if kind == RuntimeEgressAgent {
					return client.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team", AgentName: "source", AgentUID: uid, Grants: grants})
				}
				return client.UpsertSandboxTemplateDefinition(ctx, SandboxTemplateDefinition{Namespace: "team", SandboxTemplateName: "source", SandboxTemplateUID: uid, Grants: grants})
			}
			base := egress.Access{}
			if kind == RuntimeEgressAgent {
				base = egress.Access{Destinations: []string{"https://model.example.com:443"}, Credentials: []egress.Credential{{Origin: "https://model.example.com:443", Header: "authorization", URI: "ate-secret://k8s.io/default/team/model/token"}}}
			}
			initial := egress.Access{Destinations: []string{"https://tickets.example.com"}, Credentials: []egress.Credential{{Origin: "https://tickets.example.com", Header: "Authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/team/tickets/token"}}}
			changed, err := upsert("uid", egress.Access{})
			require.NoError(t, err)
			require.False(t, changed)
			changed, err = upsert("uid", initial)
			require.NoError(t, err)
			require.True(t, changed)
			canonical, err := egress.CanonicalAccess(initial.Destinations, initial.Credentials)
			require.NoError(t, err)
			changed, err = upsert("uid", canonical)
			require.NoError(t, err)
			require.False(t, changed, "canonical equivalence must not trigger fan-out")
			if kind == RuntimeEgressAgent {
				require.NoError(t, client.RecordRuntimeRevision(ctx, RuntimeRevision{Revision: "revision", Namespace: "team", AgentName: "source", AgentUID: "uid", SourceSnapshot: []byte(`{}`), AgentCard: &a2apb.AgentCard{}, EgressDestinations: base.Destinations, Credentials: base.Credentials, ActorTemplateAtespace: "team", ActorTemplateName: "runtime"}))
			} else {
				require.NoError(t, client.RecordSandboxRevision(ctx, SandboxRevision{RuntimeArtifact: RuntimeArtifact{Revision: "revision", Namespace: "team", ActorTemplateAtespace: "team", ActorTemplateName: "runtime"}, SandboxTemplateName: "source", SandboxTemplateUID: "uid", SourceSnapshot: []byte(`{}`)}))
			}
			assertPolicy := func(grants egress.Access) {
				t.Helper()
				policy, err := client.GetRuntimeEgressPolicy(ctx, "revision")
				require.NoError(t, err)
				expected, err := egress.MergeAccess(base, grants, nil)
				require.NoError(t, err)
				require.True(t, slices.Equal(expected.Destinations, policy.Destinations))
				require.True(t, slices.Equal(expected.Credentials, policy.Credentials))
				require.NoError(t, policy.WithheldGrants)
			}
			assertPolicy(canonical)
			_, err = upsert("uid", egress.Access{Destinations: []string{"invalid"}})
			require.Error(t, err)
			assertPolicy(canonical)
			if kind == RuntimeEgressAgent {
				conflict := egress.Access{Destinations: base.Destinations, Credentials: []egress.Credential{{Origin: base.Destinations[0], Header: "authorization", URI: "ate-secret://k8s.io/default/team/other/token"}}}
				changed, err = upsert("uid", conflict)
				require.NoError(t, err)
				require.True(t, changed)
				policy, err := client.GetRuntimeEgressPolicy(ctx, "revision")
				require.NoError(t, err)
				require.Error(t, policy.WithheldGrants)
				require.Equal(t, base, policy.Access)
			}
			if kind == RuntimeEgressAgent {
				require.NoError(t, client.RetireAgentIdentities(ctx, "team", "source", nil))
			} else {
				require.NoError(t, client.RetireSandboxTemplateIdentities(ctx, "team", "source"))
			}
			assertPolicy(egress.Access{})
			changed, err = upsert("uid", canonical)
			require.NoError(t, err)
			require.True(t, changed, "reviving a source restores only its newly supplied grants")
			assertPolicy(canonical)
			changed, err = upsert("replacement", canonical)
			require.NoError(t, err)
			require.True(t, changed, "retiring the old UID requires actor fan-out")
			assertPolicy(egress.Access{})
			changed, err = upsert("replacement", canonical)
			require.NoError(t, err)
			require.False(t, changed)
			_, err = client.GetRuntimeEgressPolicy(ctx, "missing")
			require.ErrorIs(t, err, ErrNotFound)
		})
	}
}

func TestSourceGrantsRollbackWithInvalidRevision(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	grants := egress.Access{Destinations: []string{"https://tickets.example.com:443"}}
	definition := AgentDefinition{Namespace: "team", AgentName: "source", AgentUID: "uid", Grants: grants}
	_, err := client.UpsertAgentDefinition(ctx, definition)
	require.NoError(t, err)
	require.NoError(t, client.RecordRuntimeRevision(ctx, RuntimeRevision{Revision: "agent", Namespace: "team", AgentName: "source", AgentUID: "uid", SourceSnapshot: []byte(`{}`), AgentCard: &a2apb.AgentCard{}, ActorTemplateAtespace: "team", ActorTemplateName: "agent"}))
	require.NoError(t, client.RecordSandboxRevision(ctx, SandboxRevision{RuntimeArtifact: RuntimeArtifact{Revision: "sandbox", Namespace: "team", ActorTemplateAtespace: "team", ActorTemplateName: "sandbox"}, SandboxTemplateName: "scratch", SandboxTemplateUID: "sandbox", SourceSnapshot: []byte(`{}`)}))
	for _, uid := range []string{"uid", "replacement"} {
		definition.AgentUID, definition.DesiredRevision, definition.Grants = uid, "sandbox", egress.Access{}
		_, err = client.UpsertAgentDefinition(ctx, definition)
		require.ErrorIs(t, err, ErrConflict)
		policy, err := client.GetRuntimeEgressPolicy(ctx, "agent")
		require.NoError(t, err)
		require.Equal(t, grants.Destinations, policy.Destinations, "failed upsert must roll back grants and retirement")
		require.Empty(t, policy.Credentials)
	}
}

func TestRuntimeEgressMalformedInputsFailClosed(t *testing.T) {
	for _, test := range []struct{ name, statement string }{
		{"grant credentials", `UPDATE agent_definition SET grant_credentials = '[{"hostname":"legacy.example.com"}]'`},
		{"grant origin", `UPDATE agent_definition SET grant_destinations = ARRAY['invalid']`},
		{"pinned credentials", `UPDATE runtime_revision SET credentials = '[{"hostname":"legacy.example.com"}]'`},
		{"token origin", `UPDATE runtime_revision SET token_passthrough_origins = ARRAY['invalid']`},
		{"missing typed revision", `DELETE FROM agent_revision`},
	} {
		t.Run(test.name, func(t *testing.T) {
			pool := setupTestDB(t)
			client := NewClient(pool, "public")
			ctx := t.Context()
			_, err := client.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team", AgentName: "source", AgentUID: "uid"})
			require.NoError(t, err)
			require.NoError(t, client.RecordRuntimeRevision(ctx, RuntimeRevision{Revision: "revision", Namespace: "team", AgentName: "source", AgentUID: "uid", SourceSnapshot: []byte(`{}`), AgentCard: &a2apb.AgentCard{}, ActorTemplateAtespace: "team", ActorTemplateName: "runtime"}))
			_, err = pool.Exec(ctx, test.statement)
			require.NoError(t, err)
			_, err = client.GetRuntimeEgressPolicy(ctx, "revision")
			require.Error(t, err)
		})
	}
}
