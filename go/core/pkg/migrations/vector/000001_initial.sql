-- +goose Up

-- Kagent 1.0 vector baseline.

CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE memory (
    id           TEXT PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_name   TEXT,
    user_id      TEXT,
    content      TEXT,
    embedding    vector(768),
    metadata     TEXT,
    created_at   TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,
    access_count BIGINT DEFAULT 0
);
CREATE INDEX idx_memory_agent_user ON memory(agent_name, user_id);
CREATE INDEX idx_memory_expires_at ON memory(expires_at);
CREATE INDEX idx_memory_embedding_hnsw ON memory USING hnsw (embedding vector_cosine_ops);

-- +goose Down

DROP TABLE memory;
