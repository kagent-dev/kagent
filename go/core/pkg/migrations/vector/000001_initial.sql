-- +goose Up

-- Kagent 1.0 vector baseline.

CREATE TABLE memory (
    id           TEXT PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_name   TEXT,
    user_id      TEXT,
    content      TEXT,
    metadata     TEXT,
    created_at   TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,
    access_count BIGINT DEFAULT 0
);
CREATE INDEX idx_memory_agent_user ON memory(agent_name, user_id);
CREATE INDEX idx_memory_expires_at ON memory(expires_at);

-- +goose StatementBegin
DO $vector$
DECLARE
    vector_schema text := COALESCE(NULLIF(current_setting('kagent.vector_schema', true), ''), 'public');
BEGIN
    EXECUTE format('ALTER TABLE memory ADD COLUMN embedding %I.vector(768)', vector_schema);
    EXECUTE format('CREATE INDEX idx_memory_embedding_hnsw ON memory USING hnsw (embedding %I.vector_cosine_ops)', vector_schema);
END
$vector$;
-- +goose StatementEnd

-- +goose Down

DROP TABLE memory;
