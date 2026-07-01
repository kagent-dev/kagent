-- due_date is no longer a dedicated column: dates (create_date, due_date,
-- close_date, and any *_date key) are now stored as card attributes
-- (type='attribute' rows in kanban.attachment). Drop the column.
ALTER TABLE kanban.task DROP COLUMN due_date;
