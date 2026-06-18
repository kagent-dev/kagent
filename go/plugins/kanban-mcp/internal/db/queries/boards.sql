-- name: CreateBoard :one
INSERT INTO kanban.board (key, name, description, scope, owner, columns, subtasks)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpsertBoard :one
INSERT INTO kanban.board (key, name, description, scope, owner, columns, subtasks)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (key) DO UPDATE
SET name        = EXCLUDED.name,
    description = EXCLUDED.description,
    scope       = EXCLUDED.scope,
    owner       = EXCLUDED.owner,
    columns     = EXCLUDED.columns,
    subtasks    = EXCLUDED.subtasks,
    updated_at  = NOW()
RETURNING *;

-- name: GetBoardByKey :one
SELECT * FROM kanban.board
WHERE key = $1;

-- name: GetBoardByID :one
SELECT * FROM kanban.board
WHERE id = $1;

-- name: ListBoards :many
SELECT * FROM kanban.board
ORDER BY created_at;
