-- The protobuf remains the public record; this column exists only so lifecycle
-- operations can use an atomic compare-and-set across controller replicas.
ALTER TABLE agent_instance
    ADD COLUMN IF NOT EXISTS operation TEXT NOT NULL DEFAULT 'NONE',
    ADD CONSTRAINT agent_instance_operation_check
        CHECK (operation IN ('NONE', 'CREATE', 'SUSPEND', 'RESUME', 'DELETE'));

UPDATE agent_instance
SET operation = 'CREATE'
WHERE state = 'CREATING';
