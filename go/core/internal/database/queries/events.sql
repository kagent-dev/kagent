-- name: InsertEvent :exec
INSERT INTO event (id, user_id, session_id, data, created_at, updated_at)
VALUES ($1, $2, $3, $4, NOW(), NOW());

-- name: GetEvent :one
SELECT * FROM event
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
LIMIT 1;

-- name: ListEventsForSessionAsc :many
SELECT * FROM event
WHERE session_id = $1 AND user_id = $2 AND deleted_at IS NULL
  AND ($3::timestamptz IS NULL OR created_at > $3)
ORDER BY created_at ASC;

-- name: ListEventsForSessionDesc :many
SELECT * FROM event
WHERE session_id = $1 AND user_id = $2 AND deleted_at IS NULL
  AND ($3::timestamptz IS NULL OR created_at > $3)
ORDER BY created_at DESC;

-- name: ListEventsForSessionAscLimit :many
SELECT * FROM event
WHERE session_id = $1 AND user_id = $2 AND deleted_at IS NULL
  AND ($3::timestamptz IS NULL OR created_at > $3)
ORDER BY created_at ASC
LIMIT $4;

-- name: ListEventsForSessionDescLimit :many
SELECT * FROM event
WHERE session_id = $1 AND user_id = $2 AND deleted_at IS NULL
  AND ($3::timestamptz IS NULL OR created_at > $3)
ORDER BY created_at DESC
LIMIT $4;

-- name: ListEventsByContextID :many
SELECT * FROM event
WHERE session_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC;

-- name: ListEventsByContextIDLimit :many
SELECT * FROM event
WHERE session_id = $1 AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $2;

-- name: SoftDeleteEvent :exec
UPDATE event SET deleted_at = NOW()
WHERE id = $1 AND deleted_at IS NULL;
