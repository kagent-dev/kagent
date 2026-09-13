-- +goose Up

ALTER TABLE runtime_revision ADD COLUMN cleanup_pending_since TIMESTAMPTZ;

UPDATE runtime_revision
SET cleanup_pending_since = deleted_at
WHERE deleted_at IS NOT NULL;

-- +goose Down

ALTER TABLE runtime_revision DROP COLUMN cleanup_pending_since;
