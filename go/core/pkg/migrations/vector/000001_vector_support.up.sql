CREATE EXTENSION IF NOT EXISTS vector;

-- Matches the schema GORM AutoMigrate produced for the Memory struct.
-- GORM does not create HNSW indexes automatically; that is added in migration 000002.
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
CREATE INDEX IF NOT EXISTS idx_memory_agent_user ON memory(agent_name, user_id);
CREATE INDEX IF NOT EXISTS idx_memory_expires_at ON memory(expires_at);
