-- +goose Up
ALTER TABLE session
    ADD COLUMN dispatch_cleanup_executor_id UUID,
    ADD CONSTRAINT session_dispatch_cleanup_claim CHECK (
        dispatch_cleanup_executor_id IS NULL OR dispatch_id IS NOT NULL);

CREATE INDEX session_dispatch_cleanup_pending_idx ON session (id)
    WHERE dispatch_id IS NOT NULL AND dispatch_cleanup_executor_id IS NULL;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM session WHERE dispatch_cleanup_executor_id IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot remove dispatch cleanup while issued work is unresolved';
    END IF;
END $$;
-- +goose StatementEnd

DROP INDEX session_dispatch_cleanup_pending_idx;
ALTER TABLE session
    DROP CONSTRAINT session_dispatch_cleanup_claim,
    DROP COLUMN dispatch_cleanup_executor_id;
