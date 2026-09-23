package database

import (
	"context"
	"testing"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
)

func TestDeleteAgentMemoryAliasesAreAtomicAndScoped(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := t.Context()
	for _, memory := range []*Memory{
		{AgentName: "my-agent", UserID: "owner"},
		{AgentName: "my_agent", UserID: "owner"},
		{AgentName: "my-agent", UserID: "other"},
		{AgentName: "my_agent", UserID: "other"},
		{AgentName: "another-agent", UserID: "owner"},
	} {
		memory.Embedding = makeEmbedding(0.5)
		require.NoError(t, client.StoreAgentMemories(ctx, memory))
	}

	// A blocked alias deletion must also preserve the original spelling.
	_, err := db.Exec(ctx, `CREATE TABLE memory_delete_guard (memory_id TEXT REFERENCES memory(id) ON DELETE RESTRICT)`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := db.Exec(context.Background(), `DROP TABLE IF EXISTS memory_delete_guard`)
		require.NoError(t, err)
	})
	_, err = db.Exec(ctx, `INSERT INTO memory_delete_guard SELECT id FROM memory WHERE agent_name = 'my_agent' AND user_id = 'owner'`)
	require.NoError(t, err)
	require.Error(t, client.DeleteAgentMemory(ctx, "my-agent", "owner"))
	memories, err := client.ListAgentMemories(ctx, "my-agent", "owner")
	require.NoError(t, err)
	require.Len(t, memories, 2)

	_, err = db.Exec(ctx, `DROP TABLE memory_delete_guard`)
	require.NoError(t, err)
	require.NoError(t, client.DeleteAgentMemory(ctx, "my-agent", "owner"))
	memories, err = client.ListAgentMemories(ctx, "my-agent", "owner")
	require.NoError(t, err)
	require.Empty(t, memories)
	memories, err = client.ListAgentMemories(ctx, "my-agent", "other")
	require.NoError(t, err)
	require.Len(t, memories, 2)
	memories, err = client.ListAgentMemories(ctx, "another-agent", "owner")
	require.NoError(t, err)
	require.Len(t, memories, 1)
}

func TestStoreAgentMemoriesCommitsIDsWithBatch(t *testing.T) {
	client := NewClient(setupTestDB(t))
	first := &Memory{ID: "original", AgentName: "agent", UserID: "owner", Content: "first", Embedding: makeEmbedding(0.5)}
	invalid := &Memory{AgentName: "agent", UserID: "owner", Content: "second", Embedding: pgvector.NewVector([]float32{1})}
	require.Error(t, client.StoreAgentMemories(t.Context(), first, invalid))
	require.Equal(t, "original", first.ID)
	require.Empty(t, invalid.ID)
	stored, err := client.ListAgentMemories(t.Context(), "agent", "owner")
	require.NoError(t, err)
	require.Empty(t, stored)

	invalid.Embedding = makeEmbedding(0.5)
	require.NoError(t, client.StoreAgentMemories(t.Context(), first, invalid))
	require.NotEqual(t, "original", first.ID)
	require.NotEmpty(t, invalid.ID)
	require.NotEqual(t, first.ID, invalid.ID)
	stored, err = client.ListAgentMemories(t.Context(), "agent", "owner")
	require.NoError(t, err)
	require.Len(t, stored, 2)
}
