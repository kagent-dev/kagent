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

-- name: ListUserTasks :many
-- Lists a user's tasks across every session they own (or a single session when
-- session_id is set), filtering, ordering, and paginating server-side. total is
-- the COUNT(*) OVER() of the full filtered set, before LIMIT/OFFSET.
--
-- status_state matches task.data's persisted state string. Rows exist in two
-- shapes: v1 (protocol_version = 'v1', state e.g. 'TASK_STATE_WORKING') and
-- legacy (protocol_version NULL, state e.g. 'working'); the caller passes both
-- spellings in status_v1/status_legacy so either row shape matches. The two
-- vocabularies never overlap, so matching against both is unambiguous.
-- data is always a JSON object (json.Marshal output), so ::jsonb never errors;
-- the timestamp cast is guarded by a CASE (ordered evaluation) against a
-- present-but-malformed value.
--
-- COUNT(*) OVER() rides on the returned rows, so a page past the end of the set
-- carries no total; callers recover it with CountUserTasks, whose WHERE clause
-- must stay identical to this one.
SELECT task.*, COUNT(*) OVER() AS total
FROM task
JOIN session ON session.id = task.session_id
WHERE session.user_id = @user_id
  AND task.deleted_at IS NULL
  AND session.deleted_at IS NULL
  AND (sqlc.narg('session_id')::text IS NULL OR task.session_id = sqlc.narg('session_id'))
  AND (
    sqlc.narg('status_v1')::text IS NULL
    OR (task.data::jsonb -> 'status' ->> 'state') IN (sqlc.narg('status_v1'), sqlc.narg('status_legacy'))
  )
  AND (
    sqlc.narg('status_after')::timestamptz IS NULL
    OR (
      CASE
        WHEN (task.data::jsonb -> 'status' ->> 'timestamp') ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}'
        THEN (task.data::jsonb -> 'status' ->> 'timestamp')::timestamptz
      END
    ) > sqlc.narg('status_after')
  )
ORDER BY task.id
LIMIT @page_limit::int OFFSET @page_offset::int;

-- name: CountUserTasks :one
-- The full filtered count for ListUserTasks, independent of LIMIT/OFFSET. Used
-- to recover total when a requested page lands past the end of the set (an empty
-- page carries no COUNT(*) OVER()). The WHERE clause is identical to
-- ListUserTasks and must stay in sync with it.
SELECT COUNT(*)
FROM task
JOIN session ON session.id = task.session_id
WHERE session.user_id = @user_id
  AND task.deleted_at IS NULL
  AND session.deleted_at IS NULL
  AND (sqlc.narg('session_id')::text IS NULL OR task.session_id = sqlc.narg('session_id'))
  AND (
    sqlc.narg('status_v1')::text IS NULL
    OR (task.data::jsonb -> 'status' ->> 'state') IN (sqlc.narg('status_v1'), sqlc.narg('status_legacy'))
  )
  AND (
    sqlc.narg('status_after')::timestamptz IS NULL
    OR (
      CASE
        WHEN (task.data::jsonb -> 'status' ->> 'timestamp') ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}'
        THEN (task.data::jsonb -> 'status' ->> 'timestamp')::timestamptz
      END
    ) > sqlc.narg('status_after')
  );

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
