-- name: CreateSubtask :one
INSERT INTO kanban.subtask (task_id, title)
VALUES ($1, $2)
RETURNING *;

-- name: GetSubtask :one
SELECT * FROM kanban.subtask
WHERE id = $1;

-- name: ListSubtasksByTask :many
SELECT * FROM kanban.subtask
WHERE task_id = $1
ORDER BY id;

-- name: SetSubtaskDone :one
UPDATE kanban.subtask
SET done       = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateSubtaskTitle :one
UPDATE kanban.subtask
SET title      = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteSubtask :exec
DELETE FROM kanban.subtask
WHERE id = $1;
