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
WITH upserted_task AS (
INSERT INTO task (id, data, session_id, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
    data       = EXCLUDED.data,
    session_id = EXCLUDED.session_id,
    updated_at = NOW()
RETURNING session_id
)
UPDATE session
SET updated_at = NOW()
FROM upserted_task
WHERE upserted_task.session_id IS NOT NULL
  AND session.id = upserted_task.session_id
  AND session.deleted_at IS NULL;

-- name: SoftDeleteTask :exec
UPDATE task SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL;
