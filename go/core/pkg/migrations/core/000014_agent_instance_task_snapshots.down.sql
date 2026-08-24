DROP INDEX IF EXISTS agent_instance_one_active_task_idx;
CREATE UNIQUE INDEX IF NOT EXISTS agent_instance_one_active_task_idx
    ON agent_instance_task (instance_id)
    WHERE state NOT IN (
        'TASK_STATE_COMPLETED',
        'TASK_STATE_CANCELED',
        'TASK_STATE_FAILED',
        'TASK_STATE_REJECTED'
    );

ALTER TABLE agent_instance_task
    DROP COLUMN IF EXISTS history_sequence,
    DROP COLUMN IF EXISTS snapshot_uid,
    DROP COLUMN IF EXISTS snapshot_name,
    DROP COLUMN IF EXISTS snapshot_atespace;
