-- +goose Up
CREATE INDEX session_checkpoint_source_history_idx ON session_checkpoint (source_history_id, id);
CREATE INDEX session_pinned_checkpoint_idx ON session (pinned_checkpoint_id) WHERE pinned_checkpoint_id IS NOT NULL;

-- +goose Down
DROP INDEX session_pinned_checkpoint_idx;
DROP INDEX session_checkpoint_source_history_idx;
