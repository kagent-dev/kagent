ALTER TABLE agent_instance_task
    ADD COLUMN IF NOT EXISTS initial_message_id TEXT,
    ADD COLUMN IF NOT EXISTS request_hash BYTEA;

CREATE UNIQUE INDEX IF NOT EXISTS agent_instance_task_message_idx
    ON agent_instance_task (instance_id, initial_message_id)
    WHERE initial_message_id IS NOT NULL;
