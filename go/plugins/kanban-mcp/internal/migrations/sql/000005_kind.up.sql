-- kind distinguishes a Feature from a Task independently of parent_id, so a
-- top-level card can be either a Feature or a standalone Task. Existing rows are
-- backfilled from parent_id: child cards become Tasks, the rest stay Features.
ALTER TABLE kanban.task ADD COLUMN kind VARCHAR(16) NOT NULL DEFAULT 'feature';
UPDATE kanban.task SET kind = 'task' WHERE parent_id IS NOT NULL;
