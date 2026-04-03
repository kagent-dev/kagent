-- name: GetTask :one
SELECT * FROM task
WHERE id = $1 AND deleted_at IS NULL
LIMIT 1;

-- name: TaskExists :one
SELECT EXISTS (
    SELECT 1 FROM task WHERE id = $1 AND deleted_at IS NULL
) AS exists;

-- name: ListTasksForSession :many
SELECT * FROM task
WHERE session_id = $1 AND deleted_at IS NULL
ORDER BY created_at ASC;

-- name: UpsertTask :exec
INSERT INTO task (id, data, session_id, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
    data       = EXCLUDED.data,
    session_id = EXCLUDED.session_id,
    updated_at = NOW();

-- name: SoftDeleteTask :exec
UPDATE task SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL;
