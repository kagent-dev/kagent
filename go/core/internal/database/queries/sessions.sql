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

-- name: UpsertSession :exec
INSERT INTO session (id, user_id, name, agent_id, source, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
ON CONFLICT (id, user_id) DO UPDATE SET
    name       = EXCLUDED.name,
    agent_id   = EXCLUDED.agent_id,
    source     = EXCLUDED.source,
    updated_at = NOW();

-- name: SoftDeleteSession :exec
UPDATE session SET deleted_at = NOW()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- DeleteExpiredSessionsBatch hard-deletes up to batch_size idle sessions whose
-- updated_at is older than retention_days, plus cascaded conversation state.
-- name: DeleteExpiredSessionsBatch :one
WITH expired AS (
    SELECT id, user_id
    FROM session
    WHERE updated_at < NOW() - make_interval(days => sqlc.arg(retention_days)::int)
    ORDER BY updated_at ASC
    LIMIT sqlc.arg(batch_size)
),
del_shares AS (
    DELETE FROM session_share ss
    USING expired e
    WHERE ss.session_id = e.id AND ss.user_id = e.user_id
),
del_events AS (
    DELETE FROM event ev
    USING expired e
    WHERE ev.session_id = e.id AND ev.user_id = e.user_id
),
del_push AS (
    DELETE FROM push_notification pn
    USING task t, expired e
    WHERE pn.task_id = t.id
      AND t.session_id = e.id
      AND COALESCE(t.user_id, e.user_id) = e.user_id
),
del_tasks AS (
    DELETE FROM task t
    USING expired e
    WHERE t.session_id = e.id
      AND COALESCE(t.user_id, e.user_id) = e.user_id
),
del_cp_writes AS (
    DELETE FROM lg_checkpoint_write w
    USING expired e
    WHERE w.user_id = e.user_id AND w.thread_id = e.id
),
del_cps AS (
    DELETE FROM lg_checkpoint c
    USING expired e
    WHERE c.user_id = e.user_id AND c.thread_id = e.id
),
del_crewai_mem AS (
    DELETE FROM crewai_agent_memory m
    USING expired e
    WHERE m.user_id = e.user_id AND m.thread_id = e.id
),
del_crewai_flow AS (
    DELETE FROM crewai_flow_state f
    USING expired e
    WHERE f.user_id = e.user_id AND f.thread_id = e.id
),
del_sessions AS (
    DELETE FROM session s
    USING expired e
    WHERE s.id = e.id AND s.user_id = e.user_id
    RETURNING s.id
)
SELECT COUNT(*)::bigint AS deleted FROM del_sessions;
