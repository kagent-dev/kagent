-- +goose Up
-- MCP payloads are opaque JSON, not queryable database documents. JSONB rejects
-- escaped NUL characters and numbers outside PostgreSQL's numeric range even
-- when the payload is valid JSON. JSON preserves these values without decoding.
ALTER TABLE dsh_approval_operations
    DROP CONSTRAINT dsh_approval_operations_arguments_check,
    ALTER COLUMN arguments TYPE JSON USING arguments::json,
    ALTER COLUMN result TYPE JSON USING result::json,
    ADD CONSTRAINT dsh_approval_operations_arguments_check CHECK (json_typeof(arguments) = 'object');

-- +goose Down
-- Refuse rollback atomically if newer payloads cannot be represented as JSONB;
-- never discard or sanitize an approved action or its recorded result.
ALTER TABLE dsh_approval_operations
    DROP CONSTRAINT dsh_approval_operations_arguments_check,
    ALTER COLUMN arguments TYPE JSONB USING arguments::jsonb,
    ALTER COLUMN result TYPE JSONB USING result::jsonb,
    ADD CONSTRAINT dsh_approval_operations_arguments_check CHECK (jsonb_typeof(arguments) = 'object');
