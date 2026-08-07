-- name: GetSession :one
SELECT * FROM session
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
LIMIT 1;

-- name: ListSessions :many
SELECT * FROM session
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY updated_at DESC, created_at DESC;

-- name: ListSessionsForAgent :many
SELECT s.id, s.user_id, s.name, s.created_at, s.updated_at, s.deleted_at, s.agent_id, s.source,
       (CASE WHEN s.user_id = $2 THEN NULL::text    ELSE sh.token     END) AS share_token,
       (CASE WHEN s.user_id = $2 THEN NULL::boolean ELSE sh.read_only END) AS share_read_only
FROM session s
LEFT JOIN LATERAL (
    SELECT ss.token, ss.read_only
    FROM session_share ss
    JOIN session_share_access sa ON sa.share_id = ss.id
    WHERE ss.session_id = s.id AND sa.user_id = $2
    ORDER BY ss.read_only ASC, ss.created_at DESC
    LIMIT 1
) sh ON true
WHERE s.agent_id = $1 AND s.deleted_at IS NULL
  AND (s.source IS NULL OR s.source != 'agent')
  AND (s.user_id = $2 OR sh.token IS NOT NULL)
ORDER BY s.updated_at DESC, s.created_at DESC;

-- name: ListSessionsForAgentAllUsers :many
SELECT * FROM session
WHERE agent_id = $1 AND deleted_at IS NULL
  AND (source IS NULL OR source != 'agent')
ORDER BY updated_at DESC, created_at DESC;

-- UpsertSession resurrects a soft-deleted (id, user_id) as a fresh incarnation:
-- deleted_at is cleared, created_at restarts, and the previous incarnation's
-- events, owned tasks, and shares are purged so the recreated session starts
-- empty. NULL-owned tasks are left alone: the created_at bump already hides
-- them from this session (see the ownership bound in tasks.sql), and they may
-- still resolve to another user's same-id session.
-- name: UpsertSession :exec
WITH prior AS (
    SELECT 1 AS resurrected FROM session
    WHERE id = $1 AND user_id = $2 AND deleted_at IS NOT NULL
),
purged_events AS (
    UPDATE event SET deleted_at = NOW()
    WHERE session_id = $1 AND user_id = $2 AND deleted_at IS NULL
      AND EXISTS (SELECT 1 FROM prior)
),
purged_tasks AS (
    UPDATE task SET deleted_at = NOW()
    WHERE session_id = $1 AND user_id = $2 AND deleted_at IS NULL
      AND EXISTS (SELECT 1 FROM prior)
),
purged_shares AS (
    DELETE FROM session_share
    WHERE session_id = $1 AND user_id = $2
      AND EXISTS (SELECT 1 FROM prior)
)
INSERT INTO session (id, user_id, name, agent_id, source, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
ON CONFLICT (id, user_id) DO UPDATE SET
    name       = EXCLUDED.name,
    agent_id   = EXCLUDED.agent_id,
    source     = EXCLUDED.source,
    updated_at = NOW(),
    deleted_at = NULL,
    created_at = CASE WHEN session.deleted_at IS NOT NULL THEN NOW()
                      ELSE session.created_at END;

-- name: SoftDeleteSession :exec
UPDATE session SET deleted_at = NOW()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;
