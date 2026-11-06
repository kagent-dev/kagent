package database

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeleteMemoryEntryPreservesOtherRecordsAndScopes(t *testing.T) {
	client := NewClient(setupTestDB(t), "public")
	ctx := t.Context()
	first := &Memory{AgentName: "team-a/my-agent", UserID: "owner", Content: "delete", Embedding: makeEmbedding(0.5)}
	second := &Memory{AgentName: "team-a/my-agent", UserID: "owner", Content: "keep", Embedding: makeEmbedding(0.5)}
	require.NoError(t, client.StoreAgentMemories(ctx, first, second))
	require.ErrorIs(t, client.DeleteMemoryEntry(ctx, "team-a/my-agent", "other", first.ID), ErrNotFound)
	require.ErrorIs(t, client.DeleteMemoryEntry(ctx, "other-agent", "owner", first.ID), ErrNotFound)
	require.NoError(t, client.DeleteMemoryEntry(ctx, "team-a/my-agent", "owner", first.ID))
	require.ErrorIs(t, client.DeleteMemoryEntry(ctx, "team-a/my-agent", "owner", first.ID), ErrNotFound)
	memories, err := client.ListAgentMemories(ctx, "team-a/my-agent", "owner")
	require.NoError(t, err)
	require.Len(t, memories, 1)
	require.Equal(t, second.ID, memories[0].ID)
	results, err := client.SearchAgentMemory(ctx, "team-a/my-agent", "owner", makeEmbedding(0.5), 5)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, second.ID, results[0].ID)
}
