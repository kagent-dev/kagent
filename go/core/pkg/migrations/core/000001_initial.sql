-- +goose Up

-- Unreleased Kagent 1.0 baseline. Fold schema changes into this migration
-- until release; development databases must be recreated when it changes.

CREATE TABLE tool (
    id          TEXT        NOT NULL,
    server_name TEXT        NOT NULL,
    group_kind  TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ,
    description TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (id, server_name, group_kind)
);
CREATE INDEX idx_tool_deleted_at ON tool(deleted_at);

CREATE TABLE toolserver (
    name           TEXT        NOT NULL,
    group_kind     TEXT        NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at     TIMESTAMPTZ,
    description    TEXT NOT NULL DEFAULT '',
    last_connected TIMESTAMPTZ,
    PRIMARY KEY (name, group_kind)
);
CREATE INDEX idx_toolserver_deleted_at ON toolserver(deleted_at);

CREATE TABLE runtime_revision (
    revision                 TEXT        PRIMARY KEY,
    namespace                TEXT        NOT NULL,
    agent_template_name      TEXT        NOT NULL,
    agent_template_uid       TEXT        NOT NULL,
    harness_name             TEXT        NOT NULL,
    harness_uid              TEXT        NOT NULL,
    source_snapshot          JSONB       NOT NULL,
    egress_destinations      TEXT[]      NOT NULL DEFAULT '{}',
    actor_template_atespace  TEXT        CONSTRAINT runtime_revision_actor_template_namespace_not_null NOT NULL,
    actor_template_name      TEXT        NOT NULL,
    actor_template_uid       TEXT        NOT NULL DEFAULT '',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    agent_card               BYTEA       NOT NULL,
    -- Logical deletion; retain the row until ActorTemplate cleanup completes.
    deleted_at               TIMESTAMPTZ,
    CONSTRAINT runtime_revision_actor_template_namespace_actor_template_na_key
        UNIQUE (actor_template_atespace, actor_template_name)
);

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

CREATE TABLE a2a_context (
    id                      UUID        PRIMARY KEY,
    user_id                 TEXT        NOT NULL CHECK (user_id <> ''),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    context_id              UUID        NOT NULL,
    parent_history_id       UUID        REFERENCES a2a_context(id) ON DELETE RESTRICT,
    parent_history_sequence BIGINT,
    CONSTRAINT a2a_context_binding_key UNIQUE (id, context_id),
    CHECK ((parent_history_id IS NULL) = (parent_history_sequence IS NULL)),
    CHECK (parent_history_id <> id),
    CHECK (parent_history_sequence > 0)
);
CREATE INDEX a2a_context_parent_history_idx ON a2a_context (parent_history_id, parent_history_sequence)
    WHERE parent_history_id IS NOT NULL;

CREATE TABLE agent_instance_checkpoint (
    id                     UUID        PRIMARY KEY,
    source_instance_id     UUID        NOT NULL,
    user_id                TEXT        NOT NULL,
    request_id             TEXT        NOT NULL,
    head_task_id           TEXT        NOT NULL,
    history_sequence       BIGINT      NOT NULL,
    snapshot_atespace      TEXT        NOT NULL,
    snapshot_uri           TEXT        NOT NULL,
    snapshot_content_scope TEXT        NOT NULL,
    tag_uid                TEXT        NOT NULL DEFAULT '',
    state                  TEXT        NOT NULL,
    data                   BYTEA       NOT NULL,
    source_history_id      UUID        NOT NULL REFERENCES a2a_context(id) ON DELETE RESTRICT,
    prepared_revision      TEXT        REFERENCES runtime_revision(revision) ON DELETE RESTRICT,
    source_name            TEXT        NOT NULL DEFAULT '',
    CHECK (snapshot_content_scope IN ('FULL', 'DATA')),
    CHECK (state IN ('CREATING', 'READY', 'FAILED', 'DELETING')),
    UNIQUE (user_id, request_id)
);
CREATE INDEX agent_instance_checkpoint_list_idx
    ON agent_instance_checkpoint (source_instance_id, id);
CREATE INDEX agent_instance_checkpoint_history_idx
    ON agent_instance_checkpoint (source_history_id, history_sequence);
CREATE UNIQUE INDEX agent_instance_checkpoint_one_creating_idx
    ON agent_instance_checkpoint (source_instance_id)
    WHERE state = 'CREATING';

CREATE TABLE agent_instance (
    id                   UUID        PRIMARY KEY,
    user_id              TEXT        NOT NULL CHECK (user_id <> ''),
    request_id           TEXT        NOT NULL,
    prepared_revision    TEXT        REFERENCES runtime_revision(revision) ON DELETE RESTRICT,
    state                TEXT        NOT NULL,
    data                 BYTEA       NOT NULL,
    operation            TEXT        NOT NULL DEFAULT 'AGENT_INSTANCE_OPERATION_UNSPECIFIED',
    context_id           UUID        NOT NULL,
    source_checkpoint_id UUID        REFERENCES agent_instance_checkpoint(id) ON DELETE RESTRICT,
    history_id           UUID        NOT NULL,
    CONSTRAINT agent_instance_context_binding_fkey
        FOREIGN KEY (history_id, context_id) REFERENCES a2a_context(id, context_id) ON DELETE RESTRICT,
    CONSTRAINT agent_instance_history_key UNIQUE (history_id),
    CONSTRAINT agent_instance_operation_check
        CHECK (operation IN ('AGENT_INSTANCE_OPERATION_UNSPECIFIED', 'AGENT_INSTANCE_OPERATION_CREATE',
            'AGENT_INSTANCE_OPERATION_SUSPEND', 'AGENT_INSTANCE_OPERATION_RESUME', 'AGENT_INSTANCE_OPERATION_DELETE')),
    CHECK (state IN ('AGENT_INSTANCE_STATE_CREATING', 'AGENT_INSTANCE_STATE_READY',
        'AGENT_INSTANCE_STATE_SUSPENDED', 'AGENT_INSTANCE_STATE_FAILED')),
    UNIQUE (user_id, request_id)
);
CREATE INDEX agent_instance_user_id_id_idx
    ON agent_instance (user_id, id);

CREATE TABLE agent_instance_share (
    id          UUID        PRIMARY KEY,
    instance_id UUID        NOT NULL REFERENCES agent_instance(id) ON DELETE CASCADE,
    permission  TEXT        NOT NULL CHECK (permission IN (
        'AGENT_INSTANCE_SHARE_PERMISSION_READ_ONLY', 'AGENT_INSTANCE_SHARE_PERMISSION_READ_WRITE')),
    token_hash  BYTEA       NOT NULL UNIQUE,
    data        BYTEA       NOT NULL
);
CREATE INDEX agent_instance_share_instance_idx
    ON agent_instance_share (instance_id, id);

CREATE TABLE agent_instance_task (
    history_id             UUID        CONSTRAINT agent_instance_task_instance_id_not_null NOT NULL REFERENCES a2a_context(id) ON DELETE CASCADE,
    id                     TEXT        NOT NULL,
    state                  TEXT        NOT NULL,
    status_timestamp       TIMESTAMPTZ,
    data                   BYTEA       NOT NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    initial_message_id     TEXT,
    request_hash           BYTEA,
    snapshot_atespace      TEXT,
    snapshot_uri           TEXT,
    snapshot_content_scope TEXT,
    history_sequence       BIGINT,
    position               BIGINT      NOT NULL GENERATED BY DEFAULT AS IDENTITY,
    PRIMARY KEY (history_id, id)
);
CREATE UNIQUE INDEX agent_instance_one_active_task_idx
    ON agent_instance_task (history_id)
    WHERE state NOT IN (
        'TASK_STATE_COMPLETED',
        'TASK_STATE_CANCELED',
        'TASK_STATE_FAILED',
        'TASK_STATE_REJECTED',
        'TASK_STATE_INPUT_REQUIRED',
        'TASK_STATE_AUTH_REQUIRED'
    );
CREATE UNIQUE INDEX agent_instance_task_list_idx
    ON agent_instance_task (history_id, position);
CREATE UNIQUE INDEX agent_instance_task_message_idx
    ON agent_instance_task (history_id, initial_message_id)
    WHERE initial_message_id IS NOT NULL;

CREATE TABLE agent_instance_task_event (
    sequence   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    history_id UUID        CONSTRAINT agent_instance_task_event_instance_id_not_null NOT NULL REFERENCES a2a_context(id) ON DELETE CASCADE,
    task_id    TEXT,
    data       BYTEA       NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    message_id TEXT,
    -- Creation events retain the metadata needed to rebuild task indexes.
    task_position BIGINT,
    initial_message_id TEXT,
    request_hash BYTEA,
    snapshot_atespace TEXT,
    snapshot_uri TEXT,
    snapshot_content_scope TEXT,
    CHECK ((snapshot_atespace IS NULL AND snapshot_uri IS NULL AND snapshot_content_scope IS NULL)
        OR (snapshot_atespace IS NOT NULL AND snapshot_uri IS NOT NULL AND snapshot_content_scope IS NOT NULL)),
    CHECK (task_position IS NULL OR (task_position > 0 AND task_id IS NOT NULL AND message_id IS NULL)),
    CHECK (task_position IS NOT NULL OR (initial_message_id IS NULL AND request_hash IS NULL))
);
CREATE UNIQUE INDEX agent_instance_task_event_creation_idx
    ON agent_instance_task_event (history_id, task_id) WHERE task_position IS NOT NULL;
CREATE UNIQUE INDEX agent_instance_task_event_position_idx
    ON agent_instance_task_event (history_id, task_position) WHERE task_position IS NOT NULL;
CREATE INDEX agent_instance_task_event_instance_sequence_idx
    ON agent_instance_task_event (history_id, sequence);
CREATE UNIQUE INDEX agent_instance_task_event_message_idx
    ON agent_instance_task_event (history_id, task_id, message_id)
    WHERE message_id IS NOT NULL;

CREATE TABLE scheduled_run (
    id UUID PRIMARY KEY,
    creator TEXT NOT NULL CHECK (creator <> ''),
    request_id TEXT NOT NULL CHECK (char_length(request_id) BETWEEN 1 AND 128),
    request_hash BYTEA NOT NULL CHECK (octet_length(request_hash) = 32),
    data BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
    next_execution_time TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    UNIQUE (creator, request_id),
    CHECK (deleted_at IS NULL OR next_execution_time IS NULL)
);
CREATE INDEX scheduled_run_owner_idx ON scheduled_run (creator, id) WHERE deleted_at IS NULL;
CREATE INDEX scheduled_run_due_idx ON scheduled_run (next_execution_time, id)
    WHERE deleted_at IS NULL AND next_execution_time IS NOT NULL;

-- The optional instance ID is historical provenance, without a foreign key.
CREATE TABLE scheduled_run_execution (
    id UUID PRIMARY KEY,
    scheduled_run_id UUID NOT NULL REFERENCES scheduled_run(id) ON DELETE RESTRICT,
    scheduled_time TIMESTAMPTZ,
    manual_request_id TEXT CHECK (char_length(manual_request_id) BETWEEN 1 AND 128),
    data BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
    deadline TIMESTAMPTZ NOT NULL CHECK (deadline > created_at),
    agent_instance_id UUID,
    task_id TEXT,
    completed_at TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    lease_token UUID,
    state TEXT NOT NULL DEFAULT 'SCHEDULED_RUN_EXECUTION_STATE_PENDING' CHECK (state IN (
        'SCHEDULED_RUN_EXECUTION_STATE_PENDING', 'SCHEDULED_RUN_EXECUTION_STATE_RUNNING',
        'SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED', 'SCHEDULED_RUN_EXECUTION_STATE_FAILED',
        'SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT')),
    CHECK ((scheduled_time IS NULL) <> (manual_request_id IS NULL)),
    CHECK (state NOT IN ('SCHEDULED_RUN_EXECUTION_STATE_RUNNING', 'SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED')
        OR agent_instance_id IS NOT NULL),
    CHECK (task_id IS NULL OR agent_instance_id IS NOT NULL),
    CHECK ((completed_at IS NOT NULL) = (state IN ('SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED',
        'SCHEDULED_RUN_EXECUTION_STATE_FAILED', 'SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT'))),
    UNIQUE (scheduled_run_id, scheduled_time),
    UNIQUE (scheduled_run_id, manual_request_id)
);
CREATE INDEX scheduled_run_execution_history_idx ON scheduled_run_execution (scheduled_run_id, id);
CREATE INDEX scheduled_run_execution_pending_idx ON scheduled_run_execution (next_attempt_at, id)
    WHERE state IN ('SCHEDULED_RUN_EXECUTION_STATE_PENDING', 'SCHEDULED_RUN_EXECUTION_STATE_RUNNING');

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

-- +goose Down

DROP TABLE scheduled_run_execution;
DROP TABLE scheduled_run;
DROP VIEW unreferenced_runtime_revision;

DROP TABLE agent_instance_share;
DROP TABLE agent_instance_task_event;
DROP TABLE agent_instance_task;
DROP TABLE agent_instance;
DROP TABLE agent_instance_checkpoint;
DROP TABLE a2a_context;
DROP TABLE agent_template_harness_pair;
DROP TABLE runtime_revision;
DROP TABLE toolserver;
DROP TABLE tool;
