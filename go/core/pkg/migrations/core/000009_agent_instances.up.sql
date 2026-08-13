ALTER TABLE agent_template_harness_pair
    ADD COLUMN IF NOT EXISTS agent_template_labels JSONB NOT NULL DEFAULT '{}';

CREATE TABLE IF NOT EXISTS agent_instance (
    id TEXT PRIMARY KEY,
    namespace TEXT NOT NULL,
    user_id TEXT NOT NULL CHECK (user_id <> ''),
    request_id TEXT NOT NULL,
    prepared_revision TEXT REFERENCES runtime_revision(revision) ON DELETE RESTRICT,
    actor_uid TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL,
    labels JSONB NOT NULL DEFAULT '{}',
    data BYTEA NOT NULL,
    CHECK (state IN ('CREATING', 'READY', 'SUSPENDED', 'FAILED')),
    UNIQUE (user_id, namespace, request_id)
);

CREATE INDEX IF NOT EXISTS agent_instance_namespace_user_id_id_idx
    ON agent_instance (namespace, user_id, id);

CREATE TABLE IF NOT EXISTS agent_instance_share (
    id TEXT PRIMARY KEY,
    namespace TEXT NOT NULL,
    instance_id TEXT NOT NULL REFERENCES agent_instance(id) ON DELETE CASCADE,
    creator TEXT NOT NULL,
    permission TEXT NOT NULL,
    token_hash BYTEA NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (permission IN ('READ_ONLY', 'READ_WRITE'))
);

CREATE INDEX IF NOT EXISTS agent_instance_share_instance_idx
    ON agent_instance_share (namespace, instance_id, id);
