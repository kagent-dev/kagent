-- +goose Up

ALTER TABLE runtime_revision ADD COLUMN passthrough_origins TEXT[] NOT NULL DEFAULT '{}';

-- PostgreSQL expands r.* when the view is created.
DROP VIEW agent_runtime_revision;
CREATE VIEW agent_runtime_revision AS
    SELECT r.*, a.agent_name, a.agent_uid, a.agent_card
    FROM runtime_revision r JOIN agent_revision a USING (revision) WHERE r.kind = 'agent';

-- +goose Down

DROP VIEW agent_runtime_revision;
ALTER TABLE runtime_revision DROP COLUMN passthrough_origins;
CREATE VIEW agent_runtime_revision AS
    SELECT r.*, a.agent_name, a.agent_uid, a.agent_card
    FROM runtime_revision r JOIN agent_revision a USING (revision) WHERE r.kind = 'agent';
