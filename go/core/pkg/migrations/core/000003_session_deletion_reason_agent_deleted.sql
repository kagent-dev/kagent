-- +goose Up

-- The expiration sweep deletes the sessions of a deleted Agent with a reason
-- of their own, kept apart from an idle expiry.
ALTER TABLE session DROP CONSTRAINT session_deletion_reason_check,
    ADD CONSTRAINT session_deletion_reason_check
        CHECK (deletion_reason IN ('user_requested', 'idle_timeout', 'agent_deleted'));

-- +goose Down

ALTER TABLE session DROP CONSTRAINT session_deletion_reason_check,
    ADD CONSTRAINT session_deletion_reason_check
        CHECK (deletion_reason IN ('user_requested', 'idle_timeout'));
