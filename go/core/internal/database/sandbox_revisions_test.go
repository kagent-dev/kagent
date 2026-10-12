package database

import (
	"testing"

	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/stretchr/testify/require"
)

func TestSandboxRevisionPinsEgress(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	input, options := sandboxFixture(t, client, "initial")
	revision := SandboxRevision{
		RuntimeArtifact:     RuntimeArtifact{Revision: "tickets", Kind: "sandbox", Namespace: "team-a", ActorTemplateAtespace: "team-a", ActorTemplateName: "tickets", ActorTemplateUID: "actor-uid"},
		SandboxTemplateName: "scratch", SandboxTemplateUID: "uid", SourceSnapshot: []byte(`{}`),
		EgressDestinations: []string{"https://tickets.example.com:443"},
		Credentials:        []egress.Credential{{Origin: "https://tickets.example.com:443", Header: "authorization", Prefix: "Bearer ", URI: "ate-secret://k8s.io/default/team-a/ticket-auth/token"}},
	}
	require.NoError(t, client.UpsertSandboxTemplateDefinition(ctx, SandboxTemplateDefinition{Namespace: "team-a", SandboxTemplateName: "scratch", SandboxTemplateUID: "uid", DesiredRevision: revision.Revision}))
	require.NoError(t, client.RecordSandboxRevision(ctx, revision, true))
	created, _, err := client.CreateSandbox(ctx, input, "create", options)
	require.NoError(t, err)
	require.Equal(t, revision.Revision, created.PreparedRevision)
	saved, err := client.GetSandboxRevision(ctx, created.PreparedRevision)
	require.NoError(t, err)
	require.Equal(t, revision.EgressDestinations, saved.EgressDestinations)
	require.Equal(t, revision.Credentials, saved.Credentials)
	// Retries may refresh artifact identity, but cannot rewrite an existing grant.
	revision.EgressDestinations, revision.Credentials = nil, nil
	require.NoError(t, client.RecordSandboxRevision(ctx, revision, true))
	_, _ = sandboxFixture(t, client, "new-policy")
	retried, _, err := client.CreateSandbox(ctx, input, "create", options)
	require.NoError(t, err)
	require.Equal(t, created.PreparedRevision, retried.PreparedRevision)
	pinned, err := client.GetSandboxRevision(ctx, retried.PreparedRevision)
	require.NoError(t, err)
	require.Equal(t, saved.EgressDestinations, pinned.EgressDestinations)
	require.Equal(t, saved.Credentials, pinned.Credentials)

	// Reject malformed durable data instead of silently dropping authentication.
	_, err = client.db.Exec(ctx, `UPDATE runtime_revision SET credentials = '[{"hostname":"tickets.example.com","header":"authorization","uri":"ate-secret://k8s.io/default/team-a/ticket-auth/token"}]' WHERE revision = $1`, created.PreparedRevision)
	require.NoError(t, err)
	_, err = client.GetSandboxRevision(ctx, created.PreparedRevision)
	require.ErrorContains(t, err, "decode sandbox revision")
}
