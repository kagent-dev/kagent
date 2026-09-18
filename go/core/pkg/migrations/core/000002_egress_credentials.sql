-- +goose Up
ALTER TABLE runtime_revision ADD COLUMN credentials JSONB NOT NULL DEFAULT '[]'
    CHECK (jsonb_typeof(credentials) = 'array');

-- +goose Down
ALTER TABLE runtime_revision DROP COLUMN credentials;
