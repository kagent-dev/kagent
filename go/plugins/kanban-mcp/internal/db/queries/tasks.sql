-- name: CreateTask :one
INSERT INTO kanban.task (title, description, status, labels, board_id, kind)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: CreateChildTask :one
INSERT INTO kanban.task (title, description, status, labels, parent_id, board_id, kind)
VALUES ($1, $2, $3, $4, $5, $6, 'task')
RETURNING *;

-- name: GetTask :one
SELECT * FROM kanban.task
WHERE id = $1;

-- name: ListBoardTasks :many
SELECT * FROM kanban.task
WHERE board_id = sqlc.arg('board_id')
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('assignee')::text IS NULL OR assignee = sqlc.narg('assignee'))
  AND (sqlc.narg('label')::text IS NULL OR sqlc.narg('label') = ANY(labels))
ORDER BY created_at;

-- name: ListChildTasks :many
SELECT * FROM kanban.task
WHERE parent_id = $1
ORDER BY created_at;

-- name: UpdateTask :one
UPDATE kanban.task
SET title             = $2,
    description       = $3,
    status            = $4,
    assignee          = $5,
    labels            = $6,
    user_input_needed = $7,
    updated_at        = NOW()
WHERE id = $1
RETURNING *;

-- name: MoveTask :one
UPDATE kanban.task
SET status     = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: AssignTask :one
UPDATE kanban.task
SET assignee   = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: SetUserInputNeeded :one
UPDATE kanban.task
SET user_input_needed = $2,
    updated_at        = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteTask :exec
DELETE FROM kanban.task
WHERE id = $1;
