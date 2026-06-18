CREATE TABLE kanban.board (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key         VARCHAR(64)  NOT NULL UNIQUE,
    name        VARCHAR(255) NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    scope       VARCHAR(16)  NOT NULL DEFAULT 'general',
    owner       VARCHAR(255) NOT NULL DEFAULT '',
    columns     TEXT[]       NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Built-in board that holds all pre-existing tasks. Its columns are the original
-- fixed workflow so the default board behaves exactly as the v1.x single board.
INSERT INTO kanban.board (key, name, description, scope, owner, columns)
VALUES (
    'default',
    'Default',
    'Default kanban board',
    'general',
    '',
    ARRAY['Inbox', 'Plan', 'Develop', 'Testing', 'CodeReview', 'Release', 'Done']
);

ALTER TABLE kanban.task
    ADD COLUMN board_id BIGINT REFERENCES kanban.board(id) ON DELETE CASCADE;

UPDATE kanban.task
SET board_id = (SELECT id FROM kanban.board WHERE key = 'default');

ALTER TABLE kanban.task ALTER COLUMN board_id SET NOT NULL;

CREATE INDEX idx_task_board_id ON kanban.task(board_id);
