-- name: GetSession :one
SELECT * FROM session
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
LIMIT 1;

-- name: ListSessions :many
SELECT * FROM session
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY updated_at DESC, created_at DESC;

-- name: ListSessionsForAgent :many
SELECT * FROM session
WHERE agent_id = $1 AND user_id = $2 AND deleted_at IS NULL
  AND (source IS NULL OR source != 'agent')
ORDER BY updated_at DESC, created_at DESC;

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
