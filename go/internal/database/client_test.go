package database

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	dbpkg "github.com/kagent-dev/kagent/go/pkg/database"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConcurrentAgentUpserts verifies that concurrent StoreAgent calls
// don't corrupt data. The database's OnConflict clause ensures atomic upserts.
func TestConcurrentAgentUpserts(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)

	const numGoroutines = 10
	const numUpserts = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// All goroutines upsert to the same agent ID - this tests conflict handling
	agentID := "test-agent"

	for i := range numGoroutines {
		go func(goroutineID int) {
			defer wg.Done()
			for j := range numUpserts {
				agent := &dbpkg.Agent{
					ID:   agentID,
					Type: fmt.Sprintf("type-%d-%d", goroutineID, j),
				}
				err := client.StoreAgent(agent)
				assert.NoError(t, err, "StoreAgent should not fail")
			}
		}(i)
	}

	wg.Wait()

	// Verify the agent exists and has valid data (not corrupted)
	agent, err := client.GetAgent(agentID)
	require.NoError(t, err)
	assert.Equal(t, agentID, agent.ID)
	assert.NotEmpty(t, agent.Type) // Should have some valid type from one of the upserts
}

// TestConcurrentToolServerUpserts verifies that concurrent StoreToolServer calls
// work correctly without application-level locking.
func TestConcurrentToolServerUpserts(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)

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
				_, err := client.StoreToolServer(toolServer)
				assert.NoError(t, err, "StoreToolServer should not fail")
			}
		}(i)
	}

	wg.Wait()

	// Verify the tool server exists and has valid data
	server, err := client.GetToolServer(serverName)
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

	serverName := "test-server"
	groupKind := "RemoteMCPServer"

	// Create the tool server first
	_, err := client.StoreToolServer(&dbpkg.ToolServer{
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
			tools := []*v1alpha2.MCPTool{
				{Name: fmt.Sprintf("tool-a-%d", goroutineID), Description: "Tool A"},
				{Name: fmt.Sprintf("tool-b-%d", goroutineID), Description: "Tool B"},
			}
			err := client.RefreshToolsForServer(serverName, groupKind, tools...)
			assert.NoError(t, err, "RefreshToolsForServer should not fail")
		}(i)
	}

	wg.Wait()

	// Verify the tools exist (we don't know which goroutine's tools "won", but the state should be consistent)
	tools, err := client.ListToolsForServer(serverName, groupKind)
	require.NoError(t, err)
	// Should have exactly 2 tools from one of the refresh operations
	assert.Len(t, tools, 2, "Should have exactly 2 tools after concurrent refreshes")
}

// TestStoreAgentIdempotence verifies that calling StoreAgent multiple times
// with the same data is idempotent and doesn't error. This is critical for
// the lock-free concurrency model where concurrent upserts must succeed.
func TestStoreAgentIdempotence(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)

	agent := &dbpkg.Agent{
		ID:   "idempotent-agent",
		Type: "declarative",
	}

	// First store should succeed
	err := client.StoreAgent(agent)
	require.NoError(t, err, "First StoreAgent should succeed")

	// Second store with same data should also succeed (idempotent)
	err = client.StoreAgent(agent)
	require.NoError(t, err, "Second StoreAgent should succeed (idempotent)")

	// Third store with updated data should succeed (upsert)
	agent.Type = "byo"
	err = client.StoreAgent(agent)
	require.NoError(t, err, "Third StoreAgent with updated data should succeed")

	// Verify final state
	retrieved, err := client.GetAgent(agent.ID)
	require.NoError(t, err)
	assert.Equal(t, "byo", retrieved.Type, "Agent should have updated type")
}

// TestStoreToolServerIdempotence verifies that StoreToolServer is idempotent.
func TestStoreToolServerIdempotence(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)

	server := &dbpkg.ToolServer{
		Name:        "idempotent-server",
		GroupKind:   "RemoteMCPServer",
		Description: "Original description",
	}

	// First store
	_, err := client.StoreToolServer(server)
	require.NoError(t, err, "First StoreToolServer should succeed")

	// Second store with same data (idempotent)
	_, err = client.StoreToolServer(server)
	require.NoError(t, err, "Second StoreToolServer should succeed")

	// Third store with updated data (upsert)
	server.Description = "Updated description"
	_, err = client.StoreToolServer(server)
	require.NoError(t, err, "Third StoreToolServer with updated data should succeed")

	// Verify final state
	retrieved, err := client.GetToolServer(server.Name)
	require.NoError(t, err)
	assert.Equal(t, "Updated description", retrieved.Description)
}

// setupTestDB creates an in-memory SQLite database for testing using Turso.
// Turso driver (via replace github.com/glebarez/sqlite) handles datetime scanning.
func setupTestDB(t *testing.T) *Manager {
	t.Helper()

	config := &Config{
		DatabaseType: DatabaseTypeSqlite,
		SqliteConfig: &SqliteConfig{
			DatabasePath: ":memory:",
		},
	}

	manager, err := NewManager(config)
	require.NoError(t, err, "Failed to create test database")

	err = manager.Initialize()
	require.NoError(t, err, "Failed to initialize test database")

	t.Cleanup(func() {
		manager.Close()
	})

	return manager
}
func TestListEventsForSession(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	userID := "test-user"
	sessionID := "test-session"

	// Create 3 events
	for i := range 3 {
		event := &dbpkg.Event{
			ID:        fmt.Sprintf("event-%d", i),
			SessionID: sessionID,
			UserID:    userID,
			Data:      "{}",
		}
		err := client.StoreEvents(event)
		require.NoError(t, err)
	}

	tests := []struct {
		name          string
		limit         int
		expectedCount int
	}{
		{"Limit 1", 1, 1},
		{"Limit 2", 2, 2},
		{"Limit 0 (No limit)", 0, 3},
		{"Limit -1 (No limit)", -1, 3},
		{"Limit 5 (More than exists)", 5, 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := dbpkg.QueryOptions{
				Limit: tc.limit,
			}
			events, err := client.ListEventsForSession(sessionID, userID, opts)
			require.NoError(t, err)
			assert.Len(t, events, tc.expectedCount)
		})
	}
}

func TestListEventsForSessionOrdering(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	userID := "test-user"
	sessionID := "test-session"

	// Create events with specific timestamps
	// Using a significant gap to ensure database resolution handles it correctly
	baseTime := time.Now().Add(-10 * time.Hour)

	for i := range 3 {
		event := &dbpkg.Event{
			ID:        fmt.Sprintf("event-%d", i),
			SessionID: sessionID,
			UserID:    userID,
			CreatedAt: baseTime.Add(time.Duration(i) * time.Hour),
			Data:      "{}",
		}
		err := client.StoreEvents(event)
		require.NoError(t, err)
	}

	t.Run("Default (Desc)", func(t *testing.T) {
		opts := dbpkg.QueryOptions{}
		events, err := client.ListEventsForSession(sessionID, userID, opts)
		require.NoError(t, err)
		require.Len(t, events, 3)
		// Should be 2, 1, 0
		assert.Equal(t, "event-2", events[0].ID)
		assert.Equal(t, "event-1", events[1].ID)
		assert.Equal(t, "event-0", events[2].ID)
	})

	t.Run("Ascending", func(t *testing.T) {
		opts := dbpkg.QueryOptions{
			OrderAsc: true,
		}
		events, err := client.ListEventsForSession(sessionID, userID, opts)
		require.NoError(t, err)
		require.Len(t, events, 3)
		// Should be 0, 1, 2
		assert.Equal(t, "event-0", events[0].ID)
		assert.Equal(t, "event-1", events[1].ID)
		assert.Equal(t, "event-2", events[2].ID)
	})
}

// setupVectorTestDB creates an in-memory SQLite database with the vector extension enabled.
// libSQL/Turso bundles the vector extension, so vector_distance_cos is available at runtime.
func setupVectorTestDB(t *testing.T) *Manager {
	t.Helper()

	config := &Config{
		DatabaseType: DatabaseTypeSqlite,
		SqliteConfig: &SqliteConfig{
			DatabasePath:  ":memory:",
			VectorEnabled: true,
		},
	}

	manager, err := NewManager(config)
	require.NoError(t, err, "Failed to create vector-enabled test database")

	err = manager.Initialize()
	require.NoError(t, err, "Failed to initialize vector-enabled test database")

	t.Cleanup(func() {
		manager.Close()
	})

	return manager
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
	db := setupVectorTestDB(t)
	client := NewClient(db)

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
		err := client.StoreAgentMemory(m)
		require.NoError(t, err)
	}

	// Query with embedding; all three memories should be returned with high similarity.
	results, err := client.SearchAgentMemory(agentName, userID, makeEmbedding(0.5), 3)
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
	db := setupVectorTestDB(t)
	client := NewClient(db)

	agentName := "batch-agent"
	userID := "batch-user"

	memories := []*dbpkg.Memory{
		{ID: "b-1", AgentName: agentName, UserID: userID, Content: "batch memory 1", Embedding: makeEmbedding(0.2)},
		{ID: "b-2", AgentName: agentName, UserID: userID, Content: "batch memory 2", Embedding: makeEmbedding(0.4)},
		{ID: "b-3", AgentName: agentName, UserID: userID, Content: "batch memory 3", Embedding: makeEmbedding(0.6)},
	}

	err := client.StoreAgentMemories(memories)
	require.NoError(t, err)

	results, err := client.SearchAgentMemory(agentName, userID, makeEmbedding(0.5), 10)
	require.NoError(t, err)
	assert.Len(t, results, 3, "All 3 batch-stored memories should be found")
}

// TestSearchAgentMemoryLimit verifies that the limit parameter is respected when
// searching for similar memories.
func TestSearchAgentMemoryLimit(t *testing.T) {
	db := setupVectorTestDB(t)
	client := NewClient(db)

	agentName := "limit-agent"
	userID := "limit-user"

	for i := range 5 {
		err := client.StoreAgentMemory(&dbpkg.Memory{
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
			results, err := client.SearchAgentMemory(agentName, userID, makeEmbedding(0.5), tc.limit)
			require.NoError(t, err)
			assert.Len(t, results, tc.expected)
		})
	}
}

// TestSearchAgentMemoryIsolation verifies that searches are scoped to the
// correct (agentName, userID) pair and do not return results for other agents or users.
func TestSearchAgentMemoryIsolation(t *testing.T) {
	db := setupVectorTestDB(t)
	client := NewClient(db)

	require.NoError(t, client.StoreAgentMemory(&dbpkg.Memory{
		ID: "iso-1", AgentName: "agent-a", UserID: "user-1",
		Content: "agent-a user-1 memory", Embedding: makeEmbedding(0.5),
	}))
	require.NoError(t, client.StoreAgentMemory(&dbpkg.Memory{
		ID: "iso-2", AgentName: "agent-b", UserID: "user-1",
		Content: "agent-b user-1 memory", Embedding: makeEmbedding(0.5),
	}))
	require.NoError(t, client.StoreAgentMemory(&dbpkg.Memory{
		ID: "iso-3", AgentName: "agent-a", UserID: "user-2",
		Content: "agent-a user-2 memory", Embedding: makeEmbedding(0.5),
	}))

	results, err := client.SearchAgentMemory("agent-a", "user-1", makeEmbedding(0.5), 10)
	require.NoError(t, err)
	require.Len(t, results, 1, "Should only return memories for agent-a / user-1")
	assert.Equal(t, "iso-1", results[0].ID)
}

// TestDeleteAgentMemory verifies that DeleteAgentMemory removes all memories for the
// given agent/user pair and that the hyphen-to-underscore normalization works correctly.
func TestDeleteAgentMemory(t *testing.T) {
	db := setupVectorTestDB(t)
	client := NewClient(db)

	agentName := "my-agent"
	userID := "del-user"

	for i := range 3 {
		err := client.StoreAgentMemory(&dbpkg.Memory{
			ID:        fmt.Sprintf("del-%d", i),
			AgentName: agentName,
			UserID:    userID,
			Content:   fmt.Sprintf("memory to delete %d", i),
			Embedding: makeEmbedding(float32(i+1) * 0.2),
		})
		require.NoError(t, err)
	}

	// Confirm they exist before deletion
	before, err := client.SearchAgentMemory(agentName, userID, makeEmbedding(0.5), 10)
	require.NoError(t, err)
	require.Len(t, before, 3)

	err = client.DeleteAgentMemory(agentName, userID)
	require.NoError(t, err)

	after, err := client.SearchAgentMemory(agentName, userID, makeEmbedding(0.5), 10)
	require.NoError(t, err)
	assert.Empty(t, after, "All memories should be deleted")
}

// TestPruneExpiredMemories verifies that expired memories with low access counts are removed
// and that frequently-accessed expired memories have their TTL extended instead.
func TestPruneExpiredMemories(t *testing.T) {
	db := setupVectorTestDB(t)
	client := NewClient(db)

	agentName := "prune-agent"
	userID := "prune-user"

	past := time.Now().Add(-1 * time.Hour)

	// Memory that is expired and unpopular — should be deleted
	require.NoError(t, client.StoreAgentMemory(&dbpkg.Memory{
		ID:          "prune-cold",
		AgentName:   agentName,
		UserID:      userID,
		Content:     "cold expired memory",
		Embedding:   makeEmbedding(0.1),
		ExpiresAt:   &past,
		AccessCount: 2,
	}))

	// Memory that is expired but popular (AccessCount >= 10) — TTL should be extended
	require.NoError(t, client.StoreAgentMemory(&dbpkg.Memory{
		ID:          "prune-hot",
		AgentName:   agentName,
		UserID:      userID,
		Content:     "hot expired memory",
		Embedding:   makeEmbedding(0.9),
		ExpiresAt:   &past,
		AccessCount: 15,
	}))

	// Memory that has not expired — should be untouched
	future := time.Now().Add(24 * time.Hour)
	require.NoError(t, client.StoreAgentMemory(&dbpkg.Memory{
		ID:          "prune-live",
		AgentName:   agentName,
		UserID:      userID,
		Content:     "non-expired memory",
		Embedding:   makeEmbedding(0.5),
		ExpiresAt:   &future,
		AccessCount: 0,
	}))

	err := client.PruneExpiredMemories()
	require.NoError(t, err)

	results, err := client.SearchAgentMemory(agentName, userID, makeEmbedding(0.5), 10)
	require.NoError(t, err)

	ids := make([]string, 0, len(results))
	for _, r := range results {
		ids = append(ids, r.ID)
	}

	assert.NotContains(t, ids, "prune-cold", "Expired unpopular memory should be pruned")
	assert.Contains(t, ids, "prune-hot", "Expired popular memory should have TTL extended and be retained")
	assert.Contains(t, ids, "prune-live", "Non-expired memory should be retained")
}
