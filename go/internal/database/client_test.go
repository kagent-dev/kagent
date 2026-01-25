package database

import (
	"fmt"
	"sync"
	"testing"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
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
				agent := &Agent{
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
				toolServer := &ToolServer{
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
	_, err := client.StoreToolServer(&ToolServer{
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

	agent := &Agent{
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

	server := &ToolServer{
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

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) *Manager {
	t.Helper()

	config := &Config{
		DatabaseType: DatabaseTypeSqlite,
		SqliteConfig: &SqliteConfig{
			DatabasePath: "file::memory:?cache=shared",
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
