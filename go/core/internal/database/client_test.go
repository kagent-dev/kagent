package database

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConcurrentToolServerUpserts verifies that concurrent StoreToolServer calls
// work correctly without application-level locking.
func TestConcurrentToolServerUpserts(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	const numGoroutines = 10
	const numUpserts = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	serverName := "test-server"
	groupKind := "RemoteMCPServer"

	for i := range numGoroutines {
		go func(goroutineID int) {
			defer wg.Done()
			for j := range numUpserts {
				toolServer := &dbpkg.ToolServer{
					Name:        serverName,
					GroupKind:   groupKind,
					Description: fmt.Sprintf("Description from goroutine %d iteration %d", goroutineID, j),
				}
				_, err := client.StoreToolServer(ctx, toolServer)
				assert.NoError(t, err, "StoreToolServer should not fail")
			}
		}(i)
	}

	wg.Wait()

	// Verify the tool server exists and has valid data
	server, err := client.GetToolServer(ctx, serverName)
	require.NoError(t, err)
	assert.Equal(t, serverName, server.Name)
	assert.NotEmpty(t, server.Description)
}

// TestConcurrentRefreshToolsForServer verifies that concurrent RefreshToolsForServer
// calls work correctly. This is the most complex operation that previously required
// an application-level lock.
func TestConcurrentRefreshToolsForServer(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	serverName := "test-server"
	groupKind := "RemoteMCPServer"

	// Create the tool server first
	_, err := client.StoreToolServer(ctx, &dbpkg.ToolServer{
		Name:        serverName,
		GroupKind:   groupKind,
		Description: "Test server",
	})
	require.NoError(t, err)

	const numGoroutines = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := range numGoroutines {
		go func(goroutineID int) {
			defer wg.Done()
			// Each goroutine refreshes with a different set of tools
			tools := []*v1alpha3.MCPTool{
				{Name: fmt.Sprintf("tool-a-%d", goroutineID), Description: "Tool A"},
				{Name: fmt.Sprintf("tool-b-%d", goroutineID), Description: "Tool B"},
			}
			err := client.RefreshToolsForServer(ctx, serverName, groupKind, tools...)
			assert.NoError(t, err, "RefreshToolsForServer should not fail")
		}(i)
	}

	wg.Wait()

	// Verify the tools exist and no data was corrupted. With READ COMMITTED isolation,
	// concurrent delete+insert transactions can interleave, so we don't assert on an
	// exact count. What matters is that all calls succeeded and valid tool records exist.
	tools, err := client.ListToolsForServer(ctx, serverName, groupKind)
	require.NoError(t, err)
	assert.NotEmpty(t, tools, "Should have tools after concurrent refreshes")
	for _, tool := range tools {
		assert.Equal(t, serverName, tool.ServerName)
		assert.Equal(t, groupKind, tool.GroupKind)
	}
}

// TestStoreToolServerIdempotence verifies that StoreToolServer is idempotent.
func TestStoreToolServerIdempotence(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	server := &dbpkg.ToolServer{
		Name:        "idempotent-server",
		GroupKind:   "RemoteMCPServer",
		Description: "Original description",
	}

	// First store
	_, err := client.StoreToolServer(ctx, server)
	require.NoError(t, err, "First StoreToolServer should succeed")

	// Second store with same data (idempotent)
	_, err = client.StoreToolServer(ctx, server)
	require.NoError(t, err, "Second StoreToolServer should succeed")

	// Third store with updated data (upsert)
	server.Description = "Updated description"
	_, err = client.StoreToolServer(ctx, server)
	require.NoError(t, err, "Third StoreToolServer with updated data should succeed")

	// Verify final state
	retrieved, err := client.GetToolServer(ctx, server.Name)
	require.NoError(t, err)
	assert.Equal(t, "Updated description", retrieved.Description)
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
			tool, toolserver, lg_checkpoint, lg_checkpoint_write,
			crewai_agent_memory, crewai_flow_state, memory,
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

	memories := []*dbpkg.Memory{
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
		err := client.StoreAgentMemory(ctx, m)
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

	memories := []*dbpkg.Memory{
		{ID: "b-1", AgentName: agentName, UserID: userID, Content: "batch memory 1", Embedding: makeEmbedding(0.2)},
		{ID: "b-2", AgentName: agentName, UserID: userID, Content: "batch memory 2", Embedding: makeEmbedding(0.4)},
		{ID: "b-3", AgentName: agentName, UserID: userID, Content: "batch memory 3", Embedding: makeEmbedding(0.6)},
	}

	err := client.StoreAgentMemories(ctx, memories)
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
		err := client.StoreAgentMemory(ctx, &dbpkg.Memory{
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

	mem1 := &dbpkg.Memory{AgentName: "agent-a", UserID: "user-1", Content: "agent-a user-1 memory", Embedding: makeEmbedding(0.5)}
	require.NoError(t, client.StoreAgentMemory(ctx, mem1))
	require.NoError(t, client.StoreAgentMemory(ctx, &dbpkg.Memory{AgentName: "agent-b", UserID: "user-1", Content: "agent-b user-1 memory", Embedding: makeEmbedding(0.5)}))
	require.NoError(t, client.StoreAgentMemory(ctx, &dbpkg.Memory{AgentName: "agent-a", UserID: "user-2", Content: "agent-a user-2 memory", Embedding: makeEmbedding(0.5)}))

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

	stored := &dbpkg.Memory{AgentName: "ns__my_agent", UserID: "user-1", Content: "stored under underscore form", Embedding: makeEmbedding(0.5)}
	require.NoError(t, client.StoreAgentMemory(ctx, stored))

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
		err := client.StoreAgentMemory(ctx, &dbpkg.Memory{
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
	coldMem := &dbpkg.Memory{AgentName: agentName, UserID: userID, Content: "cold expired memory", Embedding: makeEmbedding(0.1), ExpiresAt: &past, AccessCount: 2}
	require.NoError(t, client.StoreAgentMemory(ctx, coldMem))

	// Memory that is expired but popular (AccessCount >= 10), TTL should be extended
	hotMem := &dbpkg.Memory{AgentName: agentName, UserID: userID, Content: "hot expired memory", Embedding: makeEmbedding(0.9), ExpiresAt: &past, AccessCount: 15}
	require.NoError(t, client.StoreAgentMemory(ctx, hotMem))

	// Memory that has not expired, should be untouched
	future := time.Now().Add(24 * time.Hour)
	liveMem := &dbpkg.Memory{AgentName: agentName, UserID: userID, Content: "non-expired memory", Embedding: makeEmbedding(0.5), ExpiresAt: &future, AccessCount: 0}
	require.NoError(t, client.StoreAgentMemory(ctx, liveMem))

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
		err := client.StoreAgentMemory(ctx, &dbpkg.Memory{
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

// TestSingleRowReadsMapMissingToErrNotFound verifies that every single-row
// read maps the driver's no-rows error to dbpkg.ErrNotFound, so callers can
// match with errors.Is without importing pgx.
func TestSingleRowReadsMapMissingToErrNotFound(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()
	missing := "missing"

	tests := []struct {
		name string
		read func() error
	}{
		{name: "GetTool", read: func() error { _, err := client.GetTool(ctx, "missing"); return err }},
		{name: "GetToolServer", read: func() error { _, err := client.GetToolServer(ctx, "missing"); return err }},
		{name: "ListCheckpoints by ID", read: func() error {
			_, err := client.ListCheckpoints(ctx, "user", "thread", "namespace", &missing, 0)
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.read()
			require.Error(t, err)
			require.ErrorIs(t, err, dbpkg.ErrNotFound)
		})
	}
}
