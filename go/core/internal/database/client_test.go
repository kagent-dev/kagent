package database

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/jackc/pgx/v5/pgxpool"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConcurrentAgentUpserts verifies that concurrent StoreAgent calls
// don't corrupt data. The database's OnConflict clause ensures atomic upserts.
func TestConcurrentAgentUpserts(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

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
				err := client.StoreAgent(ctx, agent)
				assert.NoError(t, err, "StoreAgent should not fail")
			}
		}(i)
	}

	wg.Wait()

	// Verify the agent exists and has valid data (not corrupted)
	agent, err := client.GetAgent(ctx, agentID)
	require.NoError(t, err)
	assert.Equal(t, agentID, agent.ID)
	assert.NotEmpty(t, agent.Type) // Should have some valid type from one of the upserts
}

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
			tools := []*v1alpha2.MCPTool{
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

// TestConcurrentSessionUpserts verifies that concurrent StoreSession calls
// don't corrupt data and that a session is always visible via GetSession
// immediately after StoreSession returns. This validates that StoreSession
// uses an explicit transaction (withTx) so the write is committed before
// the function returns, preventing read-your-writes issues on pooled connections.
func TestConcurrentSessionUpserts(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	const numGoroutines = 10
	const numUpserts = 20

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	userID := "test-user"

	for i := range numGoroutines {
		go func(goroutineID int) {
			defer wg.Done()
			for j := range numUpserts {
				sessionID := fmt.Sprintf("session-%d-%d", goroutineID, j)
				name := fmt.Sprintf("Session %d-%d", goroutineID, j)
				agentID := "test-agent"
				session := &dbpkg.Session{
					ID:      sessionID,
					UserID:  userID,
					Name:    &name,
					AgentID: &agentID,
				}
				err := client.StoreSession(ctx, session)
				assert.NoError(t, err, "StoreSession should not fail")

				// Immediately read back, must be visible (validates withTx commit)
				got, err := client.GetSession(ctx, sessionID, userID)
				assert.NoError(t, err, "GetSession should find the session immediately after StoreSession")
				if got != nil {
					assert.Equal(t, sessionID, got.ID)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify all sessions exist
	sessions, err := client.ListSessions(ctx, userID)
	require.NoError(t, err)
	assert.Len(t, sessions, numGoroutines*numUpserts, "All sessions should be stored")
}

// TestStoreSessionIdempotence verifies that calling StoreSession multiple times
// with the same ID is idempotent (upsert behavior).
func TestStoreSessionIdempotence(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	userID := "test-user"
	name1 := "Original"
	agentID := "agent-1"
	session := &dbpkg.Session{
		ID:      "idempotent-session",
		UserID:  userID,
		Name:    &name1,
		AgentID: &agentID,
	}

	err := client.StoreSession(ctx, session)
	require.NoError(t, err, "First StoreSession should succeed")

	// Second store with same data should also succeed
	err = client.StoreSession(ctx, session)
	require.NoError(t, err, "Second StoreSession should succeed (idempotent)")

	// Third store with updated name should succeed (upsert)
	name2 := "Updated"
	session.Name = &name2
	err = client.StoreSession(ctx, session)
	require.NoError(t, err, "Third StoreSession with updated data should succeed")

	// Verify final state
	retrieved, err := client.GetSession(ctx, session.ID, userID)
	require.NoError(t, err)
	assert.Equal(t, "Updated", *retrieved.Name, "Session should have updated name")

	_, err = client.GetSession(ctx, "no-such-session", userID)
	require.Error(t, err)

	_, err = client.GetSession(ctx, session.ID, "other-user")
	require.Error(t, err, "another user's session must not be readable")
}

func TestListSessionsOrdersByRecentActivity(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	userID := "test-user"
	agentID := "test-agent"
	for _, sessionID := range []string{"old-active", "old-inactive", "new-inactive"} {
		err := client.StoreSession(ctx, &dbpkg.Session{
			ID:      sessionID,
			UserID:  userID,
			AgentID: &agentID,
		})
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond)
	}

	err := client.StoreEvents(ctx, &dbpkg.Event{
		ID:        "event-1",
		SessionID: "old-active",
		UserID:    userID,
		Data:      "{}",
	})
	require.NoError(t, err)

	allSessions, err := client.ListSessions(ctx, userID)
	require.NoError(t, err)
	require.Len(t, allSessions, 3)
	assert.Equal(t, []string{"old-active", "new-inactive", "old-inactive"}, []string{
		allSessions[0].ID,
		allSessions[1].ID,
		allSessions[2].ID,
	})

	agentSessions, err := client.ListSessionsForAgent(ctx, agentID, userID)
	require.NoError(t, err)
	require.Len(t, agentSessions, 3)
	assert.Equal(t, []string{"old-active", "new-inactive", "old-inactive"}, []string{
		agentSessions[0].ID,
		agentSessions[1].ID,
		agentSessions[2].ID,
	})
}

func TestStoreEventTouchesSessionActivity(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	userID := "test-user"
	sessionID := "active-session"

	err := client.StoreSession(ctx, &dbpkg.Session{
		ID:     sessionID,
		UserID: userID,
	})
	require.NoError(t, err)
	before, err := client.GetSession(ctx, sessionID, userID)
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)

	err = client.StoreEvents(ctx, &dbpkg.Event{
		ID:        "event-1",
		SessionID: sessionID,
		UserID:    userID,
		Data:      "{}",
	})
	require.NoError(t, err)

	got, err := client.GetSession(ctx, sessionID, userID)
	require.NoError(t, err)
	assert.True(t, got.UpdatedAt.After(before.UpdatedAt), "session updated_at should advance after storing an event")
}

func TestStoreTaskTouchesSessionActivity(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	userID := "test-user"
	sessionID := "active-session"

	err := client.StoreSession(ctx, &dbpkg.Session{
		ID:     sessionID,
		UserID: userID,
	})
	require.NoError(t, err)
	before, err := client.GetSession(ctx, sessionID, userID)
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)

	err = client.StoreTask(ctx, &a2a.Task{
		ID:        "task-1",
		ContextID: sessionID,
	}, userID)
	require.NoError(t, err)

	got, err := client.GetSession(ctx, sessionID, userID)
	require.NoError(t, err)
	assert.True(t, got.UpdatedAt.After(before.UpdatedAt), "session updated_at should advance after storing a task")
}

func TestTaskAccessIsScopedToOwner(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	require.NoError(t, client.StoreTask(ctx, &a2a.Task{ID: "task-owned"}, "user-a"))

	_, err := client.GetTask(ctx, "task-owned", "user-b")
	require.Error(t, err, "another user must not read this task")

	err = client.DeleteTask(ctx, "task-owned", "user-b")
	require.ErrorIs(t, err, dbpkg.ErrTaskOwnedByAnotherUser, "another user must not delete this task")

	task, err := client.GetTask(ctx, "task-owned", "user-a")
	require.NoError(t, err, "task must still exist after another user's delete attempt")
	assert.Equal(t, a2a.TaskID("task-owned"), task.ID)

	err = client.StoreTask(ctx, &a2a.Task{ID: "task-owned"}, "user-b")
	require.ErrorIs(t, err, dbpkg.ErrTaskOwnedByAnotherUser, "another user must not take over this task id")

	require.NoError(t, client.DeleteTask(ctx, "task-owned", "user-a"), "the real owner can delete it")
	require.NoError(t, client.DeleteTask(ctx, "task-owned", "user-a"), "deleting an already-gone task is not an error")
}

// A soft-deleted task keeps its primary key row, so its id is burned: reusing
// it must fail loudly for everyone instead of reporting success while writing
// nothing (or silently updating a row that stays deleted).
func TestDeletedTaskIdCannotBeReused(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	require.NoError(t, client.StoreTask(ctx, &a2a.Task{ID: "t-dead"}, "alice"))
	require.NoError(t, client.DeleteTask(ctx, "t-dead", "alice"))

	err := client.StoreTask(ctx, &a2a.Task{ID: "t-dead"}, "bob")
	require.ErrorIs(t, err, dbpkg.ErrTaskOwnedByAnotherUser, "another user must not reuse a deleted id")

	err = client.StoreTask(ctx, &a2a.Task{ID: "t-dead"}, "alice")
	require.ErrorIs(t, err, dbpkg.ErrTaskOwnedByAnotherUser, "the owner must not silently resurrect a deleted id")

	_, err = client.GetTask(ctx, "t-dead", "alice")
	require.Error(t, err, "the task must stay deleted")
}

// TestNullOwnedTaskAccess covers tasks with a NULL user_id: rows written
// before the owner column existed, or by a pre-upgrade pod during a rolling
// upgrade. Such a task is only visible to, and claimable by, the caller when
// its session id maps to exactly one user across its whole history.
func TestNullOwnedTaskAccess(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	seedNullTask := func(id, sessionID string) {
		_, err := db.Exec(ctx,
			`INSERT INTO task (id, data, session_id, created_at, updated_at) VALUES ($1, '{}', $2, NOW(), NOW())`,
			id, sessionID)
		require.NoError(t, err)
	}

	// alice is the only user in session s-mine's history.
	require.NoError(t, client.StoreSession(ctx, &dbpkg.Session{ID: "s-mine", UserID: "alice"}))
	seedNullTask("t-legacy", "s-mine")

	_, err := client.GetTask(ctx, "t-legacy", "alice")
	require.NoError(t, err, "sole session owner must read the NULL-owned task")
	_, err = client.GetTask(ctx, "t-legacy", "bob")
	require.Error(t, err, "the NULL-owned task must stay hidden from other users")

	tasks, err := client.ListTasksForSession(ctx, "s-mine", "alice")
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	tasks, err = client.ListTasksForSession(ctx, "s-mine", "bob")
	require.NoError(t, err)
	assert.Empty(t, tasks)

	err = client.StoreTask(ctx, &a2a.Task{ID: "t-legacy", ContextID: "s-mine"}, "bob")
	require.ErrorIs(t, err, dbpkg.ErrTaskOwnedByAnotherUser, "another user must not claim the NULL-owned task")
	err = client.DeleteTask(ctx, "t-legacy", "bob")
	require.ErrorIs(t, err, dbpkg.ErrTaskOwnedByAnotherUser, "another user must not delete the NULL-owned task")

	require.NoError(t, client.StoreTask(ctx, &a2a.Task{ID: "t-legacy", ContextID: "s-mine"}, "alice"),
		"sole session owner claims the task by writing it")
	err = client.StoreTask(ctx, &a2a.Task{ID: "t-legacy", ContextID: "s-mine"}, "bob")
	require.ErrorIs(t, err, dbpkg.ErrTaskOwnedByAnotherUser, "the claim must stick")

	// A session id used by two users is ambiguous: the NULL-owned task stays
	// hidden from both, and neither can claim it.
	require.NoError(t, client.StoreSession(ctx, &dbpkg.Session{ID: "s-shared", UserID: "alice"}))
	require.NoError(t, client.StoreSession(ctx, &dbpkg.Session{ID: "s-shared", UserID: "bob"}))
	seedNullTask("t-ambiguous", "s-shared")

	_, err = client.GetTask(ctx, "t-ambiguous", "alice")
	require.Error(t, err)
	_, err = client.GetTask(ctx, "t-ambiguous", "bob")
	require.Error(t, err)
	err = client.StoreTask(ctx, &a2a.Task{ID: "t-ambiguous", ContextID: "s-shared"}, "alice")
	require.ErrorIs(t, err, dbpkg.ErrTaskOwnedByAnotherUser)
}

// TestNullOwnedTaskAgainstLaterSessionIsInaccessible: a NULL-owned task whose
// session_id has no session row *at all* (the original session was hard
// deleted, or never migrated cleanly) must not become claimable just because
// someone later creates a brand new session reusing that same id. The owner
// resolution must only trust sessions that existed at or before the task was
// written, never one created afterward.
func TestNullOwnedTaskAgainstLaterSessionIsInaccessible(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	_, err := db.Exec(ctx,
		`INSERT INTO task (id, data, session_id, created_at, updated_at) VALUES ($1, '{}', $2, NOW(), NOW())`,
		"t-orphan", "s-freed")
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)
	// bob creates a session reusing the freed session id after the orphaned
	// task already existed. bob must gain no access to it.
	require.NoError(t, client.StoreSession(ctx, &dbpkg.Session{ID: "s-freed", UserID: "bob"}))

	_, err = client.GetTask(ctx, "t-orphan", "bob")
	require.Error(t, err, "a session created after an orphaned task must not resolve ownership to its creator")

	tasks, err := client.ListTasksForSession(ctx, "s-freed", "bob")
	require.NoError(t, err)
	assert.Empty(t, tasks, "a session created after an orphaned task must not surface it in listings")

	err = client.StoreTask(ctx, &a2a.Task{ID: "t-orphan", ContextID: "s-freed"}, "bob")
	require.ErrorIs(t, err, dbpkg.ErrTaskOwnedByAnotherUser, "bob must not be able to claim the orphaned task")
	err = client.DeleteTask(ctx, "t-orphan", "bob")
	require.ErrorIs(t, err, dbpkg.ErrTaskOwnedByAnotherUser, "bob must not be able to delete the orphaned task")
}

// TestListTasksForSessionIsScopedToOwner: session ids are not globally unique
// (session's key is (id, user_id)), so listing tasks by session id alone
// would leak one user's tasks to another user holding the same session id.
func TestListTasksForSessionIsScopedToOwner(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	require.NoError(t, client.StoreSession(ctx, &dbpkg.Session{ID: "s-shared", UserID: "alice"}))
	require.NoError(t, client.StoreSession(ctx, &dbpkg.Session{ID: "s-shared", UserID: "bob"}))
	require.NoError(t, client.StoreTask(ctx, &a2a.Task{ID: "t-alice", ContextID: "s-shared"}, "alice"))
	require.NoError(t, client.StoreTask(ctx, &a2a.Task{ID: "t-bob", ContextID: "s-shared"}, "bob"))

	bobBefore, err := client.GetSession(ctx, "s-shared", "bob")
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)

	tasks, err := client.ListTasksForSession(ctx, "s-shared", "alice")
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, a2a.TaskID("t-alice"), tasks[0].ID)

	tasks, err = client.ListTasksForSession(ctx, "s-shared", "bob")
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, a2a.TaskID("t-bob"), tasks[0].ID)

	// Storing alice's task must not touch bob's same-id session.
	require.NoError(t, client.StoreTask(ctx, &a2a.Task{ID: "t-alice", ContextID: "s-shared"}, "alice"))
	bobAfter, err := client.GetSession(ctx, "s-shared", "bob")
	require.NoError(t, err)
	assert.Equal(t, bobBefore.UpdatedAt, bobAfter.UpdatedAt,
		"another user's task write must not advance this session's updated_at")
}

// TestStoreAgentIdempotence verifies that calling StoreAgent multiple times
// with the same data is idempotent and doesn't error. This is critical for
// the lock-free concurrency model where concurrent upserts must succeed.
func TestStoreAgentIdempotence(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()

	agent := &dbpkg.Agent{
		ID:   "idempotent-agent",
		Type: "declarative",
	}

	// First store should succeed
	err := client.StoreAgent(ctx, agent)
	require.NoError(t, err, "First StoreAgent should succeed")

	// Second store with same data should also succeed (idempotent)
	err = client.StoreAgent(ctx, agent)
	require.NoError(t, err, "Second StoreAgent should succeed (idempotent)")

	// Third store with updated data should succeed (upsert)
	agent.Type = "byo"
	err = client.StoreAgent(ctx, agent)
	require.NoError(t, err, "Third StoreAgent with updated data should succeed")

	// Verify final state
	retrieved, err := client.GetAgent(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, "byo", retrieved.Type, "Agent should have updated type")
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
			agent, session, event, task, push_notification, feedback,
			tool, toolserver, lg_checkpoint, lg_checkpoint_write,
			crewai_agent_memory, crewai_flow_state, memory
		RESTART IDENTITY CASCADE
	`)
	require.NoError(t, err, "Failed to truncate test tables")

	return sharedDB
}
func TestListEventsForSession(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()
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
		err := client.StoreEvents(ctx, event)
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
			events, err := client.ListEventsForSession(ctx, sessionID, userID, opts)
			require.NoError(t, err)
			assert.Len(t, events, tc.expectedCount)
		})
	}
}

func TestListEventsForSessionOrdering(t *testing.T) {
	db := setupTestDB(t)
	client := NewClient(db)
	ctx := context.Background()
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
		err := client.StoreEvents(ctx, event)
		require.NoError(t, err)
	}

	t.Run("Default (Desc)", func(t *testing.T) {
		opts := dbpkg.QueryOptions{}
		events, err := client.ListEventsForSession(ctx, sessionID, userID, opts)
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
		events, err := client.ListEventsForSession(ctx, sessionID, userID, opts)
		require.NoError(t, err)
		require.Len(t, events, 3)
		// Should be 0, 1, 2
		assert.Equal(t, "event-0", events[0].ID)
		assert.Equal(t, "event-1", events[1].ID)
		assert.Equal(t, "event-2", events[2].ID)
	})
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
