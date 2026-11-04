-- +goose Up

-- Native synchronous delegation waits survive observer disconnects and controller restarts.
CREATE TABLE task_continuation (
    id UUID PRIMARY KEY,
    parent_session_id UUID NOT NULL REFERENCES session(id) ON DELETE CASCADE,
    parent_task_id TEXT NOT NULL,
    child_session_id UUID NOT NULL REFERENCES session(id) ON DELETE CASCADE,
    child_message_id TEXT NOT NULL,
    child_task_id TEXT,
    resume_task_id TEXT,
    awaited BOOLEAN NOT NULL DEFAULT FALSE,
    delivered BOOLEAN NOT NULL DEFAULT FALSE,
    abandoned BOOLEAN NOT NULL DEFAULT FALSE,
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    claim_token UUID,
    lease_until TIMESTAMPTZ,
    UNIQUE (parent_task_id, child_session_id, child_message_id),
    CHECK (parent_session_id <> child_session_id),
    CHECK ((claim_token IS NULL) = (lease_until IS NULL))
);
CREATE INDEX task_continuation_pending ON task_continuation (parent_session_id)
    WHERE NOT delivered AND NOT abandoned;
CREATE INDEX task_continuation_child ON task_continuation (child_session_id, child_message_id);

-- +goose Down
DROP TABLE task_continuation;
