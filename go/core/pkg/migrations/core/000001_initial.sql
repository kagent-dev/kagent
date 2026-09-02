-- +goose Up

-- Kagent baseline. This section applies the previous schema changes in order.

-- These initial definitions match the schema that GORM created.
-- Later statements produce the current Kagent schema.
--
-- Notes on column definitions vs. what you might expect:
--   - created_at/updated_at are nullable: GORM sets these in Go code, not via a
--     DB default or NOT NULL constraint.
--   - version, write_idx, access_count are BIGINT: GORM maps Go `int` to bigint.

CREATE TABLE agent (
    id         TEXT        PRIMARY KEY,
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    type       TEXT        NOT NULL,
    config     JSON
);
CREATE INDEX idx_agent_deleted_at ON agent(deleted_at);

CREATE TABLE feedback (
    id            BIGSERIAL   PRIMARY KEY,
    created_at    TIMESTAMPTZ,
    updated_at    TIMESTAMPTZ,
    deleted_at    TIMESTAMPTZ,
    user_id       TEXT        NOT NULL,
    message_id    BIGINT,
    is_positive   BOOLEAN     NOT NULL DEFAULT false,
    feedback_text TEXT        NOT NULL,
    issue_type    TEXT
);
CREATE INDEX idx_feedback_deleted_at ON feedback(deleted_at);
CREATE INDEX idx_feedback_user_id    ON feedback(user_id);
CREATE INDEX idx_feedback_message_id ON feedback(message_id);

CREATE TABLE tool (
    id          TEXT        NOT NULL,
    server_name TEXT        NOT NULL,
    group_kind  TEXT        NOT NULL,
    created_at  TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ,
    deleted_at  TIMESTAMPTZ,
    description TEXT,
    PRIMARY KEY (id, server_name, group_kind)
);
CREATE INDEX idx_tool_deleted_at ON tool(deleted_at);

CREATE TABLE toolserver (
    name           TEXT        NOT NULL,
    group_kind     TEXT        NOT NULL,
    created_at     TIMESTAMPTZ,
    updated_at     TIMESTAMPTZ,
    deleted_at     TIMESTAMPTZ,
    description    TEXT,
    last_connected TIMESTAMPTZ,
    PRIMARY KEY (name, group_kind)
);
CREATE INDEX idx_toolserver_deleted_at ON toolserver(deleted_at);

CREATE TABLE lg_checkpoint (
    user_id              TEXT        NOT NULL,
    thread_id            TEXT        NOT NULL,
    checkpoint_ns        TEXT        NOT NULL DEFAULT '',
    checkpoint_id        TEXT        NOT NULL,
    parent_checkpoint_id TEXT,
    created_at           TIMESTAMPTZ,
    updated_at           TIMESTAMPTZ,
    deleted_at           TIMESTAMPTZ,
    metadata             TEXT        NOT NULL,
    checkpoint           TEXT        NOT NULL,
    checkpoint_type      TEXT        NOT NULL,
    version              BIGINT      NOT NULL DEFAULT 1,
    PRIMARY KEY (user_id, thread_id, checkpoint_ns, checkpoint_id)
);
CREATE INDEX idx_lg_checkpoint_parent_checkpoint_id ON lg_checkpoint(parent_checkpoint_id);
CREATE INDEX idx_lgcp_list                          ON lg_checkpoint(created_at);
CREATE INDEX idx_lg_checkpoint_deleted_at           ON lg_checkpoint(deleted_at);

CREATE TABLE lg_checkpoint_write (
    user_id       TEXT        NOT NULL,
    thread_id     TEXT        NOT NULL,
    checkpoint_ns TEXT        NOT NULL DEFAULT '',
    checkpoint_id TEXT        NOT NULL,
    write_idx     BIGINT      NOT NULL,
    value         TEXT        NOT NULL,
    value_type    TEXT        NOT NULL,
    channel       TEXT        NOT NULL,
    task_id       TEXT        NOT NULL,
    created_at    TIMESTAMPTZ,
    updated_at    TIMESTAMPTZ,
    deleted_at    TIMESTAMPTZ,
    PRIMARY KEY (user_id, thread_id, checkpoint_ns, checkpoint_id, write_idx)
);
CREATE INDEX idx_lg_checkpoint_write_deleted_at ON lg_checkpoint_write(deleted_at);

CREATE TABLE crewai_agent_memory (
    user_id     TEXT        NOT NULL,
    thread_id   TEXT        NOT NULL,
    created_at  TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ,
    deleted_at  TIMESTAMPTZ,
    memory_data TEXT        NOT NULL,
    PRIMARY KEY (user_id, thread_id)
);
CREATE INDEX idx_crewai_memory_list             ON crewai_agent_memory(created_at);
CREATE INDEX idx_crewai_agent_memory_deleted_at ON crewai_agent_memory(deleted_at);

CREATE TABLE crewai_flow_state (
    user_id     TEXT        NOT NULL,
    thread_id   TEXT        NOT NULL,
    method_name TEXT        NOT NULL,
    created_at  TIMESTAMPTZ,
    updated_at  TIMESTAMPTZ,
    deleted_at  TIMESTAMPTZ,
    state_data  TEXT        NOT NULL,
    PRIMARY KEY (user_id, thread_id, method_name)
);
CREATE INDEX idx_crewai_flow_state_list       ON crewai_flow_state(created_at);
CREATE INDEX idx_crewai_flow_state_deleted_at ON crewai_flow_state(deleted_at);

-- Backfill any NULLs (none expected, but safe) then add NOT NULL constraints.
-- These columns always had DEFAULT values but were missing NOT NULL in 000001.

UPDATE feedback SET is_positive = false WHERE is_positive IS NULL;
ALTER TABLE feedback ALTER COLUMN is_positive SET NOT NULL;

UPDATE lg_checkpoint SET version = 1 WHERE version IS NULL;
ALTER TABLE lg_checkpoint ALTER COLUMN version SET NOT NULL;

ALTER TABLE agent ADD COLUMN workload_type TEXT;
UPDATE agent SET workload_type = 'deployment' WHERE workload_type IS NULL;
ALTER TABLE agent ALTER COLUMN workload_type SET DEFAULT 'deployment';
ALTER TABLE agent ALTER COLUMN workload_type SET NOT NULL;

-- Normalize the feedback primary key to (id) only.
--
-- The initial definition already uses PRIMARY KEY (id).
-- This statement preserves the final constraint from the previous sequence.
ALTER TABLE feedback
    DROP CONSTRAINT feedback_pkey,
    ADD CONSTRAINT feedback_pkey PRIMARY KEY (id);

CREATE TABLE runtime_revision (
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

CREATE TABLE agent_template_harness_pair (
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

CREATE INDEX agent_template_harness_pair_name_idx
    ON agent_template_harness_pair (namespace, agent_template_name, harness_name);

ALTER TABLE agent_template_harness_pair
    ADD COLUMN agent_template_labels JSONB NOT NULL DEFAULT '{}';

CREATE TABLE agent_instance (
    id TEXT PRIMARY KEY,
    namespace TEXT NOT NULL,
    user_id TEXT NOT NULL CHECK (user_id <> ''),
    request_id TEXT NOT NULL,
    prepared_revision TEXT REFERENCES runtime_revision(revision) ON DELETE RESTRICT,
    state TEXT NOT NULL,
    labels JSONB NOT NULL DEFAULT '{}',
    data BYTEA NOT NULL,
    CHECK (state IN ('CREATING', 'READY', 'SUSPENDED', 'FAILED')),
    UNIQUE (user_id, namespace, request_id)
);

CREATE INDEX agent_instance_namespace_user_id_id_idx
    ON agent_instance (namespace, user_id, id);

CREATE TABLE agent_instance_share (
    id TEXT PRIMARY KEY,
    namespace TEXT NOT NULL,
    instance_id TEXT NOT NULL REFERENCES agent_instance(id) ON DELETE CASCADE,
    permission TEXT NOT NULL,
    token_hash BYTEA NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (permission IN ('READ_ONLY', 'READ_WRITE'))
);

CREATE INDEX agent_instance_share_instance_idx
    ON agent_instance_share (namespace, instance_id, id);

-- The protobuf remains the public record; this column exists only so lifecycle
-- operations can use an atomic compare-and-set across controller replicas.
ALTER TABLE agent_instance
    ADD COLUMN operation TEXT NOT NULL DEFAULT 'NONE',
    ADD CONSTRAINT agent_instance_operation_check
        CHECK (operation IN ('NONE', 'CREATE', 'SUSPEND', 'RESUME', 'DELETE'));

UPDATE agent_instance
SET operation = 'CREATE'
WHERE state = 'CREATING';

CREATE TABLE agent_instance_task (
    instance_id TEXT NOT NULL REFERENCES agent_instance(id) ON DELETE CASCADE,
    id TEXT NOT NULL,
    state TEXT NOT NULL,
    status_timestamp TIMESTAMPTZ,
    data BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (instance_id, id)
);

CREATE UNIQUE INDEX agent_instance_one_active_task_idx
    ON agent_instance_task (instance_id)
    WHERE state NOT IN (
        'TASK_STATE_COMPLETED',
        'TASK_STATE_CANCELED',
        'TASK_STATE_FAILED',
        'TASK_STATE_REJECTED'
    );

CREATE INDEX agent_instance_task_list_idx
    ON agent_instance_task (instance_id, id);

CREATE TABLE agent_instance_task_event (
    sequence BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    instance_id TEXT NOT NULL REFERENCES agent_instance(id) ON DELETE CASCADE,
    task_id TEXT,
    data BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX agent_instance_task_event_instance_sequence_idx
    ON agent_instance_task_event (instance_id, sequence);

ALTER TABLE agent_instance_task
    ADD COLUMN initial_message_id TEXT,
    ADD COLUMN request_hash BYTEA;

CREATE UNIQUE INDEX agent_instance_task_message_idx
    ON agent_instance_task (instance_id, initial_message_id)
    WHERE initial_message_id IS NOT NULL;

ALTER TABLE runtime_revision
    ADD COLUMN agent_card JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE runtime_revision
    ALTER COLUMN agent_card DROP DEFAULT;

ALTER TABLE agent_instance_task
    ADD COLUMN snapshot_atespace TEXT,
    ADD COLUMN snapshot_name TEXT,
    ADD COLUMN snapshot_uid TEXT,
    ADD COLUMN snapshot_content_scope TEXT,
    ADD COLUMN history_sequence BIGINT;

DROP INDEX agent_instance_one_active_task_idx;
CREATE UNIQUE INDEX agent_instance_one_active_task_idx
    ON agent_instance_task (instance_id)
    WHERE state NOT IN (
        'TASK_STATE_COMPLETED',
        'TASK_STATE_CANCELED',
        'TASK_STATE_FAILED',
        'TASK_STATE_REJECTED',
        'TASK_STATE_INPUT_REQUIRED',
        'TASK_STATE_AUTH_REQUIRED'
    );

CREATE TABLE agent_instance_checkpoint (
    id TEXT PRIMARY KEY,
    namespace TEXT NOT NULL,
    -- Provenance only: checkpoints outlive their source and may initialize other Actors.
    source_instance_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    head_task_id TEXT NOT NULL,
    history_sequence BIGINT NOT NULL,
    snapshot_atespace TEXT NOT NULL,
    snapshot_name TEXT NOT NULL,
    snapshot_uid TEXT NOT NULL,
    snapshot_content_scope TEXT NOT NULL,
    tag_uid TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL,
    failure TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (snapshot_content_scope IN ('FULL', 'DATA')),
    CHECK (state IN ('CREATING', 'READY', 'FAILED', 'DELETING')),
    UNIQUE (user_id, namespace, request_id)
);

CREATE INDEX agent_instance_checkpoint_list_idx
    ON agent_instance_checkpoint (namespace, source_instance_id, id);

CREATE UNIQUE INDEX agent_instance_checkpoint_one_creating_idx
    ON agent_instance_checkpoint (source_instance_id)
    WHERE state = 'CREATING';

CREATE TABLE a2a_context (
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
    ADD COLUMN context_id TEXT;

UPDATE agent_instance SET context_id = id WHERE context_id IS NULL;

ALTER TABLE agent_instance
    ALTER COLUMN context_id SET NOT NULL,
    ADD CONSTRAINT agent_instance_context_id_fkey
        FOREIGN KEY (context_id) REFERENCES a2a_context(id) ON DELETE RESTRICT;

ALTER TABLE agent_instance_task
    DROP CONSTRAINT agent_instance_task_instance_id_fkey;

ALTER TABLE agent_instance_task
    RENAME COLUMN instance_id TO context_id;

ALTER TABLE agent_instance_task
    ADD CONSTRAINT agent_instance_task_context_id_fkey
        FOREIGN KEY (context_id) REFERENCES a2a_context(id) ON DELETE CASCADE;

ALTER TABLE agent_instance_task_event
    DROP CONSTRAINT agent_instance_task_event_instance_id_fkey;

ALTER TABLE agent_instance_task_event
    RENAME COLUMN instance_id TO context_id;

ALTER TABLE agent_instance_task_event
    ADD CONSTRAINT agent_instance_task_event_context_id_fkey
        FOREIGN KEY (context_id) REFERENCES a2a_context(id) ON DELETE CASCADE;

ALTER TABLE agent_instance_task_event
    ADD COLUMN message_id TEXT;

CREATE UNIQUE INDEX agent_instance_task_event_message_idx
    ON agent_instance_task_event (context_id, task_id, message_id)
    WHERE message_id IS NOT NULL;

ALTER TABLE agent_instance_checkpoint
    ADD COLUMN source_context_id TEXT,
    ADD COLUMN prepared_revision TEXT REFERENCES runtime_revision(revision) ON DELETE RESTRICT,
    ADD COLUMN source_labels JSONB NOT NULL DEFAULT '{}'
        CHECK (jsonb_typeof(source_labels) = 'object');

UPDATE agent_instance_checkpoint
SET source_context_id = source_instance_id
WHERE source_context_id IS NULL;

ALTER TABLE agent_instance_checkpoint
    ALTER COLUMN source_context_id SET NOT NULL,
    ADD CONSTRAINT agent_instance_checkpoint_source_context_id_fkey
        FOREIGN KEY (source_context_id) REFERENCES a2a_context(id) ON DELETE RESTRICT;

UPDATE agent_instance_checkpoint c
SET prepared_revision = i.prepared_revision,
    source_labels = i.labels
FROM agent_instance i
WHERE i.id = c.source_instance_id
  AND c.prepared_revision IS NULL;

ALTER TABLE agent_instance
    ADD COLUMN source_checkpoint_id TEXT REFERENCES agent_instance_checkpoint(id) ON DELETE RESTRICT;

-- A reader-supplied display name for the conversation an AgentInstance is.
-- Deliberately not unique: unlike a Kubernetes name this is a label for a human,
-- and two conversations with the same agent may reasonably carry the same title.
-- The default keeps the column additive — every existing row reads as unnamed.
ALTER TABLE agent_instance
    ADD COLUMN name TEXT NOT NULL DEFAULT '';

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

ALTER TABLE runtime_revision
    RENAME COLUMN actor_template_namespace TO actor_template_atespace;

ALTER TABLE runtime_revision
    DROP COLUMN phase,
    DROP COLUMN golden_snapshot;

-- +goose Down

-- Version zero has no Kagent schema, so remove the baseline in dependency order.
DROP TABLE agent_instance_share;
DROP TABLE agent_instance_task_event;
DROP TABLE agent_instance_task;
DROP TABLE agent_instance;
DROP TABLE agent_instance_checkpoint;
DROP TABLE a2a_context;
DROP TABLE agent_template_harness_pair;
DROP TABLE runtime_revision;
DROP TABLE crewai_flow_state;
DROP TABLE crewai_agent_memory;
DROP TABLE lg_checkpoint_write;
DROP TABLE lg_checkpoint;
DROP TABLE toolserver;
DROP TABLE tool;
DROP TABLE feedback;
DROP TABLE agent;
