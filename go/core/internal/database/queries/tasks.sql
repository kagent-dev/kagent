-- Task ownership: a task belongs to task.user_id. A NULL user_id (row written
-- before the owner column existed, or by a pre-upgrade pod during a rolling
-- upgrade) is only visible to, and claimable by, a caller whose session id
-- maps to exactly one user across its whole history (deleted sessions
-- included) and that user is the caller. Anything ambiguous stays hidden
-- rather than guessed. This mirrors the backfill rule in migration
-- 000007_task_owner.
--
-- The resolving session must also have existed at or before the task was
-- written (s.created_at <= task.created_at). Without that bound, a task
-- whose original session is gone entirely (hard deleted, or never migrated)
-- would become claimable by whoever is first to create a brand new session
-- reusing that same id after the fact, handing them a stranger's task.

-- name: GetTask :one
SELECT * FROM task
WHERE task.id = $1 AND task.deleted_at IS NULL
  AND (task.user_id = $2 OR (task.user_id IS NULL AND $2 = (
      SELECT MIN(s.user_id) FROM session s
      WHERE s.id = task.session_id AND s.created_at <= task.created_at
      HAVING COUNT(DISTINCT s.user_id) = 1)))
LIMIT 1;

-- name: GetTaskOwner :one
SELECT user_id FROM task WHERE id = $1 AND deleted_at IS NULL LIMIT 1;

-- name: TaskExists :one
SELECT EXISTS (
    SELECT 1 FROM task WHERE id = $1 AND deleted_at IS NULL
) AS exists;

-- name: ListTasksForSession :many
SELECT * FROM task
WHERE task.session_id = $1 AND task.deleted_at IS NULL
  AND (task.user_id = $2 OR (task.user_id IS NULL AND $2 = (
      SELECT MIN(s.user_id) FROM session s
      WHERE s.id = task.session_id AND s.created_at <= task.created_at
      HAVING COUNT(DISTINCT s.user_id) = 1)))
ORDER BY created_at ASC;

-- UpsertTask returns the upserted id, or no rows when the write was rejected:
-- the id belongs to another user, or it belongs to a soft-deleted task (a
-- deleted id is never updated or resurrected, it stays burned). Callers map
-- "no rows" to a conflict error.
-- name: UpsertTask :one
WITH upserted_task AS (
INSERT INTO task (id, data, session_id, protocol_version, user_id, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
    data             = EXCLUDED.data,
    session_id       = EXCLUDED.session_id,
    protocol_version = EXCLUDED.protocol_version,
    user_id          = EXCLUDED.user_id,
    updated_at       = NOW()
WHERE task.deleted_at IS NULL
  AND (task.user_id = EXCLUDED.user_id
   OR (task.user_id IS NULL AND EXCLUDED.user_id = (
       SELECT MIN(s.user_id) FROM session s
       WHERE s.id = task.session_id AND s.created_at <= task.created_at
       HAVING COUNT(DISTINCT s.user_id) = 1)))
RETURNING id, session_id, user_id
),
touched_session AS (
UPDATE session
SET updated_at = NOW()
FROM upserted_task
WHERE upserted_task.session_id IS NOT NULL
  AND session.id = upserted_task.session_id
  AND session.user_id = upserted_task.user_id
  AND session.deleted_at IS NULL
)
SELECT id FROM upserted_task;

-- name: SoftDeleteTask :exec
UPDATE task SET deleted_at = NOW()
WHERE task.id = $1 AND task.deleted_at IS NULL
  AND (task.user_id = $2 OR (task.user_id IS NULL AND $2 = (
      SELECT MIN(s.user_id) FROM session s
      WHERE s.id = task.session_id AND s.created_at <= task.created_at
      HAVING COUNT(DISTINCT s.user_id) = 1)));
