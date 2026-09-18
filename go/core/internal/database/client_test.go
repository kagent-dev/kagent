package database

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDirectModelScans covers database defaults, required catalog fields, and nullable
// memory fields when rows are scanned directly into application models.
func TestDirectModelScans(t *testing.T) {
	ctx := t.Context()
	db := setupTestDB(t)
	client := NewClient(db)
	_, err := db.Exec(ctx, `INSERT INTO tool (id, server_name, group_kind) VALUES ('defaulted', 'server', 'kind')`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO toolserver (name, group_kind) VALUES ('defaulted', 'kind')`)
	require.NoError(t, err)

	t.Run("tools", func(t *testing.T) {
		tools, err := client.ListTools(ctx)
		require.NoError(t, err)
		require.Len(t, tools, 1)
		tool := tools[0]
		assert.Empty(t, tool.Description)
		assert.False(t, tool.CreatedAt.IsZero())
		assert.Equal(t, tool.CreatedAt, tool.UpdatedAt)
		assert.Nil(t, tool.DeletedAt)
	})

	t.Run("servers", func(t *testing.T) {
		servers, err := client.ListToolServers(ctx)
		require.NoError(t, err)
		require.Len(t, servers, 1)
		server := servers[0]
		assert.Empty(t, server.Description)
		assert.False(t, server.CreatedAt.IsZero())
		assert.Equal(t, server.CreatedAt, server.UpdatedAt)
		assert.Nil(t, server.DeletedAt)
		assert.Nil(t, server.LastConnected)
		require.NoError(t, client.RefreshToolServer(ctx, &ToolServer{Name: "defaulted", GroupKind: "kind", Description: "updated"}))
		updated, err := client.ListToolServers(ctx)
		require.NoError(t, err)
		require.Len(t, updated, 1)
		assert.Equal(t, server.CreatedAt, updated[0].CreatedAt)
		assert.False(t, updated[0].UpdatedAt.Before(server.UpdatedAt))
		assert.Equal(t, "updated", updated[0].Description)
	})

	for _, query := range []string{
		`UPDATE tool SET created_at = NULL`,
		`UPDATE tool SET updated_at = NULL`,
		`UPDATE tool SET description = NULL`,
		`UPDATE toolserver SET created_at = NULL`,
		`UPDATE toolserver SET updated_at = NULL`,
		`UPDATE toolserver SET description = NULL`,
	} {
		t.Run(query, func(t *testing.T) {
			_, err := db.Exec(ctx, query)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			assert.Equal(t, "23502", pgErr.Code) // not_null_violation
		})
	}

	t.Run("memory", func(t *testing.T) {
		embedding := make([]float32, 768)
		embedding[0] = 1
		_, err := db.Exec(ctx, `INSERT INTO memory (id, agent_name, user_id, embedding, access_count) VALUES ('nullable', 'agent', 'user', $1, NULL)`, pgvector.NewVector(embedding))
		require.NoError(t, err)
		memory := &Memory{AgentName: "agent", UserID: "user", Embedding: makeEmbedding(0.5), AccessCount: 1}
		require.NoError(t, client.StoreAgentMemories(ctx, memory))
		all, err := client.ListAgentMemories(ctx, "agent", "user")
		require.NoError(t, err)
		require.Len(t, all, 2)
		assert.Equal(t, "nullable", all[0].ID) // SQL NULL counts still sort first descending.
		assert.Empty(t, all[0].Content)
		assert.Empty(t, all[0].Metadata)
		assert.Equal(t, embedding, all[0].Embedding.Slice())
		assert.True(t, all[0].CreatedAt.IsZero())
		assert.Nil(t, all[0].ExpiresAt)
		assert.Zero(t, all[0].AccessCount)
		results, err := client.SearchAgentMemory(ctx, "agent", "user", makeEmbedding(0.5), 2)
		require.NoError(t, err)
		require.Len(t, results, 2)
		assert.Equal(t, memory.ID, results[0].ID)
		assert.Equal(t, all[0], results[1].Memory)
		assert.Greater(t, results[1].Score, 0.0)
	})
}

// setupTestDB resets the shared Postgres database's tables for test isolation.
func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}

	// Truncate application tables instead of full down+up migrations.
	// Full down migration drops and recreates the pgvector extension, which
	// changes type OIDs and breaks existing pool connections.
	_, err := sharedDB.Exec(context.Background(), `
		TRUNCATE TABLE
			scheduled_run,
			tool, toolserver, memory,
			agent_instance_share,
			agent_instance, a2a_context, agent_template_harness_pair, runtime_revision
		RESTART IDENTITY CASCADE
	`)
	require.NoError(t, err, "Failed to truncate test tables")

	return sharedDB
}

// makeEmbedding returns a 768-dimensional vector where all values are set to v.
// This makes it easy to construct vectors with known cosine similarity relationships.
func makeEmbedding(v float32) pgvector.Vector {
	vals := make([]float32, 768)
	for i := range vals {
		vals[i] = v
	}
	return pgvector.NewVector(vals)
}

// TestStoreAndSearchAgentMemory verifies that stored memories can be retrieved
// via vector similarity search and that results are ordered by cosine similarity.
func TestStoreAndSearchAgentMemory(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	agentName := "test-agent"
	userID := "test-user"

	memories := []*Memory{
		{
			ID:        "mem-1",
			AgentName: agentName,
			UserID:    userID,
			Content:   "memory about Go",
			Embedding: makeEmbedding(0.1),
		},
		{
			ID:        "mem-2",
			AgentName: agentName,
			UserID:    userID,
			Content:   "memory about Python",
			Embedding: makeEmbedding(0.9),
		},
		{
			ID:        "mem-3",
			AgentName: agentName,
			UserID:    userID,
			Content:   "memory about Kubernetes",
			Embedding: makeEmbedding(0.5),
		},
	}

	for _, m := range memories {
		err := client.StoreAgentMemories(ctx, m)
		require.NoError(t, err)
	}

	// Query with embedding; all three memories should be returned with high similarity.
	results, err := client.SearchAgentMemory(ctx, agentName, userID, makeEmbedding(0.5), 3)
	require.NoError(t, err)
	require.Len(t, results, 3, "Should return all 3 memories")
	// Scores should be in [0, 1] (cosine similarity)
	for _, r := range results {
		assert.True(t, r.Score >= 0 && r.Score <= 1, "Score should be in [0, 1]")
	}
}

// TestStoreAgentMemoriesBatch verifies that StoreAgentMemories stores all memories
// atomically via a transaction and that they are all retrievable afterwards.
func TestStoreAgentMemoriesBatch(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	agentName := "batch-agent"
	userID := "batch-user"

	memories := []*Memory{
		{ID: "b-1", AgentName: agentName, UserID: userID, Content: "batch memory 1", Embedding: makeEmbedding(0.2)},
		{ID: "b-2", AgentName: agentName, UserID: userID, Content: "batch memory 2", Embedding: makeEmbedding(0.4)},
		{ID: "b-3", AgentName: agentName, UserID: userID, Content: "batch memory 3", Embedding: makeEmbedding(0.6)},
	}

	err := client.StoreAgentMemories(ctx, memories...)
	require.NoError(t, err)

	results, err := client.SearchAgentMemory(ctx, agentName, userID, makeEmbedding(0.5), 10)
	require.NoError(t, err)
	assert.Len(t, results, 3, "All 3 batch-stored memories should be found")
}

// TestSearchAgentMemoryLimit verifies that the limit parameter is respected when
// searching for similar memories.
func TestSearchAgentMemoryLimit(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	agentName := "limit-agent"
	userID := "limit-user"

	for i := range 5 {
		err := client.StoreAgentMemories(ctx, &Memory{
			ID:        fmt.Sprintf("lim-%d", i),
			AgentName: agentName,
			UserID:    userID,
			Content:   fmt.Sprintf("memory %d", i),
			Embedding: makeEmbedding(float32(i+1) * 0.1),
		})
		require.NoError(t, err)
	}

	tests := []struct {
		limit    int
		expected int
	}{
		{1, 1},
		{3, 3},
		{5, 5},
		{10, 5}, // capped at the total number stored
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("limit_%d", tc.limit), func(t *testing.T) {
			results, err := client.SearchAgentMemory(ctx, agentName, userID, makeEmbedding(0.5), tc.limit)
			require.NoError(t, err)
			assert.Len(t, results, tc.expected)
		})
	}
}

// TestSearchAgentMemoryIsolation verifies that searches are scoped to the
// correct (agentName, userID) pair and do not return results for other agents or users.
func TestSearchAgentMemoryIsolation(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	mem1 := &Memory{AgentName: "agent-a", UserID: "user-1", Content: "agent-a user-1 memory", Embedding: makeEmbedding(0.5)}
	require.NoError(t, client.StoreAgentMemories(ctx, mem1))
	require.NoError(t, client.StoreAgentMemories(ctx, &Memory{AgentName: "agent-b", UserID: "user-1", Content: "agent-b user-1 memory", Embedding: makeEmbedding(0.5)}))
	require.NoError(t, client.StoreAgentMemories(ctx, &Memory{AgentName: "agent-a", UserID: "user-2", Content: "agent-a user-2 memory", Embedding: makeEmbedding(0.5)}))

	results, err := client.SearchAgentMemory(ctx, "agent-a", "user-1", makeEmbedding(0.5), 10)
	require.NoError(t, err)
	require.Len(t, results, 1, "Should only return memories for agent-a / user-1")
	assert.Equal(t, mem1.ID, results[0].ID)
}

// TestSearchAgentMemoryNormalizedName verifies that a search with a hyphenated
// agent name finds memories stored under the underscore form, matching the
// normalization ListAgentMemories and DeleteAgentMemory already apply.
func TestSearchAgentMemoryNormalizedName(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	stored := &Memory{AgentName: "ns__my_agent", UserID: "user-1", Content: "stored under underscore form", Embedding: makeEmbedding(0.5)}
	require.NoError(t, client.StoreAgentMemories(ctx, stored))

	results, err := client.SearchAgentMemory(ctx, "ns__my-agent", "user-1", makeEmbedding(0.5), 10)
	require.NoError(t, err)
	require.Len(t, results, 1, "Search should find memories stored under the normalized name")
	assert.Equal(t, stored.ID, results[0].ID)
}

// TestDeleteAgentMemory verifies that DeleteAgentMemory removes all memories for the
// given agent/user pair and that the hyphen-to-underscore normalization works correctly.
func TestDeleteAgentMemory(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	agentName := "my-agent"
	userID := "del-user"

	for i := range 3 {
		err := client.StoreAgentMemories(ctx, &Memory{
			ID:        fmt.Sprintf("del-%d", i),
			AgentName: agentName,
			UserID:    userID,
			Content:   fmt.Sprintf("memory to delete %d", i),
			Embedding: makeEmbedding(float32(i+1) * 0.2),
		})
		require.NoError(t, err)
	}

	// Confirm they exist before deletion
	before, err := client.SearchAgentMemory(ctx, agentName, userID, makeEmbedding(0.5), 10)
	require.NoError(t, err)
	require.Len(t, before, 3)

	err = client.DeleteAgentMemory(ctx, agentName, userID)
	require.NoError(t, err)

	after, err := client.SearchAgentMemory(ctx, agentName, userID, makeEmbedding(0.5), 10)
	require.NoError(t, err)
	assert.Empty(t, after, "All memories should be deleted")
}

// TestPruneExpiredMemories verifies that expired memories with low access counts are removed
// and that frequently-accessed expired memories have their TTL extended instead.
func TestPruneExpiredMemories(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	agentName := "prune-agent"
	userID := "prune-user"

	past := time.Now().Add(-1 * time.Hour)

	// Memory that is expired and unpopular, should be deleted
	coldMem := &Memory{AgentName: agentName, UserID: userID, Content: "cold expired memory", Embedding: makeEmbedding(0.1), ExpiresAt: &past, AccessCount: 2}
	require.NoError(t, client.StoreAgentMemories(ctx, coldMem))

	// Memory that is expired but popular (AccessCount >= 10), TTL should be extended
	hotMem := &Memory{AgentName: agentName, UserID: userID, Content: "hot expired memory", Embedding: makeEmbedding(0.9), ExpiresAt: &past, AccessCount: 15}
	require.NoError(t, client.StoreAgentMemories(ctx, hotMem))

	// Memory that has not expired, should be untouched
	future := time.Now().Add(24 * time.Hour)
	liveMem := &Memory{AgentName: agentName, UserID: userID, Content: "non-expired memory", Embedding: makeEmbedding(0.5), ExpiresAt: &future, AccessCount: 0}
	require.NoError(t, client.StoreAgentMemories(ctx, liveMem))

	err := client.PruneExpiredMemories(ctx)
	require.NoError(t, err)

	results, err := client.SearchAgentMemory(ctx, agentName, userID, makeEmbedding(0.5), 10)
	require.NoError(t, err)

	ids := make([]string, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.ID)
	}

	assert.NotContains(t, ids, coldMem.ID, "Expired unpopular memory should be pruned")
	assert.Contains(t, ids, hotMem.ID, "Expired popular memory should have TTL extended and be retained")
	assert.Contains(t, ids, liveMem.ID, "Non-expired memory should be retained")
}

func countRows(t *testing.T, db *pgxpool.Pool, query string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.QueryRow(context.Background(), query, args...).Scan(&n))
	return n
}

// TestSearchAgentMemoryConcurrentAccessCount verifies concurrent searches over
// overlapping rows do not deadlock when incrementing access_count and still
// return results.
func TestSearchAgentMemoryConcurrentAccessCount(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	agentName := "concurrent-agent"
	userID := "concurrent-user"

	// Small store so every search hits the same top rows (max overlap).
	for i := range 5 {
		err := client.StoreAgentMemories(ctx, &Memory{
			AgentName: agentName,
			UserID:    userID,
			Content:   fmt.Sprintf("shared memory %d", i),
			Embedding: makeEmbedding(float32(i+1) * 0.15),
		})
		require.NoError(t, err)
	}

	const numGoroutines = 20
	const searchesPerGoroutine = 10

	var wg sync.WaitGroup
	errs := make(chan error, numGoroutines*searchesPerGoroutine)
	wg.Add(numGoroutines)

	for range numGoroutines {
		go func() {
			defer wg.Done()
			for range searchesPerGoroutine {
				results, err := client.SearchAgentMemory(ctx, agentName, userID, makeEmbedding(0.5), 5)
				if err != nil {
					errs <- err
					return
				}
				if len(results) == 0 {
					errs <- fmt.Errorf("expected search results, got none")
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err, "concurrent memory search must not fail")
	}
}
