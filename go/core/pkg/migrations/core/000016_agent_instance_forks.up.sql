CREATE TABLE IF NOT EXISTS a2a_context (
    id TEXT PRIMARY KEY,
    namespace TEXT NOT NULL,
    user_id TEXT NOT NULL CHECK (user_id <> ''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO a2a_context (id, namespace, user_id)
SELECT id, namespace, user_id FROM agent_instance
ON CONFLICT DO NOTHING;

INSERT INTO a2a_context (id, namespace, user_id, created_at)
SELECT source_instance_id, namespace, user_id, MIN(created_at)
FROM agent_instance_checkpoint
GROUP BY source_instance_id, namespace, user_id
ON CONFLICT DO NOTHING;

ALTER TABLE agent_instance
    ADD COLUMN IF NOT EXISTS context_id TEXT;

UPDATE agent_instance SET context_id = id WHERE context_id IS NULL;

ALTER TABLE agent_instance
    ALTER COLUMN context_id SET NOT NULL,
    ADD CONSTRAINT agent_instance_context_id_fkey
        FOREIGN KEY (context_id) REFERENCES a2a_context(id) ON DELETE RESTRICT;

ALTER TABLE agent_instance_task
    DROP CONSTRAINT IF EXISTS agent_instance_task_instance_id_fkey;

ALTER TABLE agent_instance_task
    RENAME COLUMN instance_id TO context_id;

ALTER TABLE agent_instance_task
    ADD CONSTRAINT agent_instance_task_context_id_fkey
        FOREIGN KEY (context_id) REFERENCES a2a_context(id) ON DELETE CASCADE;

ALTER TABLE agent_instance_task_event
    DROP CONSTRAINT IF EXISTS agent_instance_task_event_instance_id_fkey;

ALTER TABLE agent_instance_task_event
    RENAME COLUMN instance_id TO context_id;

ALTER TABLE agent_instance_task_event
    ADD CONSTRAINT agent_instance_task_event_context_id_fkey
        FOREIGN KEY (context_id) REFERENCES a2a_context(id) ON DELETE CASCADE;

ALTER TABLE agent_instance_task_event
    ADD COLUMN IF NOT EXISTS message_id TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS agent_instance_task_event_message_idx
    ON agent_instance_task_event (context_id, task_id, message_id)
    WHERE message_id IS NOT NULL;

ALTER TABLE agent_instance_checkpoint
    ADD COLUMN IF NOT EXISTS source_context_id TEXT,
    ADD COLUMN IF NOT EXISTS prepared_revision TEXT REFERENCES runtime_revision(revision) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS source_instance_data BYTEA;

UPDATE agent_instance_checkpoint
SET source_context_id = source_instance_id
WHERE source_context_id IS NULL;

ALTER TABLE agent_instance_checkpoint
    ALTER COLUMN source_context_id SET NOT NULL,
    ADD CONSTRAINT agent_instance_checkpoint_source_context_id_fkey
        FOREIGN KEY (source_context_id) REFERENCES a2a_context(id) ON DELETE RESTRICT;

UPDATE agent_instance_checkpoint c
SET prepared_revision = i.prepared_revision,
    source_instance_data = i.data
FROM agent_instance i
WHERE i.id = c.source_instance_id
  AND (c.prepared_revision IS NULL OR c.source_instance_data IS NULL);

ALTER TABLE agent_instance
    ADD COLUMN IF NOT EXISTS source_checkpoint_id TEXT REFERENCES agent_instance_checkpoint(id) ON DELETE RESTRICT;
