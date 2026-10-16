package database

import (
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
	require.NoError(t, client.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team-a", AgentName: "assistant", AgentUID: "old-uid", DesiredRevision: "agent"}))
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
	require.NoError(t, client.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team-a", AgentName: "assistant", AgentUID: "new-uid", DesiredRevision: "pending"}))
	pinned, err := client.GetRuntimeEgress(ctx, instance.PreparedRevision)
	require.NoError(t, err)
	require.Equal(t, "old-uid", pinned.SourceUID)
	require.Equal(t, RuntimeEgressAgent, pinned.Kind)
	require.Equal(t, revision.TokenPassthroughOrigins, pinned.TokenPassthroughOrigins)
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
	require.Equal(t, RuntimeEgressSandbox, pinned.Kind)
	require.Equal(t, "scratch", pinned.SourceName)
	require.Equal(t, "uid", pinned.SourceUID)
	for _, test := range []struct {
		source RuntimeEgressSource
		want   string
	}{
		{RuntimeEgressSource{Kind: RuntimeEgressAgent, Namespace: "team-a", Name: "assistant"}, instance.PreparedRevision},
		{RuntimeEgressSource{Kind: RuntimeEgressSandbox, Namespace: "team-a", Name: "scratch"}, created.PreparedRevision},
		{RuntimeEgressSource{Kind: RuntimeEgressAgent, Namespace: "other", Name: "assistant"}, ""},
		{RuntimeEgressSource{Kind: RuntimeEgressAgent, Namespace: "team-a", Name: "unrelated"}, ""},
	} {
		revisions, err := client.ListRuntimeEgressSourceRevisions(ctx, test.source, "", 10)
		require.NoError(t, err)
		if test.want == "" {
			require.Empty(t, revisions)
		} else {
			require.Equal(t, []string{test.want}, revisions)
		}
	}
	for _, kind := range []RuntimeEgressSourceKind{"", "typo"} {
		_, err := client.ListRuntimeEgressSourceRevisions(ctx, RuntimeEgressSource{Kind: kind, Namespace: "team-a", Name: "assistant"}, "", 10)
		require.Error(t, err)
	}
	first, err := client.ListRuntimeEgressRevisions(ctx, "", 1)
	require.NoError(t, err)
	require.Len(t, first, 1)
	second, err := client.ListRuntimeEgressRevisions(ctx, first[0], 1)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.ElementsMatch(t, []string{instance.PreparedRevision, created.PreparedRevision}, []string{first[0], second[0]})
	last, err := client.ListRuntimeEgressRevisions(ctx, second[0], 1)
	require.NoError(t, err)
	require.Empty(t, last)
	for _, test := range []struct{ revision, id string }{{instance.PreparedRevision, instance.Id}, {created.PreparedRevision, created.Id}} {
		targets, err := client.ListRuntimeEgressTargets(ctx, test.revision, "", 1)
		require.NoError(t, err)
		require.Len(t, targets, 1)
		require.Equal(t, test.id, targets[0].ID)
		targets, err = client.ListRuntimeEgressTargets(ctx, test.revision, test.id, 1)
		require.NoError(t, err)
		require.Empty(t, targets)
	}
	_, err = client.BeginSessionOperation(ctx, instance.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	require.NoError(t, err)
	remaining, err := client.ListRuntimeEgressTargets(ctx, instance.PreparedRevision, "", 10)
	require.NoError(t, err)
	require.Empty(t, remaining)
}

func TestRuntimeEgressPolicyIsMutableControllerOutput(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	revision := RuntimeRevision{Revision: "policy-revision", Namespace: "team-a", AgentName: "assistant", AgentUID: "uid", SourceSnapshot: []byte(`{}`), AgentCard: &a2apb.AgentCard{}, ActorTemplateAtespace: "team-a", ActorTemplateName: "runtime", EgressDestinations: []string{"https://model.example.com:443"}}
	require.NoError(t, client.RecordRuntimeRevision(ctx, revision))
	_, err := client.GetRuntimeEgressPolicy(ctx, revision.Revision)
	require.ErrorIs(t, err, ErrNotFound, "absence must not become an empty allowlist")
	allowed := egress.Access{Destinations: []string{"https://model.example.com:443", "https://tools.example.com:443"}, Credentials: []egress.Credential{{Origin: "https://tools.example.com:443", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/team-a/auth/token"}}}
	changed, err := client.SetRuntimeEgressPolicy(ctx, revision.Revision, allowed)
	require.NoError(t, err)
	require.True(t, changed)
	var before string
	require.NoError(t, client.db.QueryRow(ctx, `SELECT xmin::text FROM runtime_egress_policy WHERE revision = $1`, revision.Revision).Scan(&before))
	equivalent := egress.Access{Destinations: []string{"https://tools.example.com", "https://model.example.com", "https://tools.example.com:443"}, Credentials: allowed.Credentials}
	changed, err = client.SetRuntimeEgressPolicy(ctx, revision.Revision, equivalent)
	require.NoError(t, err)
	require.False(t, changed, "equivalent canonical policies must not trigger actor updates")
	var after string
	require.NoError(t, client.db.QueryRow(ctx, `SELECT xmin::text FROM runtime_egress_policy WHERE revision = $1`, revision.Revision).Scan(&after))
	require.Equal(t, before, after, "unchanged policies must not rewrite the row")
	// A different API replica reads the same intended policy without any K8s cache.
	replica := NewClient(client.db, "public")
	got, err := replica.GetRuntimeEgressPolicy(ctx, revision.Revision)
	require.NoError(t, err)
	require.Equal(t, allowed, got)
	rotated := egress.Access{Destinations: allowed.Destinations, Credentials: []egress.Credential{allowed.Credentials[0]}}
	rotated.Credentials[0].URI = "ate-secret://k8s.io/default/team-a/rotated/token"
	changed, err = client.SetRuntimeEgressPolicy(ctx, revision.Revision, rotated)
	require.NoError(t, err)
	require.True(t, changed, "a credential-only change must update actors")
	changed, err = client.SetRuntimeEgressPolicy(ctx, revision.Revision, egress.Access{Destinations: revision.EgressDestinations})
	require.NoError(t, err)
	require.True(t, changed)
	// Preparation observations must not restore an earlier policy.
	require.NoError(t, client.RecordRuntimeRevision(ctx, revision))
	require.NoError(t, client.PromoteRuntimeRevision(ctx, revision.Revision))
	got, err = replica.GetRuntimeEgressPolicy(ctx, revision.Revision)
	require.NoError(t, err)
	require.Equal(t, revision.EgressDestinations, got.Destinations)
	require.Empty(t, got.Credentials)
	pinned, err := client.GetRuntimeEgress(ctx, revision.Revision)
	require.NoError(t, err)
	require.Equal(t, revision.EgressDestinations, pinned.EgressDestinations)
	require.Empty(t, pinned.Credentials)
	changed, err = client.SetRuntimeEgressPolicy(ctx, "missing", allowed)
	require.ErrorIs(t, err, ErrNotFound)
	require.False(t, changed)
	_, err = client.SetRuntimeEgressPolicy(ctx, revision.Revision, egress.Access{Destinations: []string{"invalid"}})
	require.Error(t, err)
	// Durable malformed data must fail rather than granting a partial policy.
	_, err = client.db.Exec(ctx, `UPDATE runtime_egress_policy SET credentials = '[{"hostname":"legacy","header":"authorization"}]' WHERE revision = $1`, revision.Revision)
	require.NoError(t, err)
	_, err = replica.GetRuntimeEgressPolicy(ctx, revision.Revision)
	require.Error(t, err)
	_, err = client.BeginRuntimeRevisionDeletion(ctx, revision.Revision)
	require.NoError(t, err)
	_, err = client.SetRuntimeEgressPolicy(ctx, revision.Revision, allowed)
	require.ErrorIs(t, err, ErrNotFound, "deleting revisions must remain distinguishable from unchanged policies")
}
