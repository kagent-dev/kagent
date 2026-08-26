DROP INDEX IF EXISTS agent_instance_task_event_message_idx;

ALTER TABLE agent_instance_task_event
    DROP COLUMN IF EXISTS message_id;

ALTER TABLE agent_instance_task_event
    DROP CONSTRAINT IF EXISTS agent_instance_task_event_context_id_fkey;

ALTER TABLE agent_instance_task_event
    RENAME COLUMN context_id TO instance_id;

ALTER TABLE agent_instance_task_event
    ADD CONSTRAINT agent_instance_task_event_instance_id_fkey
        FOREIGN KEY (instance_id) REFERENCES agent_instance(id) ON DELETE CASCADE;

ALTER TABLE agent_instance_task
    DROP CONSTRAINT IF EXISTS agent_instance_task_context_id_fkey;

ALTER TABLE agent_instance_task
    RENAME COLUMN context_id TO instance_id;

ALTER TABLE agent_instance_task
    ADD CONSTRAINT agent_instance_task_instance_id_fkey
        FOREIGN KEY (instance_id) REFERENCES agent_instance(id) ON DELETE CASCADE;

ALTER TABLE agent_instance
    DROP COLUMN IF EXISTS source_checkpoint_id,
    DROP COLUMN IF EXISTS context_id;

ALTER TABLE agent_instance_checkpoint
    DROP COLUMN IF EXISTS source_context_id,
    DROP COLUMN IF EXISTS source_labels,
    DROP COLUMN IF EXISTS prepared_revision;

DROP TABLE IF EXISTS a2a_context;
