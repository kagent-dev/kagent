-- +goose Up
ALTER TABLE session_checkpoint
    ADD COLUMN executor_id uuid,
    ADD COLUMN executor_expires_at timestamptz,
    ADD CONSTRAINT session_checkpoint_executor_pair CHECK (
        (executor_id IS NULL) = (executor_expires_at IS NULL)
    );

-- +goose Down
ALTER TABLE session_checkpoint
    DROP CONSTRAINT session_checkpoint_executor_pair,
    DROP COLUMN executor_expires_at,
    DROP COLUMN executor_id;
