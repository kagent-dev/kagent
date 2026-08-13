CREATE TABLE IF NOT EXISTS runtime_revision (
    revision TEXT PRIMARY KEY,
    namespace TEXT NOT NULL,
    agent_template_name TEXT NOT NULL,
    agent_template_uid TEXT NOT NULL,
    harness_name TEXT NOT NULL,
    harness_uid TEXT NOT NULL,
    source_snapshot JSONB NOT NULL,
    egress_destinations TEXT[] NOT NULL DEFAULT '{}',
    actor_template_namespace TEXT NOT NULL,
    actor_template_name TEXT NOT NULL,
    actor_template_uid TEXT NOT NULL DEFAULT '',
    phase TEXT NOT NULL,
    golden_snapshot TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (actor_template_namespace, actor_template_name)
);

CREATE TABLE IF NOT EXISTS agent_template_harness_pair (
    namespace TEXT NOT NULL,
    agent_template_name TEXT NOT NULL,
    agent_template_uid TEXT NOT NULL,
    harness_name TEXT NOT NULL,
    harness_uid TEXT NOT NULL,
    desired_revision TEXT NOT NULL,
    latest_successful_revision TEXT REFERENCES runtime_revision(revision) ON DELETE RESTRICT,
    retired_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (namespace, agent_template_uid, harness_uid)
);

CREATE INDEX IF NOT EXISTS agent_template_harness_pair_name_idx
    ON agent_template_harness_pair (namespace, agent_template_name, harness_name);
