DROP INDEX IF EXISTS kanban.idx_task_board_id;
ALTER TABLE kanban.task DROP COLUMN IF EXISTS board_id;
DROP TABLE IF EXISTS kanban.board;
