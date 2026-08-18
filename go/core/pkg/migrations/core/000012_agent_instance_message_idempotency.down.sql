DROP INDEX IF EXISTS agent_instance_task_message_idx;
ALTER TABLE agent_instance_task
    DROP COLUMN IF EXISTS request_hash,
    DROP COLUMN IF EXISTS initial_message_id;
