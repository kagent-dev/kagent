ALTER TABLE agent_instance_task
    ADD COLUMN IF NOT EXISTS snapshot_atespace TEXT,
    ADD COLUMN IF NOT EXISTS snapshot_name TEXT,
    ADD COLUMN IF NOT EXISTS snapshot_uid TEXT,
    ADD COLUMN IF NOT EXISTS snapshot_content_scope TEXT,
    ADD COLUMN IF NOT EXISTS history_sequence BIGINT;

DROP INDEX IF EXISTS agent_instance_one_active_task_idx;
CREATE UNIQUE INDEX IF NOT EXISTS agent_instance_one_active_task_idx
    ON agent_instance_task (instance_id)
    WHERE state NOT IN (
        'TASK_STATE_COMPLETED',
        'TASK_STATE_CANCELED',
        'TASK_STATE_FAILED',
        'TASK_STATE_REJECTED',
        'TASK_STATE_INPUT_REQUIRED',
        'TASK_STATE_AUTH_REQUIRED'
    );
