-- +goose Up
CREATE INDEX session_checkpoint_pending_idx ON session_checkpoint (id)
    WHERE state IN ('CREATING', 'DELETING');

-- +goose Down
DROP INDEX session_checkpoint_pending_idx;
