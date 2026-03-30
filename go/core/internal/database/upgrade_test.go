package database

// TestUpgradeFromGORM validates that the golang-migrate migrations run cleanly
// against a database that was previously managed by GORM AutoMigrate, and that
// pre-existing data is accessible via the new sqlc client afterwards.
//
// It simulates an existing deployment by:
//  1. Creating the schema that GORM AutoMigrate would have produced (no migration
//     tracking tables, no gen_random_uuid() default on memory.id).
//  2. Seeding representative rows, including soft-deleted CrewAI rows that GORM's
//     Delete() hook would have left behind.
//  3. Running the new golang-migrate migrations.
//  4. Verifying that all pre-existing data is readable and that new writes work.

import (
	"context"
	"database/sql"
	"testing"
	"time"

	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gormSchema reproduces the DDL that GORM AutoMigrate emitted for the kagent
// models. Key differences from the current migrations:
//   - No schema_migrations / vector_schema_migrations tracking tables.
//   - memory.id has no DEFAULT (GORM relied on the BeforeCreate hook).
//   - Indexes may have different names (GORM derives them from the struct name).
const gormSchema = `
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS agent (
    id         TEXT        PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    type       TEXT        NOT NULL,
    config     JSON
);
CREATE INDEX IF NOT EXISTS idx_agent_deleted_at ON agent(deleted_at);

CREATE TABLE IF NOT EXISTS session (
    id         TEXT        NOT NULL,
    user_id    TEXT        NOT NULL,
    name       TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    agent_id   TEXT,
    source     TEXT,
    PRIMARY KEY (id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_session_name       ON session(name);
CREATE INDEX IF NOT EXISTS idx_session_agent_id   ON session(agent_id);
CREATE INDEX IF NOT EXISTS idx_session_deleted_at ON session(deleted_at);
CREATE INDEX IF NOT EXISTS idx_session_source     ON session(source);

CREATE TABLE IF NOT EXISTS event (
    id         TEXT        NOT NULL,
    user_id    TEXT        NOT NULL,
    session_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    data       TEXT        NOT NULL,
    PRIMARY KEY (id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_event_session_id ON event(session_id);
CREATE INDEX IF NOT EXISTS idx_event_deleted_at ON event(deleted_at);

CREATE TABLE IF NOT EXISTS task (
    id         TEXT        PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    data       TEXT        NOT NULL,
    session_id TEXT
);
CREATE INDEX IF NOT EXISTS idx_task_session_id ON task(session_id);
CREATE INDEX IF NOT EXISTS idx_task_deleted_at ON task(deleted_at);

CREATE TABLE IF NOT EXISTS push_notification (
    id         TEXT        PRIMARY KEY,
    task_id    TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    data       TEXT        NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_push_notification_task_id    ON push_notification(task_id);
CREATE INDEX IF NOT EXISTS idx_push_notification_deleted_at ON push_notification(deleted_at);

CREATE TABLE IF NOT EXISTS feedback (
    id            BIGSERIAL   PRIMARY KEY,
    created_at    TIMESTAMPTZ,
    updated_at    TIMESTAMPTZ,
    deleted_at    TIMESTAMPTZ,
    user_id       TEXT        NOT NULL,
    message_id    BIGINT,
    is_positive   BOOLEAN     DEFAULT false,
    feedback_text TEXT        NOT NULL,
    issue_type    TEXT
);
CREATE INDEX IF NOT EXISTS idx_feedback_deleted_at ON feedback(deleted_at);
CREATE INDEX IF NOT EXISTS idx_feedback_user_id    ON feedback(user_id);

CREATE TABLE IF NOT EXISTS tool (
    id          TEXT        NOT NULL,
    server_name TEXT        NOT NULL,
    group_kind  TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ,
    description TEXT,
    PRIMARY KEY (id, server_name, group_kind)
);
CREATE INDEX IF NOT EXISTS idx_tool_deleted_at ON tool(deleted_at);

CREATE TABLE IF NOT EXISTS toolserver (
    name           TEXT        NOT NULL,
    group_kind     TEXT        NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at     TIMESTAMPTZ,
    description    TEXT,
    last_connected TIMESTAMPTZ,
    PRIMARY KEY (name, group_kind)
);
CREATE INDEX IF NOT EXISTS idx_toolserver_deleted_at ON toolserver(deleted_at);

CREATE TABLE IF NOT EXISTS lg_checkpoint (
    user_id              TEXT        NOT NULL,
    thread_id            TEXT        NOT NULL,
    checkpoint_ns        TEXT        NOT NULL DEFAULT '',
    checkpoint_id        TEXT        NOT NULL,
    parent_checkpoint_id TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at           TIMESTAMPTZ,
    metadata             TEXT        NOT NULL,
    checkpoint           TEXT        NOT NULL,
    checkpoint_type      TEXT        NOT NULL,
    version              INTEGER     DEFAULT 1,
    PRIMARY KEY (user_id, thread_id, checkpoint_ns, checkpoint_id)
);
CREATE INDEX IF NOT EXISTS idx_lg_checkpoint_parent_checkpoint_id ON lg_checkpoint(parent_checkpoint_id);
CREATE INDEX IF NOT EXISTS idx_lgcp_list                          ON lg_checkpoint(created_at);
CREATE INDEX IF NOT EXISTS idx_lg_checkpoint_deleted_at           ON lg_checkpoint(deleted_at);

CREATE TABLE IF NOT EXISTS lg_checkpoint_write (
    user_id       TEXT    NOT NULL,
    thread_id     TEXT    NOT NULL,
    checkpoint_ns TEXT    NOT NULL DEFAULT '',
    checkpoint_id TEXT    NOT NULL,
    write_idx     INTEGER NOT NULL,
    value         TEXT    NOT NULL,
    value_type    TEXT    NOT NULL,
    channel       TEXT    NOT NULL,
    task_id       TEXT    NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at    TIMESTAMPTZ,
    PRIMARY KEY (user_id, thread_id, checkpoint_ns, checkpoint_id, write_idx)
);
CREATE INDEX IF NOT EXISTS idx_lg_checkpoint_write_deleted_at ON lg_checkpoint_write(deleted_at);

CREATE TABLE IF NOT EXISTS crewai_agent_memory (
    user_id     TEXT        NOT NULL,
    thread_id   TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ,
    memory_data TEXT        NOT NULL,
    PRIMARY KEY (user_id, thread_id)
);

CREATE TABLE IF NOT EXISTS crewai_flow_state (
    user_id     TEXT        NOT NULL,
    thread_id   TEXT        NOT NULL,
    method_name TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ,
    state_data  TEXT        NOT NULL,
    PRIMARY KEY (user_id, thread_id, method_name)
);

CREATE TABLE IF NOT EXISTS memory (
    id           TEXT        PRIMARY KEY,
    agent_name   TEXT,
    user_id      TEXT,
    content      TEXT,
    embedding    vector(768),
    metadata     TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ,
    access_count INTEGER     DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_memory_embedding_hnsw ON memory USING hnsw (embedding vector_cosine_ops);
`

func TestUpgradeFromGORM(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping upgrade test in short mode")
	}

	ctx := context.Background()
	connStr := dbtest.StartT(ctx, t)

	// ── Step 1: apply the GORM-era schema ────────────────────────────────────
	rawDB, err := sql.Open("pgx", connStr)
	require.NoError(t, err)
	t.Cleanup(func() { rawDB.Close() })

	_, err = rawDB.ExecContext(ctx, gormSchema)
	require.NoError(t, err, "GORM schema setup failed")

	// ── Step 2: seed pre-migration data ──────────────────────────────────────
	now := time.Now().UTC().Truncate(time.Millisecond)
	softDeleted := now.Add(-24 * time.Hour)

	seeds := []struct {
		name  string
		query string
		args  []any
	}{
		{
			"agent",
			`INSERT INTO agent (id, type, created_at, updated_at) VALUES ($1, $2, $3, $4)`,
			[]any{"agent-1", "autogen", now, now},
		},
		{
			"session",
			`INSERT INTO session (id, user_id, name, agent_id, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`,
			[]any{"session-1", "user-1", "test session", "agent-1", now, now},
		},
		{
			"event",
			`INSERT INTO event (id, user_id, session_id, data, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`,
			[]any{"event-1", "user-1", "session-1", `{"role":"user"}`, now, now},
		},
		{
			"task",
			`INSERT INTO task (id, session_id, data, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)`,
			[]any{"task-1", "session-1", `{"id":"task-1"}`, now, now},
		},
		{
			"toolserver",
			`INSERT INTO toolserver (name, group_kind, description, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)`,
			[]any{"server-1", "MCPServer.kagent.dev", "test server", now, now},
		},
		{
			"tool",
			`INSERT INTO tool (id, server_name, group_kind, description, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`,
			[]any{"tool-1", "server-1", "MCPServer.kagent.dev", "a tool", now, now},
		},
		// Soft-deleted CrewAI memory row — simulates GORM's Delete() behaviour.
		// After migration the upsert must revive it (deleted_at = NULL).
		{
			"crewai_agent_memory (soft-deleted)",
			`INSERT INTO crewai_agent_memory (user_id, thread_id, memory_data, created_at, updated_at, deleted_at) VALUES ($1, $2, $3, $4, $5, $6)`,
			[]any{"user-1", "thread-1", `{"task_description":"old task"}`, now, now, softDeleted},
		},
		// Soft-deleted CrewAI flow state row — same scenario.
		{
			"crewai_flow_state (soft-deleted)",
			`INSERT INTO crewai_flow_state (user_id, thread_id, method_name, state_data, created_at, updated_at, deleted_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			[]any{"user-1", "thread-1", "kickoff", `{"status":"done"}`, now, now, softDeleted},
		},
		// Memory row with a manually supplied ID (old GORM BeforeCreate behaviour).
		{
			"memory",
			`INSERT INTO memory (id, agent_name, user_id, content, embedding, metadata, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			[]any{"mem-1", "agent-1", "user-1", "hello world", pgvector.NewVector(make([]float32, 768)), "{}", now},
		},
	}

	for _, s := range seeds {
		_, err := rawDB.ExecContext(ctx, s.query, s.args...)
		require.NoError(t, err, "seeding %s failed", s.name)
	}

	// ── Step 3: run the new migrations ───────────────────────────────────────
	dbtest.MigrateT(t, connStr, true)

	// ── Step 4: connect via the new client ───────────────────────────────────
	db, err := Connect(ctx, &PostgresConfig{URL: connStr})
	require.NoError(t, err)
	client := NewClient(db)

	// ── Step 5: verify pre-existing data is readable ─────────────────────────

	agent, err := client.GetAgent(ctx, "agent-1")
	require.NoError(t, err)
	assert.Equal(t, "agent-1", agent.ID)
	assert.Equal(t, "autogen", agent.Type)

	session, err := client.GetSession(ctx, "session-1", "user-1")
	require.NoError(t, err)
	assert.Equal(t, "session-1", session.ID)
	assert.Equal(t, "agent-1", *session.AgentID)

	events, err := client.ListEventsForSession(ctx, "session-1", "user-1", dbpkg.QueryOptions{})
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "event-1", events[0].ID)

	tasks, err := client.ListTasksForSession(ctx, "session-1")
	require.NoError(t, err)
	require.Len(t, tasks, 1)

	toolServer, err := client.GetToolServer(ctx, "server-1")
	require.NoError(t, err)
	assert.Equal(t, "server-1", toolServer.Name)

	tools, err := client.ListToolsForServer(ctx, "server-1", "MCPServer.kagent.dev")
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "tool-1", tools[0].ID)

	// ── Step 6: verify soft-deleted CrewAI rows are revived by upsert ────────
	// Before upsert both rows are invisible (deleted_at IS NOT NULL).
	results, err := client.SearchCrewAIMemoryByTask(ctx, "user-1", "thread-1", "old task", 10)
	require.NoError(t, err)
	assert.Empty(t, results, "soft-deleted memory should be invisible before upsert")

	err = client.StoreCrewAIMemory(ctx, &dbpkg.CrewAIAgentMemory{
		UserID:     "user-1",
		ThreadID:   "thread-1",
		MemoryData: `{"task_description":"old task"}`,
	})
	require.NoError(t, err)

	results, err = client.SearchCrewAIMemoryByTask(ctx, "user-1", "thread-1", "old task", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1, "upsert should revive soft-deleted memory row")

	// ── Step 7: verify new writes work (gen_random_uuid() default) ───────────
	embedding := pgvector.NewVector(make([]float32, 768))
	mem := &dbpkg.Memory{
		AgentName: "agent-1",
		UserID:    "user-1",
		Content:   "new memory content",
		Embedding: embedding,
		Metadata:  "{}",
	}
	err = client.StoreAgentMemory(ctx, mem)
	require.NoError(t, err)
	assert.NotEmpty(t, mem.ID, "StoreAgentMemory should populate ID via gen_random_uuid()")

	memories, err := client.ListAgentMemories(ctx, "agent-1", "user-1")
	require.NoError(t, err)
	assert.Len(t, memories, 2, "should see the seeded memory row and the new one")
}
