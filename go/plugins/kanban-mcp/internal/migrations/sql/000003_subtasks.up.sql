-- Checklist subtasks (v1.6): lightweight items attached to a Task. Unlike the
-- v1.x "subtasks" (which were full kanban.task rows), these carry only a title
-- and a done flag. The service enforces that subtasks attach to Tasks (child
-- tasks), not Features (top-level tasks); the FK itself only enforces that the
-- owning task row exists.
CREATE TABLE kanban.subtask (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id    BIGINT      NOT NULL REFERENCES kanban.task(id) ON DELETE CASCADE,
    title      TEXT        NOT NULL,
    done       BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_subtask_task_id ON kanban.subtask(task_id);
