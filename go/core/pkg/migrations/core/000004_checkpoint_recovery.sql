-- +goose Up
ALTER TABLE session_checkpoint
    ADD COLUMN executor_id uuid,
    ADD COLUMN operation_error bytea,
    ADD COLUMN executor_expires_at timestamptz,
    ADD CONSTRAINT session_checkpoint_executor_pair CHECK (
        (executor_id IS NULL) = (executor_expires_at IS NULL)
    );

CREATE INDEX session_checkpoint_pending_idx ON session_checkpoint (id)
    WHERE state IN ('CREATING', 'DELETING');

-- +goose Down
DROP INDEX session_checkpoint_pending_idx;
ALTER TABLE session_checkpoint
    DROP CONSTRAINT session_checkpoint_executor_pair,
    DROP COLUMN executor_expires_at,
    DROP COLUMN executor_id,
    DROP COLUMN operation_error;
