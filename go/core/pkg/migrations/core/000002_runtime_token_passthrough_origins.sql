-- +goose Up

ALTER TABLE runtime_revision ADD COLUMN token_passthrough_origins TEXT[] NOT NULL DEFAULT '{}';

-- PostgreSQL expands r.* when the view is created.
DROP VIEW agent_runtime_revision;
CREATE VIEW agent_runtime_revision AS
    SELECT r.*, a.agent_name, a.agent_uid, a.agent_card
    FROM runtime_revision r JOIN agent_revision a USING (revision) WHERE r.kind = 'agent';

-- Mutable controller output lives outside immutable runtime revision inputs.
CREATE TABLE runtime_egress_policy (
    revision TEXT PRIMARY KEY REFERENCES runtime_revision (revision) ON DELETE CASCADE,
    destinations TEXT[] NOT NULL,
    credentials JSONB NOT NULL CHECK (jsonb_typeof(credentials) = 'array')
);

-- Target changed sources without scanning all live instances.
CREATE INDEX agent_revision_source_idx ON agent_revision (agent_name, revision);
CREATE INDEX sandbox_revision_source_idx ON sandbox_revision (sandbox_template_name, revision);
CREATE INDEX runtime_instance_revision_idx ON runtime_instance (prepared_revision, id)
    WHERE state <> 'RUNTIME_STATE_DELETED' AND operation <> 'RUNTIME_OPERATION_DELETE';

-- +goose Down

DROP TABLE runtime_egress_policy;

DROP INDEX runtime_instance_revision_idx;
DROP INDEX sandbox_revision_source_idx;
DROP INDEX agent_revision_source_idx;

DROP VIEW agent_runtime_revision;
ALTER TABLE runtime_revision DROP COLUMN token_passthrough_origins;
CREATE VIEW agent_runtime_revision AS
    SELECT r.*, a.agent_name, a.agent_uid, a.agent_card
    FROM runtime_revision r JOIN agent_revision a USING (revision) WHERE r.kind = 'agent';
