CREATE SCHEMA IF NOT EXISTS kanban;

CREATE TABLE kanban.task (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title             TEXT         NOT NULL,
    description       TEXT         NOT NULL DEFAULT '',
    status            VARCHAR(32)  NOT NULL DEFAULT 'Inbox',
    assignee          VARCHAR(255) NOT NULL DEFAULT '',
    labels            TEXT[]       NOT NULL DEFAULT '{}',
    user_input_needed BOOLEAN      NOT NULL DEFAULT FALSE,
    parent_id         BIGINT       REFERENCES kanban.task(id) ON DELETE CASCADE,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_task_parent_id ON kanban.task(parent_id);
CREATE INDEX idx_task_status    ON kanban.task(status);

CREATE TABLE kanban.attachment (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id    BIGINT       NOT NULL REFERENCES kanban.task(id) ON DELETE CASCADE,
    type       VARCHAR(16)  NOT NULL,
    filename   VARCHAR(255) NOT NULL DEFAULT '',
    url        TEXT         NOT NULL DEFAULT '',
    title      VARCHAR(255) NOT NULL DEFAULT '',
    content    TEXT         NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_attachment_task_id ON kanban.attachment(task_id);
