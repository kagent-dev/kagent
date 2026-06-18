-- subtasks is an optional per-board checklist template. When a Task is created on
-- the board, these titles are auto-added as its checklist subtasks. Existing rows
-- default to an empty template (no auto-added subtasks).
ALTER TABLE kanban.board
    ADD COLUMN subtasks TEXT[] NOT NULL DEFAULT '{}';
