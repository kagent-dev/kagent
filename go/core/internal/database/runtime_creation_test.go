package database

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestRuntimeCreationUsesLatestSuccessfulRevision(t *testing.T) {
	for _, kind := range []string{"session", "sandbox"} {
		t.Run(kind, func(t *testing.T) {
			pool := setupTestDB(t)
			client := NewClient(pool, "public")
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			var create func(*Client, string) (string, bool, error)
			var desire func(*Client, string) error
			var prepare func(string)
			var unresolved string
			if kind == "session" {
				prepare = func(revision string) {
					sessionFixture(t, client, ctx, "team-a", revision, "assistant", "kagent")
				}
				create = func(c *Client, requestID string) (string, bool, error) {
					session, created, err := c.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), requestID)
					return session.GetPreparedRevision(), created, err
				}
				desire = func(c *Client, revision string) error {
					err := c.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team-a", AgentName: "assistant", AgentUID: "assistant-uid", DesiredRevision: revision})
					return err
				}
			} else {
				input, options := sandboxFixture(t, client, "revision-1")
				prepare = func(revision string) {
					_, _ = sandboxFixture(t, client, revision)
				}
				create = func(c *Client, requestID string) (string, bool, error) {
					request := proto.CloneOf(input)
					request.Id = uuid.NewString()
					instance, created, err := c.CreateSandbox(ctx, request, requestID, options)
					return instance.GetPreparedRevision(), created, err
				}
				desire = func(c *Client, revision string) error {
					err := c.UpsertSandboxTemplateDefinition(ctx, SandboxTemplateDefinition{Namespace: "team-a", SandboxTemplateName: "scratch", SandboxTemplateUID: "uid", DesiredRevision: revision})
					return err
				}
				unresolved = "pending:uid:2"
			}
			prepare("revision-1")
			revision, created, err := create(client, "original")
			require.NoError(t, err)
			require.True(t, created)
			require.Equal(t, "revision-1", revision)

			// Unresolved and pending edits keep the last successful runtime available.
			// Retries retain their original revision even after a newer one is ready.
			for _, desired := range []string{unresolved, "revision-2"} {
				require.NoError(t, desire(client, desired))
				revision, created, err = create(client, "fresh-"+desired)
				require.NoError(t, err)
				require.True(t, created)
				require.Equal(t, "revision-1", revision)
				revision, created, err = create(client, "original")
				require.NoError(t, err)
				require.False(t, created)
				require.Equal(t, "revision-1", revision)
			}
			prepare("revision-2")
			revision, created, err = create(client, "fresh")
			require.NoError(t, err)
			require.True(t, created)
			require.Equal(t, "revision-2", revision)
			revision, created, err = create(client, "original")
			require.NoError(t, err)
			require.False(t, created)
			require.Equal(t, "revision-1", revision)
		})
	}
}

func TestReferenceRevocationBlocksNewSessions(t *testing.T) {
	pool := setupTestDB(t)
	client := NewClient(pool, "public")
	ctx := t.Context()
	sessionFixture(t, client, ctx, "team-a", "revision-1", "assistant", "kagent")
	request := newSessionRequest(uuid.NewString(), "assistant", "kagent", "")
	pinned, _, err := client.CreateSession(ctx, request, "pinned")
	require.NoError(t, err)
	prepared, err := client.GetPreparedAgentRevision(ctx, "team-a", "assistant-uid")
	require.NoError(t, err)
	require.Equal(t, "revision-1", prepared.Revision)
	_, err = client.GetPreparedAgentRevision(ctx, "team-a", "other-uid")
	require.ErrorIs(t, err, ErrNotFound)
	// Revoke the fallback while allowing a different desired revision to prepare.
	require.NoError(t, client.UpsertAgentDefinition(ctx, AgentDefinition{Namespace: "team-a", AgentName: "assistant", AgentUID: "assistant-uid", DesiredRevision: "revision-2", RevokePreparedRevision: true}))
	_, err = client.GetPreparedAgentRevision(ctx, "team-a", "assistant-uid")
	require.ErrorIs(t, err, ErrNotFound)
	_, created, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), "denied")
	require.ErrorIs(t, err, ErrNotFound)
	require.False(t, created)
	retried, created, err := client.CreateSession(ctx, request, "pinned")
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, pinned.PreparedRevision, retried.PreparedRevision)
	// A delayed readiness observation cannot restore a revoked desired revision.
	revision, err := client.GetRuntimeRevision(ctx, "revision-1")
	require.NoError(t, err)
	require.NoError(t, client.RecordRuntimeRevision(ctx, *revision, RecordRevisionOptions{Ready: true}))
	_, _, err = client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), "still-denied")
	require.ErrorIs(t, err, ErrNotFound)
	sessionFixture(t, client, ctx, "team-a", "revision-2", "assistant", "kagent")
	fresh, _, err := client.CreateSession(ctx, newSessionRequest(uuid.NewString(), "assistant", "kagent", ""), "restored")
	require.NoError(t, err)
	require.Equal(t, "revision-2", fresh.PreparedRevision)
}
