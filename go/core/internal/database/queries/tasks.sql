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
INSERT INTO task (id, data, session_id, protocol_version, created_at, updated_at)
VALUES ($1, $2, $3, $4, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
    data             = EXCLUDED.data,
    session_id       = EXCLUDED.session_id,
    protocol_version = EXCLUDED.protocol_version,
    updated_at       = NOW()
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

-- name: ListLegacyTasks :many
SELECT id, data FROM task
WHERE protocol_version IS NULL AND id > $1
ORDER BY id
LIMIT $2;

-- name: MigrateTask :execresult
UPDATE task
SET data = $1, protocol_version = $2, updated_at = NOW()
WHERE id = $3 AND data = $4 AND protocol_version IS NULL;
