package database

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
)

func TestAppliedIntegrationsPersistIndependentlyOfRuntimePreparation(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	grant := egress.IntegrationGrant{Kind: egress.IntegrationRemoteMCPServer, Name: "mcp", UID: "server-uid",
		Targets: []egresspolicy.GatewayTarget{{Origin: "https://mcp.example.com:443", Header: "authorization"}}}
	definition := AgentDefinition{Namespace: "team", AgentName: "agent", AgentUID: "agent-uid", DesiredRevision: "not-yet-prepared",
		EgressGrant: grant.Access(), IntegrationGrants: []egress.IntegrationGrant{grant}}
	require.NoError(t, client.UpsertAgentDefinition(ctx, definition))
	applied, err := client.GetAgentIntegrationGrants(ctx, "team", "agent-uid")
	require.NoError(t, err)
	require.Equal(t, definition.IntegrationGrants, applied)

	// A failed runtime compile clears its desired edge, not its applied inputs.
	definition.DesiredRevision = ""
	definition.EgressGrant = egress.Access{}
	definition.IntegrationGrants[0] = grant.WithAccess(egress.Access{})
	require.NoError(t, client.UpsertAgentDefinition(ctx, definition))
	applied, err = client.GetAgentIntegrationGrants(ctx, "team", "agent-uid")
	require.NoError(t, err)
	require.Equal(t, definition.IntegrationGrants, applied)
	require.Empty(t, applied[0].Access().TokenPassthroughTargets, "source revocations must survive reloading applied configuration")
	access, err := client.GetAgentEgressGrant(ctx, "team", "agent-uid")
	require.NoError(t, err)
	require.Empty(t, access.TokenPassthroughTargets, "source revocation must be independent of the retained configuration")

	for _, identity := range [][2]string{{"other", "agent-uid"}, {"team", "replacement"}} {
		applied, err = client.GetAgentIntegrationGrants(ctx, identity[0], identity[1])
		require.NoError(t, err)
		require.Empty(t, applied)
	}
	invalid := definition
	invalid.IntegrationGrants = []egress.IntegrationGrant{{Kind: grant.Kind, Name: grant.Name, Targets: grant.Targets}}
	require.Error(t, client.UpsertAgentDefinition(ctx, invalid), "unbound source identities must not become durable authority")
	applied, err = client.GetAgentIntegrationGrants(ctx, "team", "agent-uid")
	require.NoError(t, err)
	require.Equal(t, definition.IntegrationGrants, applied, "an invalid replacement must leave applied state intact")
	invalid.IntegrationGrants = []egress.IntegrationGrant{grant}
	invalid.IntegrationGrants[0].RevokedTargets = []egresspolicy.GatewayTarget{{Origin: "https://other.example.com:443", Header: "authorization"}}
	require.Error(t, client.UpsertAgentDefinition(ctx, invalid), "revocations must refer to the source's compiled bindings")

	require.NoError(t, client.RetireAgentIdentities(ctx, "team", "agent"))
	applied, err = client.GetAgentIntegrationGrants(ctx, "team", "agent-uid")
	require.NoError(t, err)
	require.Empty(t, applied)
	definition.AgentUID, definition.IntegrationGrants = "replacement", nil
	require.NoError(t, client.UpsertAgentDefinition(ctx, definition))
	applied, err = client.GetAgentIntegrationGrants(ctx, "team", "replacement")
	require.NoError(t, err)
	require.Empty(t, applied)
}
