-- +goose Up

-- Explicit Agents replace derived pairs. Instances and checkpoints retain their
-- existing runtime pins; new preparations are selected by Agent identity.
DROP VIEW unreferenced_runtime_revision;
DROP TABLE agent_template_harness_pair;
ALTER TABLE runtime_revision
 ADD COLUMN agent_name TEXT NOT NULL DEFAULT '',
 ADD COLUMN agent_uid TEXT NOT NULL DEFAULT '',
 ALTER COLUMN agent_template_name SET DEFAULT '',
 ALTER COLUMN agent_template_uid SET DEFAULT '',
 ALTER COLUMN harness_name SET DEFAULT '',
 ALTER COLUMN harness_uid SET DEFAULT '';

CREATE TABLE agent_definition (
 namespace TEXT NOT NULL,
 agent_name TEXT NOT NULL,
 agent_uid TEXT NOT NULL,
 desired_revision TEXT NOT NULL,
 latest_successful_revision TEXT REFERENCES runtime_revision(revision) ON DELETE RESTRICT,
 retired_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 PRIMARY KEY (namespace, agent_uid)
);
CREATE UNIQUE INDEX agent_definition_active_name_idx ON agent_definition(namespace, agent_name)
 WHERE retired_at IS NULL;

CREATE VIEW unreferenced_runtime_revision AS
SELECT r.revision FROM runtime_revision r
WHERE NOT EXISTS (
    SELECT 1 FROM agent_definition p
    WHERE p.retired_at IS NULL
      AND (p.desired_revision = r.revision OR p.latest_successful_revision = r.revision)
)
AND NOT EXISTS (
    SELECT 1 FROM agent_instance i WHERE i.prepared_revision = r.revision
)
AND NOT EXISTS (
    SELECT 1 FROM agent_instance_checkpoint c WHERE c.prepared_revision = r.revision
);

-- +goose Down

DROP VIEW unreferenced_runtime_revision;
DROP TABLE agent_definition;
ALTER TABLE runtime_revision
 DROP COLUMN agent_name,
 DROP COLUMN agent_uid,
 ALTER COLUMN agent_template_name DROP DEFAULT,
 ALTER COLUMN agent_template_uid DROP DEFAULT,
 ALTER COLUMN harness_name DROP DEFAULT,
 ALTER COLUMN harness_uid DROP DEFAULT;

CREATE TABLE agent_template_harness_pair (
    namespace                    TEXT        NOT NULL,
    agent_template_name          TEXT        NOT NULL,
    agent_template_uid           TEXT        NOT NULL,
    harness_name                 TEXT        NOT NULL,
    harness_uid                  TEXT        NOT NULL,
    desired_revision             TEXT        NOT NULL,
    latest_successful_revision   TEXT        REFERENCES runtime_revision(revision) ON DELETE RESTRICT,
    retired_at                   TIMESTAMPTZ,
    created_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (namespace, agent_template_uid, harness_uid)
);
CREATE INDEX agent_template_harness_pair_name_idx
    ON agent_template_harness_pair (namespace, agent_template_name, harness_name);

CREATE VIEW unreferenced_runtime_revision AS
SELECT r.revision FROM runtime_revision r
WHERE NOT EXISTS (
    SELECT 1 FROM agent_template_harness_pair p
    WHERE p.retired_at IS NULL
      AND (p.desired_revision = r.revision OR p.latest_successful_revision = r.revision)
)
AND NOT EXISTS (
    SELECT 1 FROM agent_instance i WHERE i.prepared_revision = r.revision
)
AND NOT EXISTS (
    SELECT 1 FROM agent_instance_checkpoint c WHERE c.prepared_revision = r.revision
);
