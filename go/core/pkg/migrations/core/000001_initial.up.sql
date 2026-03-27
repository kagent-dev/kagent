-- Baseline migration: matches the schema produced by GORM AutoMigrate in the
-- prior kagent release. Each subsequent migration applies additive changes.

CREATE TABLE IF NOT EXISTS agent (
    id         TEXT        PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    type       TEXT        NOT NULL,
    config     JSONB
);
CREATE INDEX IF NOT EXISTS idx_agent_deleted_at ON agent(deleted_at);

CREATE TABLE IF NOT EXISTS session (
    id         TEXT        NOT NULL,
    user_id    TEXT        NOT NULL,
    name       TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    agent_id   TEXT,
    PRIMARY KEY (id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_session_name      ON session(name);
CREATE INDEX IF NOT EXISTS idx_session_agent_id  ON session(agent_id);
CREATE INDEX IF NOT EXISTS idx_session_deleted_at ON session(deleted_at);

CREATE TABLE IF NOT EXISTS event (
    id         TEXT        NOT NULL,
    user_id    TEXT        NOT NULL,
    session_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    data       TEXT        NOT NULL,
    PRIMARY KEY (id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_event_session_id ON event(session_id);
CREATE INDEX IF NOT EXISTS idx_event_deleted_at ON event(deleted_at);

CREATE TABLE IF NOT EXISTS task (
    id         TEXT        PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    data       TEXT        NOT NULL,
    session_id TEXT
);
CREATE INDEX IF NOT EXISTS idx_task_session_id ON task(session_id);
CREATE INDEX IF NOT EXISTS idx_task_deleted_at ON task(deleted_at);

CREATE TABLE IF NOT EXISTS push_notification (
    id         TEXT        PRIMARY KEY,
    task_id    TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    data       TEXT        NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_push_notification_task_id    ON push_notification(task_id);
CREATE INDEX IF NOT EXISTS idx_push_notification_deleted_at ON push_notification(deleted_at);

CREATE TABLE IF NOT EXISTS feedback (
    id            BIGSERIAL   PRIMARY KEY,
    created_at    TIMESTAMPTZ,
    updated_at    TIMESTAMPTZ,
    deleted_at    TIMESTAMPTZ,
    user_id       TEXT        NOT NULL,
    message_id    BIGINT,
    is_positive   BOOLEAN     DEFAULT false,
    feedback_text TEXT        NOT NULL,
    issue_type    TEXT
);
CREATE INDEX IF NOT EXISTS idx_feedback_deleted_at ON feedback(deleted_at);
CREATE INDEX IF NOT EXISTS idx_feedback_user_id    ON feedback(user_id);
CREATE INDEX IF NOT EXISTS idx_feedback_message_id ON feedback(message_id);

CREATE TABLE IF NOT EXISTS tool (
    id          TEXT        NOT NULL,
    server_name TEXT        NOT NULL,
    group_kind  TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ,
    description TEXT,
    PRIMARY KEY (id, server_name, group_kind)
);
CREATE INDEX IF NOT EXISTS idx_tool_deleted_at ON tool(deleted_at);

CREATE TABLE IF NOT EXISTS toolserver (
    name           TEXT        NOT NULL,
    group_kind     TEXT        NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at     TIMESTAMPTZ,
    description    TEXT,
    last_connected TIMESTAMPTZ,
    PRIMARY KEY (name, group_kind)
);
CREATE INDEX IF NOT EXISTS idx_toolserver_deleted_at ON toolserver(deleted_at);

CREATE TABLE IF NOT EXISTS lg_checkpoint (
    user_id              TEXT        NOT NULL,
    thread_id            TEXT        NOT NULL,
    checkpoint_ns        TEXT        NOT NULL DEFAULT '',
    checkpoint_id        TEXT        NOT NULL,
    parent_checkpoint_id TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at           TIMESTAMPTZ,
    metadata             TEXT        NOT NULL,
    checkpoint           TEXT        NOT NULL,
    checkpoint_type      TEXT        NOT NULL,
    version              INTEGER     DEFAULT 1,
    PRIMARY KEY (user_id, thread_id, checkpoint_ns, checkpoint_id)
);
CREATE INDEX IF NOT EXISTS idx_lg_checkpoint_parent_checkpoint_id ON lg_checkpoint(parent_checkpoint_id);
CREATE INDEX IF NOT EXISTS idx_lgcp_list                          ON lg_checkpoint(created_at);
CREATE INDEX IF NOT EXISTS idx_lg_checkpoint_deleted_at          ON lg_checkpoint(deleted_at);

CREATE TABLE IF NOT EXISTS lg_checkpoint_write (
    user_id       TEXT    NOT NULL,
    thread_id     TEXT    NOT NULL,
    checkpoint_ns TEXT    NOT NULL DEFAULT '',
    checkpoint_id TEXT    NOT NULL,
    write_idx     INTEGER NOT NULL,
    value         TEXT    NOT NULL,
    value_type    TEXT    NOT NULL,
    channel       TEXT    NOT NULL,
    task_id       TEXT    NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at    TIMESTAMPTZ,
    PRIMARY KEY (user_id, thread_id, checkpoint_ns, checkpoint_id, write_idx)
);
CREATE INDEX IF NOT EXISTS idx_lg_checkpoint_write_deleted_at ON lg_checkpoint_write(deleted_at);

CREATE TABLE IF NOT EXISTS crewai_agent_memory (
    user_id     TEXT        NOT NULL,
    thread_id   TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ,
    memory_data TEXT        NOT NULL,
    PRIMARY KEY (user_id, thread_id)
);
CREATE INDEX IF NOT EXISTS idx_crewai_memory_list             ON crewai_agent_memory(created_at);
CREATE INDEX IF NOT EXISTS idx_crewai_agent_memory_deleted_at ON crewai_agent_memory(deleted_at);

CREATE TABLE IF NOT EXISTS crewai_flow_state (
    user_id     TEXT        NOT NULL,
    thread_id   TEXT        NOT NULL,
    method_name TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ,
    state_data  TEXT        NOT NULL,
    PRIMARY KEY (user_id, thread_id, method_name)
);
CREATE INDEX IF NOT EXISTS idx_crewai_flow_state_list       ON crewai_flow_state(created_at);
CREATE INDEX IF NOT EXISTS idx_crewai_flow_state_deleted_at ON crewai_flow_state(deleted_at);
