CREATE TABLE IF NOT EXISTS agent_instance_task (
    instance_id TEXT NOT NULL REFERENCES agent_instance(id) ON DELETE CASCADE,
    id TEXT NOT NULL,
    state TEXT NOT NULL,
    status_timestamp TIMESTAMPTZ,
    data BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (instance_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS agent_instance_one_active_task_idx
    ON agent_instance_task (instance_id)
    WHERE state NOT IN (
        'TASK_STATE_COMPLETED',
        'TASK_STATE_CANCELED',
        'TASK_STATE_FAILED',
        'TASK_STATE_REJECTED'
    );

CREATE INDEX IF NOT EXISTS agent_instance_task_list_idx
    ON agent_instance_task (instance_id, id);

CREATE TABLE IF NOT EXISTS agent_instance_task_event (
    sequence BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    instance_id TEXT NOT NULL REFERENCES agent_instance(id) ON DELETE CASCADE,
    task_id TEXT,
    data BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS agent_instance_task_event_instance_sequence_idx
    ON agent_instance_task_event (instance_id, sequence);
