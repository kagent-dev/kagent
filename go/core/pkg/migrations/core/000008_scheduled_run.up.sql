CREATE TABLE IF NOT EXISTS scheduled_run_executions (
    id                      TEXT        PRIMARY KEY,
    scheduled_run_namespace TEXT        NOT NULL,
    scheduled_run_name      TEXT        NOT NULL,
    scheduled_run_uid       TEXT        NOT NULL,
    start_time              TIMESTAMPTZ NOT NULL,
    completion_time         TIMESTAMPTZ,
    trigger                 TEXT        NOT NULL,
    session_id              TEXT,
    task_id                 TEXT,
    status                  TEXT        NOT NULL,
    status_message          TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT scheduled_run_executions_id_length
        CHECK (CHAR_LENGTH(id) BETWEEN 1 AND 128),
    CONSTRAINT scheduled_run_executions_trigger
        CHECK (trigger IN ('Scheduled', 'Manual')),
    CONSTRAINT scheduled_run_executions_status
        CHECK (status IN ('DispatchFailed', 'InProgress', 'Succeeded', 'Failed', 'TimedOut')),
    CONSTRAINT scheduled_run_executions_completion_time
        CHECK (completion_time IS NULL OR completion_time >= start_time),
    CONSTRAINT scheduled_run_executions_session_id_length
        CHECK (session_id IS NULL OR CHAR_LENGTH(session_id) BETWEEN 1 AND 256),
    CONSTRAINT scheduled_run_executions_task_id_length
        CHECK (task_id IS NULL OR CHAR_LENGTH(task_id) BETWEEN 1 AND 1024),
    CONSTRAINT scheduled_run_executions_status_message_length
        CHECK (status_message IS NULL OR CHAR_LENGTH(status_message) BETWEEN 1 AND 32768)
);

CREATE INDEX IF NOT EXISTS idx_scheduled_run_executions_started
    ON scheduled_run_executions (scheduled_run_namespace, scheduled_run_name, scheduled_run_uid, start_time DESC, id DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_scheduled_run_executions_session_id
    ON scheduled_run_executions (session_id)
    WHERE session_id IS NOT NULL;
