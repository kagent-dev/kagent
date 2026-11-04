package database

import (
	"strings"
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/kagent-dev/kagent/go/api/bootstrap"
)

func TestBootstrapPublicationAndRetention(t *testing.T) {
	c := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	id := strings.Repeat("a", 64)
	bundle, err := bootstrap.NewBundle(id, []byte(`{"instruction":"`+strings.Repeat("x", bootstrap.MaxPayloadBytes-64)+`"}`), []byte(`{"name":"agent"}`))
	require.NoError(t, err)
	require.NoError(t, c.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team-a", AgentName: "agent", AgentUID: "agent-uid", DesiredRevision: id}))
	r := RuntimeRevision{Revision: id, Namespace: "team-a", AgentName: "agent", AgentUID: "agent-uid", SourceSnapshot: []byte(`{}`), AgentCard: &a2apb.AgentCard{Name: "agent"}, ActorTemplateAtespace: "team-a", ActorTemplateName: "template", ActorTemplateSpecDigest: "spec"}
	for range 2 {
		require.NoError(t, c.PublishBootstrapRevision(ctx, r, bundle))
	}
	binding, err := c.GetBootstrapBinding(ctx, "team-a", "template")
	require.NoError(t, err)
	require.Empty(t, binding.ActorTemplateUID)
	_, err = c.GetBootstrapBundle(ctx, *binding)
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, c.RetireAgentIdentities(ctx, "team-a", "agent"))
	pending, err := c.BeginRuntimeRevisionDeletion(ctx, id)
	require.NoError(t, err)
	require.Nil(t, pending, "a bounded template creation attempt must finish before collection")
	require.NoError(t, c.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team-a", AgentName: "agent", AgentUID: "agent-uid", DesiredRevision: id}))
	r.ActorTemplateUID = "template-uid"
	_, err = c.RecordRuntimeRevision(ctx, r, RecordRevisionOptions{})
	require.NoError(t, err)
	binding, err = c.GetBootstrapBinding(ctx, "team-a", "template")
	require.NoError(t, err)
	got, err := c.GetBootstrapBundle(ctx, *binding)
	require.NoError(t, err)
	require.True(t, proto.Equal(bundle, got))
	changed, err := bootstrap.NewBundle(id, []byte(`{}`), bundle.AgentCardJson)
	require.NoError(t, err)
	err = c.PublishBootstrapRevision(ctx, r, changed)
	require.ErrorIs(t, err, ErrConflict)
	r.ActorTemplateUID = "replacement"
	_, err = c.RecordRuntimeRevision(ctx, r, RecordRevisionOptions{})
	require.NoError(t, err)
	_, err = c.GetBootstrapBundle(ctx, *binding)
	require.ErrorIs(t, err, ErrNotFound, "stale authorization cannot read a replacement binding")
	// Given an observed binding, when a stale source tries to publish, assert
	// that publication fails and leaves the bound payload readable.
	require.NoError(t, c.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team-a", AgentName: "agent", AgentUID: "agent-uid", DesiredRevision: strings.Repeat("c", 64)}))
	require.ErrorIs(t, c.PublishBootstrapRevision(ctx, r, bundle), ErrConflict)
	current, err := c.GetBootstrapBinding(ctx, "team-a", "template")
	require.NoError(t, err)
	got, err = c.GetBootstrapBundle(ctx, *current)
	require.NoError(t, err)
	require.True(t, proto.Equal(bundle, got))
	require.NoError(t, c.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team-a", AgentName: "agent", AgentUID: "agent-uid", DesiredRevision: id}))
	// Publishing again must not clear the observed template UID.
	r.ActorTemplateUID = ""
	err = c.PublishBootstrapRevision(ctx, r, bundle)
	require.NoError(t, err)
	binding, err = c.GetBootstrapBinding(ctx, "team-a", "template")
	require.NoError(t, err)
	require.Equal(t, "replacement", binding.ActorTemplateUID)
	r.ActorTemplateUID = "replacement"
	_, err = c.RecordRuntimeRevision(ctx, r, RecordRevisionOptions{})
	require.NoError(t, err)
	require.NoError(t, c.RetireAgentIdentities(ctx, "team-a", "agent"))
	expireBootstrapLease(t, c, id)
	deleted, err := c.BeginRuntimeRevisionDeletion(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, deleted)
	_, err = c.GetBootstrapBundle(ctx, *binding)
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, c.DeleteRuntimeRevision(ctx, id, "replacement"))
	_, err = c.GetBootstrapBinding(ctx, "team-a", "template")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestBootstrapPreparationLeaseExpiresAndRenews(t *testing.T) {
	c := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	id := strings.Repeat("b", 64)
	bundle, err := bootstrap.NewBundle(id, []byte(`{}`), []byte(`{"name":"agent"}`))
	require.NoError(t, err)
	require.NoError(t, c.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team-a", AgentName: "agent", AgentUID: "agent-uid", DesiredRevision: id}))
	revision := RuntimeRevision{Revision: id, Namespace: "team-a", AgentName: "agent", AgentUID: "agent-uid", SourceSnapshot: []byte(`{}`), AgentCard: &a2apb.AgentCard{Name: "agent"}, ActorTemplateAtespace: "team-a", ActorTemplateName: "pending-template", ActorTemplateSpecDigest: "spec"}
	require.NoError(t, c.PublishBootstrapRevision(ctx, revision, bundle))
	expireBootstrapLease(t, c, id)
	// Given an expired creation attempt, when publication is retried, assert
	// that the renewed lease protects it even if its source is then retired.
	require.NoError(t, c.PublishBootstrapRevision(ctx, revision, bundle))
	require.NoError(t, c.RetireAgentIdentities(ctx, "team-a", "agent"))
	candidate, err := c.BeginRuntimeRevisionDeletion(ctx, id)
	require.NoError(t, err)
	require.Nil(t, candidate)
	unreferenced, err := c.ListUnreferencedRuntimeRevisions(ctx)
	require.NoError(t, err)
	require.Empty(t, unreferenced)
	// Given an abandoned attempt, when its lease expires, assert that GC can
	// collect it without a release call from the crashed controller.
	expireBootstrapLease(t, c, id)
	unreferenced, err = c.ListUnreferencedRuntimeRevisions(ctx)
	require.NoError(t, err)
	require.Len(t, unreferenced, 1)
	candidate, err = c.BeginRuntimeRevisionDeletion(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, candidate)
	require.NoError(t, c.DeleteRuntimeRevision(ctx, id, ""))
	_, err = c.GetBootstrapBinding(ctx, "team-a", "pending-template")
	require.ErrorIs(t, err, ErrNotFound)
}

// Advance the durable deadline without sleeping through the production lease.
func expireBootstrapLease(t *testing.T, c *Client, revision string) {
	t.Helper()
	_, err := c.db.Exec(t.Context(), `UPDATE runtime_bootstrap SET preparing_until = statement_timestamp() - INTERVAL '1 second' WHERE revision = $1`, revision)
	require.NoError(t, err)
}
