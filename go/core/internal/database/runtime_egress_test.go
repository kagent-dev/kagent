package database

import (
	"context"
	"fmt"
	"slices"
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
)

func TestActorTokenGrantPersistenceAndRevocation(t *testing.T) {
	c := NewClient(setupTestDB(t), "public")
	grant := egress.Access{Destinations: []string{"https://mcp.example.com:443"}, Credentials: []egress.Credential{{Origin: "https://mcp.example.com:443", Header: "authorization", Prefix: "Bearer ", ActorToken: &egress.ActorToken{Audiences: []string{"mcp"}, ExpirationSeconds: 900}}}}
	definition := AgentDefinition{Namespace: "team", AgentName: "source", AgentUID: "uid", EgressGrant: grant}
	require.NoError(t, c.UpsertAgentDefinition(t.Context(), definition))
	require.NoError(t, c.RecordRuntimeRevision(t.Context(), RuntimeRevision{Revision: "actor-token", Namespace: "team", AgentName: "source", AgentUID: "uid", SourceSnapshot: []byte(`{}`), AgentCard: &a2apb.AgentCard{}, ActorTemplateAtespace: "team", ActorTemplateName: "runtime"}, RecordRevisionOptions{}))
	policy, err := c.GetRuntimeEgressPolicy(t.Context(), "actor-token")
	require.NoError(t, err)
	require.Equal(t, grant, policy)
	definition.EgressGrant = egress.Access{}
	require.NoError(t, c.UpsertAgentDefinition(t.Context(), definition))
	policy, err = c.GetRuntimeEgressPolicy(t.Context(), "actor-token")
	require.NoError(t, err)
	require.Empty(t, policy.Credentials, "live policy removal revokes Actor token injection on existing revisions")
}

func TestRuntimeEgressRetainsSourceAndListsSuspendedActors(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	revision := RuntimeRevision{Revision: "agent", Namespace: "team-a", AgentName: "scratch", AgentUID: "old-uid", SourceSnapshot: []byte(`{}`), AgentCard: &a2apb.AgentCard{}, ActorTemplateAtespace: "team-a", ActorTemplateName: "runtime", TokenPassthroughTargets: []egresspolicy.GatewayTarget{{Origin: "https://model.example.com:443", Header: "authorization"}}, EgressDestinations: []string{"https://model.example.com:443"}}
	upsertErr := client.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team-a", AgentName: "scratch", AgentUID: "old-uid", DesiredRevision: "agent"})
	require.NoError(t, upsertErr)
	require.NoError(t, client.RecordRuntimeRevision(ctx, revision, RecordRevisionOptions{Ready: true}))
	instance, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "scratch", "kagent", ""), "request")
	require.NoError(t, err)
	pendingTargets, err := client.ListRuntimeEgressSourceTargets(ctx, RuntimeEgressSource{Kind: RuntimeEgressAgent, Namespace: "team-a", Name: "scratch"}, "", 10)
	require.NoError(t, err)
	require.Empty(t, pendingTargets, "unissued creations cannot have actors")
	op, err := client.BeginSessionOperation(ctx, instance.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
	require.NoError(t, err)
	executor := uuid.New()
	claimed, err := client.ClaimSessionOperation(ctx, instance.Id, op.ID, executor)
	require.NoError(t, err)
	require.True(t, claimed)
	pendingTargets, err = client.ListRuntimeEgressSourceTargets(ctx, RuntimeEgressSource{Kind: RuntimeEgressAgent, Namespace: "team-a", Name: "scratch"}, "", 10)
	require.NoError(t, err)
	require.Len(t, pendingTargets, 1, "a claimed creation may already have allocated an actor")
	require.Empty(t, pendingTargets[0].ActorUID, "creation has not yet persisted the actor UID")
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
	upsertErr = client.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team-a", AgentName: "scratch", AgentUID: "new-uid", DesiredRevision: "pending"})
	require.NoError(t, upsertErr)
	sandbox, options := sandboxFixture(t, client, "sandbox")
	created, _, err := client.CreateSandbox(ctx, sandbox, "sandbox", options)
	require.NoError(t, err)
	sandboxOp, err := client.BeginSandboxOperation(ctx, created.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
	require.NoError(t, err)
	claimed, err = client.ClaimSandboxOperation(ctx, created.Id, sandboxOp.ID, uuid.New())
	require.NoError(t, err)
	require.True(t, claimed)
	// Both kinds deliberately share a name; source filtering must preserve kind.
	for _, test := range []struct {
		source RuntimeEgressSource
		want   string
	}{
		{RuntimeEgressSource{Kind: RuntimeEgressAgent, Namespace: "team-a", Name: "scratch"}, instance.Id},
		{RuntimeEgressSource{Kind: RuntimeEgressSandbox, Namespace: "team-a", Name: "scratch"}, created.Id},
		{RuntimeEgressSource{Kind: RuntimeEgressAgent, Namespace: "other", Name: "scratch"}, ""},
		{RuntimeEgressSource{Kind: RuntimeEgressAgent, Namespace: "team-a", Name: "unrelated"}, ""},
	} {
		targets, err := client.ListRuntimeEgressSourceTargets(ctx, test.source, "", 10)
		require.NoError(t, err)
		if test.want == "" {
			require.Empty(t, targets)
		} else {
			require.Len(t, targets, 1)
			require.Equal(t, test.want, targets[0].ID)
			if test.source.Kind == RuntimeEgressAgent {
				require.Equal(t, "actor-uid", targets[0].ActorUID)
			} else {
				require.Empty(t, targets[0].ActorUID)
			}
		}
	}
	for _, kind := range []RuntimeEgressSourceKind{"", "typo"} {
		_, err := client.ListRuntimeEgressSourceTargets(ctx, RuntimeEgressSource{Kind: kind, Namespace: "team-a", Name: "scratch"}, "", 10)
		require.Error(t, err)
	}
	// Page multiple allocated instances of one source without visiting other kinds.
	sandbox.Id = uuid.NewString()
	secondSandbox, _, err := client.CreateSandbox(ctx, sandbox, "second-sandbox", options)
	require.NoError(t, err)
	sandboxOp, err = client.BeginSandboxOperation(ctx, secondSandbox.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_CREATE)
	require.NoError(t, err)
	claimed, err = client.ClaimSandboxOperation(ctx, secondSandbox.Id, sandboxOp.ID, uuid.New())
	require.NoError(t, err)
	require.True(t, claimed)
	source := RuntimeEgressSource{Kind: RuntimeEgressSandbox, Namespace: "team-a", Name: "scratch"}
	first, err := client.ListRuntimeEgressSourceTargets(ctx, source, "", 1)
	require.NoError(t, err)
	require.Len(t, first, 1)
	second, err := client.ListRuntimeEgressSourceTargets(ctx, source, first[0].ID, 1)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.ElementsMatch(t, []string{created.Id, secondSandbox.Id}, []string{first[0].ID, second[0].ID})
	last, err := client.ListRuntimeEgressSourceTargets(ctx, source, second[0].ID, 1)
	require.NoError(t, err)
	require.Empty(t, last)
	_, err = client.BeginSessionOperation(ctx, instance.Id, apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE)
	require.NoError(t, err)
	remaining, err := client.ListRuntimeEgressSourceTargets(ctx, RuntimeEgressSource{Kind: RuntimeEgressAgent, Namespace: "team-a", Name: "scratch"}, "", 10)
	require.NoError(t, err)
	require.Empty(t, remaining)
}

func TestRuntimeEgressUnallowedTokenOriginsFailClosed(t *testing.T) {
	many := make([]string, 257)
	for i := range many {
		many[i] = fmt.Sprintf("https://model-%d.example.com", i)
	}
	for _, test := range []struct {
		name    string
		origins []string
	}{
		{"more origins than policy rules", many},
		{"different protocols on the same port", []string{"http://model.example.com:443", "https://model.example.com:443"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient(setupTestDB(t), "public")
			ctx := t.Context()
			var targets []egresspolicy.GatewayTarget
			for _, origin := range test.origins {
				targets = append(targets, egresspolicy.GatewayTarget{Origin: origin, Header: "authorization"})
			}
			require.NoError(t, client.RecordRuntimeRevision(ctx, RuntimeRevision{
				Revision: "revision", Namespace: "team", AgentName: "source", AgentUID: "uid",
				SourceSnapshot: []byte(`{}`), AgentCard: &a2apb.AgentCard{}, ActorTemplateAtespace: "team", ActorTemplateName: "runtime",
				TokenPassthroughTargets: targets,
			}, RecordRevisionOptions{}))
			policy, err := client.GetRuntimeEgressPolicy(ctx, "revision")
			require.Error(t, err)
			require.Empty(t, policy.Destinations, "invalid token destinations cannot grant network access")
		})
	}
}

func TestRuntimeEgressCanonicalTokenOriginWithholdsCredentials(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	err := client.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team", AgentName: "source", AgentUID: "uid", EgressGrant: egress.Access{
		Destinations: []string{"https://model.example.com"},
		Credentials:  []egress.Credential{{Origin: "https://model.example.com", Header: "authorization", URI: "ate-secret://k8s.io/default/team/model/token"}},
	}})
	require.NoError(t, err)
	require.NoError(t, client.RecordRuntimeRevision(ctx, RuntimeRevision{
		Revision: "revision", Namespace: "team", AgentName: "source", AgentUID: "uid",
		SourceSnapshot: []byte(`{}`), AgentCard: &a2apb.AgentCard{}, ActorTemplateAtespace: "team", ActorTemplateName: "runtime",
		EgressDestinations:      []string{"https://model.example.com:443"},
		TokenPassthroughTargets: []egresspolicy.GatewayTarget{{Origin: "https://MODEL.example.com.", Header: "Authorization"}},
	}, RecordRevisionOptions{}))
	policy, err := client.GetRuntimeEgressPolicy(ctx, "revision")
	require.NoError(t, err)
	require.Equal(t, []string{"https://model.example.com:443"}, policy.Destinations)
	require.Empty(t, policy.Credentials)
}

func TestSourceGrantsDetermineEffectivePolicy(t *testing.T) {
	for _, kind := range []RuntimeEgressSourceKind{RuntimeEgressAgent, RuntimeEgressSandbox} {
		t.Run(string(kind), func(t *testing.T) {
			client := NewClient(setupTestDB(t), "public")
			ctx := t.Context()
			upsert := func(uid string, grants egress.Access) error {
				if kind == RuntimeEgressAgent {
					return client.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team", AgentName: "source", AgentUID: uid, EgressGrant: grants})
				}
				return client.UpsertSandboxTemplateDefinition(ctx, SandboxTemplateDefinition{Namespace: "team", SandboxTemplateName: "source", SandboxTemplateUID: uid, EgressGrant: grants})
			}
			base := egress.Access{}
			if kind == RuntimeEgressAgent {
				base = egress.Access{Destinations: []string{"https://model.example.com:443"}, Credentials: []egress.Credential{{Origin: "https://model.example.com:443", Header: "authorization", URI: "ate-secret://k8s.io/default/team/model/token"}}}
			}
			initial := egress.Access{Destinations: []string{"https://tickets.example.com"}, Credentials: []egress.Credential{{Origin: "https://tickets.example.com", Header: "Authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/team/tickets/token"}}}
			err := upsert("uid", egress.Access{})
			require.NoError(t, err)
			err = upsert("uid", initial)
			require.NoError(t, err)
			canonical, err := egress.CanonicalAccess(initial.Destinations, initial.Credentials, nil)
			require.NoError(t, err)
			err = upsert("uid", canonical)
			require.NoError(t, err)
			if kind == RuntimeEgressAgent {
				require.NoError(t, client.RecordRuntimeRevision(ctx, RuntimeRevision{Revision: "revision", Namespace: "team", AgentName: "source", AgentUID: "uid", SourceSnapshot: []byte(`{}`), AgentCard: &a2apb.AgentCard{}, EgressDestinations: base.Destinations, Credentials: base.Credentials, ActorTemplateAtespace: "team", ActorTemplateName: "runtime"}, RecordRevisionOptions{}))
			} else {
				require.NoError(t, client.RecordSandboxRevision(ctx, SandboxRevision{RuntimeArtifact: RuntimeArtifact{Revision: "revision", Namespace: "team", ActorTemplateAtespace: "team", ActorTemplateName: "runtime"}, SandboxTemplateName: "source", SandboxTemplateUID: "uid", SourceSnapshot: []byte(`{}`)}, RecordRevisionOptions{}))
			}
			assertPolicy := func(grants egress.Access) {
				t.Helper()
				policy, err := client.GetRuntimeEgressPolicy(ctx, "revision")
				require.NoError(t, err)
				expected, err := egress.MergeAccess(base, grants)
				require.NoError(t, err)
				require.True(t, slices.Equal(expected.Destinations, policy.Destinations))
				require.True(t, slices.Equal(expected.Credentials, policy.Credentials))
			}
			assertPolicy(canonical)
			err = upsert("uid", egress.Access{Destinations: []string{"invalid"}})
			require.Error(t, err)
			assertPolicy(canonical)
			if kind == RuntimeEgressAgent {
				conflict := egress.Access{Destinations: append(slices.Clone(base.Destinations), canonical.Destinations...), Credentials: append(slices.Clone(canonical.Credentials), egress.Credential{Origin: base.Destinations[0], Header: "authorization", URI: "ate-secret://k8s.io/default/team/other/token"})}
				err = upsert("uid", conflict)
				require.NoError(t, err)
				policy, err := client.GetRuntimeEgressPolicy(ctx, "revision")
				require.NoError(t, err)
				expected, err := egress.MergeAccess(base, canonical)
				require.NoError(t, err)
				require.Equal(t, expected, policy, "older revisions retain nonconflicting grants")
			}
			if kind == RuntimeEgressAgent {
				require.NoError(t, client.RetireAgentIdentities(ctx, "team", "source"))
			} else {
				require.NoError(t, client.RetireSandboxTemplateIdentities(ctx, "team", "source"))
			}
			assertPolicy(egress.Access{})
			err = upsert("uid", canonical)
			require.NoError(t, err)
			assertPolicy(canonical)
			err = upsert("replacement", canonical)
			require.NoError(t, err)
			assertPolicy(egress.Access{})
			err = upsert("replacement", canonical)
			require.NoError(t, err)
			_, err = client.GetRuntimeEgressPolicy(ctx, "missing")
			require.ErrorIs(t, err, ErrNotFound)
		})
	}
}

func TestSourceGrantsRollbackWithInvalidRevision(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	grants := egress.Access{Destinations: []string{"https://tickets.example.com:443"}, TokenPassthroughTargets: []egresspolicy.GatewayTarget{{Origin: "https://tickets.example.com:443", Header: "authorization", Prefix: "Bearer "}}}
	definition := AgentDefinition{Namespace: "team", AgentName: "source", AgentUID: "uid", EgressGrant: grants}
	err := client.UpsertAgentDefinition(ctx, definition)
	require.NoError(t, err)
	require.NoError(t, client.RecordRuntimeRevision(ctx, RuntimeRevision{Revision: "agent", Namespace: "team", AgentName: "source", AgentUID: "uid", SourceSnapshot: []byte(`{}`), AgentCard: &a2apb.AgentCard{}, ActorTemplateAtespace: "team", ActorTemplateName: "agent"}, RecordRevisionOptions{}))
	require.NoError(t, client.RecordSandboxRevision(ctx, SandboxRevision{RuntimeArtifact: RuntimeArtifact{Revision: "sandbox", Namespace: "team", ActorTemplateAtespace: "team", ActorTemplateName: "sandbox"}, SandboxTemplateName: "scratch", SandboxTemplateUID: "sandbox", SourceSnapshot: []byte(`{}`)}, RecordRevisionOptions{}))
	for _, uid := range []string{"uid", "replacement"} {
		definition.AgentUID, definition.DesiredRevision, definition.EgressGrant = uid, "sandbox", egress.Access{}
		err = client.UpsertAgentDefinition(ctx, definition)
		require.ErrorIs(t, err, ErrConflict)
		policy, err := client.GetRuntimeEgressPolicy(ctx, "agent")
		require.NoError(t, err)
		require.Equal(t, grants.Destinations, policy.Destinations, "failed upsert must roll back grants and retirement")
		require.Empty(t, policy.Credentials)
	}
}

func TestRuntimeEgressMalformedInputsFailClosed(t *testing.T) {
	for _, test := range []struct{ name, statement string }{
		{"grant passthrough", `UPDATE agent_definition SET grant_token_passthrough_targets = '[{"origin":"https://tickets.example.com","header":"cookie"}]'`},
		{"grant credentials", `UPDATE agent_definition SET grant_credentials = '[{"hostname":"legacy.example.com"}]'`},
		{"grant origin", `UPDATE agent_definition SET grant_destinations = ARRAY['invalid']`},
		{"pinned credentials", `UPDATE runtime_revision SET credentials = '[{"hostname":"legacy.example.com"}]'`},
		{"token target", `UPDATE runtime_revision SET token_passthrough_targets = '[{"origin":"invalid","header":"authorization"}]'`},
		{"missing typed revision", `DELETE FROM agent_revision`},
	} {
		t.Run(test.name, func(t *testing.T) {
			pool := setupTestDB(t)
			client := NewClient(pool, "public")
			ctx := t.Context()
			err := client.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team", AgentName: "source", AgentUID: "uid"})
			require.NoError(t, err)
			require.NoError(t, client.RecordRuntimeRevision(ctx, RuntimeRevision{Revision: "revision", Namespace: "team", AgentName: "source", AgentUID: "uid", SourceSnapshot: []byte(`{}`), AgentCard: &a2apb.AgentCard{}, ActorTemplateAtespace: "team", ActorTemplateName: "runtime"}, RecordRevisionOptions{}))
			_, err = pool.Exec(ctx, test.statement)
			require.NoError(t, err)
			_, err = client.GetRuntimeEgressPolicy(ctx, "revision")
			require.Error(t, err)
		})
	}
}

func TestReadAdmittedGrantsScopesIdentity(t *testing.T) {
	for _, kind := range []RuntimeEgressSourceKind{RuntimeEgressAgent, RuntimeEgressSandbox} {
		t.Run(string(kind), func(t *testing.T) {
			client := NewClient(setupTestDB(t), "public")
			ctx := t.Context()
			var read func(context.Context, string, string) (egress.Access, error)
			var retire func(context.Context, string, string) error
			upsert := func(uid string, grants egress.Access) error {
				if kind == RuntimeEgressAgent {
					return client.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team", AgentName: "source", AgentUID: uid, EgressGrant: grants})
				}
				return client.UpsertSandboxTemplateDefinition(ctx, SandboxTemplateDefinition{Namespace: "team", SandboxTemplateName: "source", SandboxTemplateUID: uid, EgressGrant: grants})
			}
			if kind == RuntimeEgressAgent {
				read, retire = client.GetAgentEgressGrant, client.RetireAgentIdentities
			} else {
				read, retire = client.GetSandboxEgressGrant, client.RetireSandboxTemplateIdentities
			}
			grants := egress.Access{Destinations: []string{"https://tickets.example.com:443"}, TokenPassthroughTargets: []egresspolicy.GatewayTarget{{Origin: "https://tickets.example.com:443", Header: "authorization", Prefix: "Bearer "}}}
			require.NoError(t, upsert("uid", grants))
			got, err := read(ctx, "team", "uid")
			require.NoError(t, err)
			require.Equal(t, grants.Destinations, got.Destinations)
			require.Equal(t, grants.TokenPassthroughTargets, got.TokenPassthroughTargets)
			for _, scope := range [][2]string{{"team", "new"}, {"other", "uid"}} {
				got, err = read(ctx, scope[0], scope[1])
				require.NoError(t, err)
				require.Empty(t, got.Destinations)
			}
			require.NoError(t, retire(ctx, "team", "source"))
			got, err = read(ctx, "team", "uid")
			require.NoError(t, err)
			require.Empty(t, got.Destinations, "retired grants must not be resurrected")
			require.NoError(t, upsert("replacement", grants))
			got, err = read(ctx, "team", "uid")
			require.NoError(t, err)
			require.Empty(t, got.Destinations, "grants belong to the exact source UID")
		})
	}
}

func TestPassthroughRequiresLiveGrantForEveryRevision(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	target := egresspolicy.GatewayTarget{Origin: "https://mcp.example.com:443", Header: "authorization", Prefix: "Bearer "}
	grant := egress.Access{Destinations: []string{target.Origin}, TokenPassthroughTargets: []egresspolicy.GatewayTarget{target}}
	definition := AgentDefinition{Namespace: "team", AgentName: "agent", AgentUID: "uid", EgressGrant: grant}
	require.NoError(t, client.UpsertAgentDefinition(ctx, definition))
	for _, revision := range []string{"integration", "policy"} {
		r := RuntimeRevision{Revision: revision, Namespace: "team", AgentName: "agent", AgentUID: "uid",
			SourceSnapshot: []byte(`{}`), AgentCard: &a2apb.AgentCard{}, ActorTemplateAtespace: "team", ActorTemplateName: revision,
			EgressDestinations: []string{target.Origin}}
		if revision == "integration" {
			r.TokenPassthroughTargets = []egresspolicy.GatewayTarget{target}
		}
		require.NoError(t, client.RecordRuntimeRevision(ctx, r, RecordRevisionOptions{}))
	}
	check := func(want []egresspolicy.GatewayTarget) {
		t.Helper()
		for _, revision := range []string{"integration", "policy"} {
			access, err := client.GetRuntimeEgressPolicy(ctx, revision)
			require.NoError(t, err)
			require.Equal(t, []string{target.Origin}, access.Destinations)
			require.Equal(t, want, access.TokenPassthroughTargets, revision)
		}
	}
	check(grant.TokenPassthroughTargets)
	definition.EgressGrant = egress.Access{}
	require.NoError(t, client.UpsertAgentDefinition(ctx, definition))
	check(nil)
	definition.EgressGrant = grant
	require.NoError(t, client.UpsertAgentDefinition(ctx, definition))
	check(grant.TokenPassthroughTargets)
	require.NoError(t, client.RetireAgentIdentities(ctx, "team", "agent"))
	check(nil)
	definition.AgentUID = "replacement"
	require.NoError(t, client.UpsertAgentDefinition(ctx, definition))
	check(nil)
}
