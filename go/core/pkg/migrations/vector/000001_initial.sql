-- +goose Up

-- Kagent vector baseline. This section applies the previous schema changes in order.

CREATE EXTENSION IF NOT EXISTS vector;

-- This table matches the schema that GORM created for the Memory struct.
CREATE TABLE memory (
    id           TEXT        PRIMARY KEY,
    agent_name   TEXT,
    user_id      TEXT,
    content      TEXT,
    embedding    vector(768),
    metadata     TEXT,
    created_at   TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,
    access_count BIGINT      DEFAULT 0
);
CREATE INDEX idx_memory_agent_user ON memory(agent_name, user_id);
CREATE INDEX idx_memory_expires_at ON memory(expires_at);

-- Add an HNSW index for approximate vector similarity search.
CREATE INDEX idx_memory_embedding_hnsw ON memory USING hnsw (embedding vector_cosine_ops);

ALTER TABLE memory ALTER COLUMN id SET DEFAULT gen_random_uuid();

-- +goose Down

DROP TABLE memory;
