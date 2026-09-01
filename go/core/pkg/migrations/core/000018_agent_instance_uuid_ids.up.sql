ALTER TABLE agent_instance_share DROP CONSTRAINT agent_instance_share_instance_id_fkey;
ALTER TABLE agent_instance_task DROP CONSTRAINT agent_instance_task_context_id_fkey;
ALTER TABLE agent_instance_task_event DROP CONSTRAINT agent_instance_task_event_context_id_fkey;
ALTER TABLE agent_instance_checkpoint DROP CONSTRAINT agent_instance_checkpoint_source_context_id_fkey;
ALTER TABLE agent_instance DROP CONSTRAINT agent_instance_context_id_fkey;
ALTER TABLE agent_instance DROP CONSTRAINT agent_instance_source_checkpoint_id_fkey;

ALTER TABLE a2a_context ALTER COLUMN id TYPE UUID USING id::uuid;
ALTER TABLE agent_instance
    ALTER COLUMN id TYPE UUID USING id::uuid,
    ALTER COLUMN context_id TYPE UUID USING context_id::uuid,
    ALTER COLUMN source_checkpoint_id TYPE UUID USING source_checkpoint_id::uuid;
ALTER TABLE agent_instance_share
    ALTER COLUMN id TYPE UUID USING id::uuid,
    ALTER COLUMN instance_id TYPE UUID USING instance_id::uuid;
ALTER TABLE agent_instance_task ALTER COLUMN context_id TYPE UUID USING context_id::uuid;
ALTER TABLE agent_instance_task_event ALTER COLUMN context_id TYPE UUID USING context_id::uuid;
ALTER TABLE agent_instance_checkpoint
    ALTER COLUMN id TYPE UUID USING id::uuid,
    ALTER COLUMN source_instance_id TYPE UUID USING source_instance_id::uuid,
    ALTER COLUMN source_context_id TYPE UUID USING source_context_id::uuid;

ALTER TABLE agent_instance_share ADD CONSTRAINT agent_instance_share_instance_id_fkey
    FOREIGN KEY (instance_id) REFERENCES agent_instance(id) ON DELETE CASCADE;
ALTER TABLE agent_instance_task ADD CONSTRAINT agent_instance_task_context_id_fkey
    FOREIGN KEY (context_id) REFERENCES a2a_context(id) ON DELETE CASCADE;
ALTER TABLE agent_instance_task_event ADD CONSTRAINT agent_instance_task_event_context_id_fkey
    FOREIGN KEY (context_id) REFERENCES a2a_context(id) ON DELETE CASCADE;
ALTER TABLE agent_instance_checkpoint ADD CONSTRAINT agent_instance_checkpoint_source_context_id_fkey
    FOREIGN KEY (source_context_id) REFERENCES a2a_context(id) ON DELETE RESTRICT;
ALTER TABLE agent_instance ADD CONSTRAINT agent_instance_context_id_fkey
    FOREIGN KEY (context_id) REFERENCES a2a_context(id) ON DELETE RESTRICT;
ALTER TABLE agent_instance ADD CONSTRAINT agent_instance_source_checkpoint_id_fkey
    FOREIGN KEY (source_checkpoint_id) REFERENCES agent_instance_checkpoint(id) ON DELETE RESTRICT;
