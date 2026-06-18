-- Add an optional due date to cards (Features and Tasks). NULL means no due date.
ALTER TABLE kanban.task
    ADD COLUMN due_date TIMESTAMPTZ;
