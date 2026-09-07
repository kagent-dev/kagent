-- +goose Up
ALTER TABLE agent_instance
    ADD COLUMN deleted_at TIMESTAMPTZ,
    ADD COLUMN agent_template_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN harness_name TEXT NOT NULL DEFAULT '';

-- Preserve queryable provenance after the runtime revision is released.
UPDATE agent_instance i
SET agent_template_name = r.agent_template_name, harness_name = r.harness_name
FROM runtime_revision r WHERE r.revision = i.prepared_revision;

ALTER TABLE agent_instance DROP CONSTRAINT agent_instance_state_check;
ALTER TABLE agent_instance ADD CONSTRAINT agent_instance_state_check
    CHECK (state IN ('CREATING', 'READY', 'SUSPENDED', 'FAILED', 'DELETED'));
ALTER TABLE agent_instance ADD CONSTRAINT agent_instance_tombstone_check CHECK (
    (deleted_at IS NULL AND state <> 'DELETED')
    OR (deleted_at IS NOT NULL AND state = 'DELETED' AND operation = 'NONE'
        AND prepared_revision IS NULL AND source_checkpoint_id IS NULL)
);
CREATE INDEX agent_instance_live_idx ON agent_instance (namespace, user_id, id)
    WHERE deleted_at IS NULL;

-- +goose Down
DELETE FROM agent_instance WHERE deleted_at IS NOT NULL;
DROP INDEX agent_instance_live_idx;
ALTER TABLE agent_instance DROP CONSTRAINT agent_instance_tombstone_check;
ALTER TABLE agent_instance DROP CONSTRAINT agent_instance_state_check;
ALTER TABLE agent_instance ADD CONSTRAINT agent_instance_state_check
    CHECK (state IN ('CREATING', 'READY', 'SUSPENDED', 'FAILED'));
ALTER TABLE agent_instance
    DROP COLUMN deleted_at,
    DROP COLUMN agent_template_name,
    DROP COLUMN harness_name;
