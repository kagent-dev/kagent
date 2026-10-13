-- +goose Up

-- The A2A push notification tables and the unique key their outbox references
-- on session_task_event joined the baseline after databases had applied it, so
-- such a database has none of them. Each statement is the baseline's own, made
-- idempotent: a database the current baseline created already has them.
CREATE TABLE IF NOT EXISTS session_push_registration (
    id UUID PRIMARY KEY,
    history_id UUID NOT NULL REFERENCES session(history_id) ON DELETE CASCADE,
    initial_message_id TEXT CHECK (initial_message_id <> ''),
    initial_request_hash BYTEA CHECK (octet_length(initial_request_hash) = 32),
    config_id TEXT NOT NULL CHECK (config_id <> ''),
    url TEXT NOT NULL CHECK (url <> ''),
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    task_id TEXT,
    closed_at TIMESTAMPTZ,
    FOREIGN KEY (history_id, task_id) REFERENCES session_task(history_id, id) ON DELETE CASCADE,
    CHECK (initial_message_id IS NOT NULL OR task_id IS NOT NULL),
    CHECK ((initial_message_id IS NULL) = (initial_request_hash IS NULL)),
    UNIQUE (id, history_id, task_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS session_push_registration_initial ON session_push_registration (history_id, initial_message_id)
    WHERE initial_message_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS session_push_registration_task_config ON session_push_registration (history_id, task_id, config_id)
    WHERE task_id IS NOT NULL AND closed_at IS NULL;
CREATE INDEX IF NOT EXISTS session_push_registration_unbound ON session_push_registration (created_at, id)
    WHERE closed_at IS NULL AND task_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS session_task_event_push_source ON session_task_event (sequence, history_id, task_id);

CREATE TABLE IF NOT EXISTS session_push_outbox (
    id UUID PRIMARY KEY,
    registration_id UUID NOT NULL,
    history_id UUID NOT NULL,
    task_id TEXT NOT NULL,
    config_revision BIGINT NOT NULL CHECK (config_revision > 0),
    source_event_sequence BIGINT NOT NULL,
    payload BYTEA NOT NULL CHECK (octet_length(payload) > 0),
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'sending', 'delivered', 'failed', 'canceled')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    claim_token UUID,
    lease_until TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    last_error TEXT,
    FOREIGN KEY (registration_id, history_id, task_id)
        REFERENCES session_push_registration(id, history_id, task_id) ON DELETE CASCADE,
    FOREIGN KEY (source_event_sequence, history_id, task_id)
        REFERENCES session_task_event(sequence, history_id, task_id) ON DELETE CASCADE,
    UNIQUE (registration_id, config_revision, source_event_sequence),
    CHECK ((state = 'sending' AND claim_token IS NOT NULL AND lease_until IS NOT NULL AND attempt_count > 0)
        OR (state <> 'sending' AND claim_token IS NULL AND lease_until IS NULL)),
    CHECK ((state = 'delivered') = (delivered_at IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS session_push_outbox_due ON session_push_outbox (next_attempt_at, id) WHERE state = 'pending';
CREATE INDEX IF NOT EXISTS session_push_outbox_lease ON session_push_outbox (lease_until, id) WHERE state = 'sending';
CREATE INDEX IF NOT EXISTS session_push_outbox_registration ON session_push_outbox (registration_id);

-- +goose Down

-- The tables and the index belong to the baseline, whose Down drops them.
SELECT 1;
